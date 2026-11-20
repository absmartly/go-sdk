package sdk

// Cluster key selection inside the context, ported from the Java SDK's
// ContextSwitchbackClusterTest (java-sdk main 07807b5). Variants come from
// the raw key and the assigner, both pinned by the platform golden vectors.

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

const sbClusterArms = 3

func sbAttributeSwitchback(path string) jsonmodels.Experiment {
	var experiment = sbDefault(sbClusterArms, sbHour)
	experiment.SwitchbackClusters = sbClusters(sbUnitType, path)
	return experiment
}

func sbExpectedSchedule(key string) []int {
	var result = make([]int, 12)
	for i := range result {
		result[i] = sbDraw(key, int64(i), sbClusterArms)
	}
	return result
}

// sbCreateBare creates a context with the given units and no attributes.
func sbCreateBare(t *testing.T, units map[string]string, data jsonmodels.ContextData) *sbContext {
	t.Helper()
	return sbCreateAt(t, sbAnchor, units, data, sbNoClusterAttribute())
}

func sbClusterExposure(variant int, exposedAt int64, eligible bool, audienceMismatch bool) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: variant, ExposedAt: exposedAt,
		Assigned: true, Eligible: eligible, AudienceMismatch: audienceMismatch}
}

// The unit uid is never the cluster key: with no cluster value the unit is
// ineligible.
func TestSwitchbackCluster_UnitUidIsNeverTheClusterKey(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	for i := int64(0); i < 3; i++ {
		expectInt(t, 0, h.at(sbAnchor+i*sbHour).treatment("exp_sb"))
	}
	h.expectPending(0)
}

// A numeric UID serves and publishes under its canonical string, exactly as
// the same string UID does; the cluster key is the attribute.
func TestSwitchbackCluster_NumericUnitUidClustersLikeItsCanonicalString(t *testing.T) {
	for _, c := range []struct {
		uid  interface{}
		text string
	}{{int64(123456789), "123456789"}, {123456789.0, "123456789"}, {125.75, "126"}} {
		var run = func(set func(*sbContext)) ([]jsonmodels.PublishEvent, []int) {
			var h = sbCreateBare(t, nil, sbData(sbAttributeSwitchback("city")))
			set(h)
			h.setAttribute("city", "Lisboa")
			h.treatment("exp_sb")
			var events = h.publishEvents()
			return events, h.peekSchedule("exp_sb", sbHour, 12)
		}
		var numericEvents, numericSchedule = run(func(h *sbContext) {
			if err := h.ctx.SetUnitValue(sbUnitType, c.uid); err != nil {
				t.Fatal(err)
			}
		})
		var textEvents, textSchedule = run(func(h *sbContext) {
			if err := h.ctx.SetUnit(sbUnitType, c.text); err != nil {
				t.Fatal(err)
			}
		})
		expectValue(t, sbExpectedSchedule("Lisboa"), numericSchedule)
		expectValue(t, textSchedule, numericSchedule)
		expectValue(t, textEvents[0].Units, numericEvents[0].Units)
		expectValue(t, textEvents[0].Exposures, numericEvents[0].Exposures)
		expectValue(t, string(hashUnitBytes(c.text)), numericEvents[0].Units[0].Uid)
	}
}

// Independent reference (Python MD5/base64url) for the published hash of a
// numeric UID, and the raw-key schedule of the integer attribute 123456789
// (murmur3 reference).
func TestSwitchbackCluster_NumericUnitUidMatchesTheIndependentReference(t *testing.T) {
	var schedule = []int{0, 2, 1, 0, 1, 2, 0, 1, 2, 2, 1, 0}
	var h = sbCreateBare(t, nil, sbData(sbAttributeSwitchback("region")))
	if err := h.ctx.SetUnitValue(sbUnitType, int64(123456789)); err != nil {
		t.Fatal(err)
	}
	h.setAttribute("region", 123456789)
	h.treatment("exp_sb")
	var events = h.publishEvents()
	expectValue(t, "JfnnlDI7RTiF9RgfG2JNCw", events[0].Units[0].Uid)
	expectValue(t, []jsonmodels.Exposure{sbClusterExposure(schedule[0], sbAnchor, true, false)}, events[0].Exposures)
	expectValue(t, schedule, h.peekSchedule("exp_sb", sbHour, 12))
}

// Periods 0..11 of the raw key "Lisboa", from the independent murmur3
// reference.
func TestSwitchbackCluster_AttributeSourceServesTheRawAttributeValue(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.setAttribute("city", "Lisboa")
	expectValue(t, []int{0, 2, 1, 2, 1, 0, 1, 0, 2, 0, 1, 2}, h.peekSchedule("exp_sb", sbHour, 12))
	expectValue(t, sbExpectedSchedule("Lisboa"), h.peekSchedule("exp_sb", sbHour, 12))
}

func TestSwitchbackCluster_UnitsSharingAnAttributeValueShareTheSchedule(t *testing.T) {
	var schedule = func(uid string, city string) []int {
		var h = sbCreateBare(t, map[string]string{sbUnitType: uid}, sbData(sbAttributeSwitchback("city")))
		h.setAttribute("city", city)
		return h.peekSchedule("exp_sb", sbHour, 12)
	}
	expectValue(t, sbExpectedSchedule("Lisboa"), schedule(sbUID, "Lisboa"))
	expectValue(t, sbExpectedSchedule("Lisboa"), schedule("another-store", "Lisboa"))
	expectValue(t, sbExpectedSchedule("Porto"), schedule(sbUID, "Porto"))
	if reflect.DeepEqual(sbExpectedSchedule("Lisboa"), sbExpectedSchedule("Porto")) {
		t.Fatal("fixture: Lisboa and Porto share a schedule")
	}
}

type sbTextValue string

func (v sbTextValue) MarshalText() ([]byte, error) {
	return []byte("text-" + string(v)), nil
}

type sbStruct struct {
	City string `json:"city"`
}

// Nested and array paths, and Go values read as their JSON encoding: every
// integer type is its decimal text, a json.Number written as an integer is
// one (beyond int64 too), a TextMarshaler is the string it encodes as, a
// struct is the object it encodes as, a *big.Int is its digits and []byte
// its base64 text.
func TestSwitchbackCluster_NestedAndArrayPathsAndIntegerValuesResolve(t *testing.T) {
	for _, c := range []struct {
		path  string
		name  string
		value interface{}
		key   string
	}{
		{"geo/city", "geo", map[string]interface{}{"city": "Porto"}, "Porto"},
		{"geo/city", "geo", map[string]string{"city": "Porto"}, "Porto"},
		{"items/1/city", "items", []interface{}{map[string]interface{}{"city": "Lisbon"}, map[string]interface{}{"city": "Faro"}}, "Faro"},
		{"items/1/city", "items", []map[string]string{{"city": "Lisbon"}, {"city": "Faro"}}, "Faro"},
		{"items/0/city", "items", [1]map[string]string{{"city": "Lisbon"}}, "Lisbon"},
		{"region", "region", 42, "42"},
		{"region", "region", int8(-42), "-42"},
		{"region", "region", int64(math.MaxInt64), "9223372036854775807"},
		{"region", "region", int64(math.MinInt64), "-9223372036854775808"},
		{"region", "region", uint64(math.MaxUint64), "18446744073709551615"},
		{"region", "region", uint8(7), "7"},
		{"region", "region", json.Number("4000000002"), "4000000002"},
		{"region", "region", json.Number("-0"), "0"},
		{"region", "region", json.Number("123456789012345678901234567890"), "123456789012345678901234567890"},
		{"region", "region", big.NewInt(42), "42"},
		{"region", "region", sbTextValue("x"), "text-x"},
		{"region", "region", "  padded  ", "  padded  "},
		// encoding/json, like Jackson, writes bytes as base64 text.
		{"region", "region", []byte("bytes"), "Ynl0ZXM="},
		{"geo/city", "geo", sbStruct{City: "Braga"}, "Braga"},
		{"geo/city", "geo", &sbStruct{City: "Braga"}, "Braga"},
		{"geo/city", "geo", map[string]interface{}{"city": json.Number("17")}, "17"},
		{"geo/city", "geo", json.RawMessage(`{"city":"Evora"}`), "Evora"},
		{"geo/city", "geo", json.RawMessage(`{"city":17}`), "17"},
	} {
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback(c.path)))
		h.setAttribute(c.name, c.value)
		if got := h.peekSchedule("exp_sb", sbHour, 12); !reflect.DeepEqual(sbExpectedSchedule(c.key), got) {
			t.Errorf("%#v at %s: %v, want the schedule of %q", c.value, c.path, got, c.key)
		}
	}
}

// Values that yield no cluster key serve variant 0, not eligible, record no
// exposure and never panic: every floating-point number (whole ones and a
// json.Number with a fraction or exponent included), booleans, maps, lists,
// the empty string and nil. A float is never an integer here, even when it
// is whole: Go keeps the distinction JSON decoding with UseNumber gives.
func TestSwitchbackCluster_ValuesWithoutAClusterKeyAreIneligible(t *testing.T) {
	var empty *sbStruct
	for _, value := range []interface{}{nil, "", true, false, 1.5, 2.0, float32(2), 0.0, math.NaN(), math.Inf(1),
		json.Number("3.0"), json.Number("1e1"), json.Number("2E0"), json.Number(""), json.Number("x"), json.RawMessage(`3.0`),
		json.RawMessage(`null`), json.RawMessage(`""`), json.RawMessage(`true`), empty, map[string]interface{}{"a": "b"},
		[]interface{}{"x"}, []string{"x"}, complex(1, 2), struct{}{}} {
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
		h.setAttribute("city", value)
		h.at(sbAnchor + 10)
		if got := h.treatment("exp_sb"); got != 0 {
			t.Errorf("%#v: variant %d", value, got)
		}
		// Variant 0's own config, like any ineligible assignment.
		expectValue(t, 0, h.variable("sb.arm"))
		if h.pending() != 0 {
			t.Errorf("%#v: %d pending", value, h.pending())
		}
	}

	var missing = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("geo/city")))
	missing.setAttribute("geo", map[string]interface{}{"country": "PT"})
	expectInt(t, 0, missing.at(sbAnchor+10).treatment("exp_sb"))
	missing.expectPending(0)
}

type sbFailingMarshaler struct{}

func (sbFailingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errSwitchbackArmCount
}

// A value with no JSON encoding (a cycle, a channel, a function, a
// marshaler that fails, a map nested beyond the depth bound) has no cluster
// key.
func TestSwitchbackCluster_UnencodableAttributeValuesAreIneligible(t *testing.T) {
	var cyclic = map[string]interface{}{"city": "Porto"}
	cyclic["self"] = cyclic
	var list = []interface{}{nil}
	var outer = map[string]interface{}{"city": "Porto", "items": list}
	list[0] = outer
	var deep = map[string]interface{}{"leaf": "x"}
	for i := 0; i < 64; i++ {
		deep = map[string]interface{}{"city": "Porto", "next": deep}
	}
	for _, value := range []interface{}{cyclic, outer, deep, make(chan int), func() {}, sbFailingMarshaler{},
		map[string]interface{}{"city": "Porto", "bad": make(chan int)}, map[string]interface{}{"city": "Porto", "nan": math.NaN()}} {
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("geo/city")))
		h.setAttribute("geo", value)
		if got := h.treatment("exp_sb"); got != 0 {
			t.Errorf("%T: variant %d", value, got)
		}
		h.expectPending(0)
	}
	// One map below the bound, and a value reached twice through siblings,
	// are fine.
	var path = parseSwitchbackPath("geo/city")
	expectValue(t, "Porto", path.resolveAttribute(deep["next"]))
	var shared = map[string]interface{}{"a": "b"}
	expectValue(t, "Porto", path.resolveAttribute(map[string]interface{}{"city": "Porto", "x": shared, "y": shared}))
}

// Unservable configurations serve an ineligible control to linked units,
// never a unit-assignment draw, and record nothing.
func TestSwitchbackCluster_UnservableConfigurationsAreIneligible(t *testing.T) {
	var experiments []jsonmodels.Experiment
	for _, path := range []string{"items/0", "items/-1/city", "", "geo.city", "'city", `"city`} {
		experiments = append(experiments, sbAttributeSwitchback(path))
	}
	var noPeriod = sbAttributeSwitchback("city")
	noPeriod.SwitchbackPeriodMs = nil
	var zeroPeriod = sbAttributeSwitchback("city")
	zeroPeriod.SwitchbackPeriodMs = sbInt64(0)
	var negativePeriod = sbAttributeSwitchback("city")
	negativePeriod.SwitchbackPeriodMs = sbInt64(-sbHour)
	var oneArm = sbAttributeSwitchback("city")
	oneArm.Split = []float64{1}
	experiments = append(experiments, noPeriod, zeroPeriod, negativePeriod, oneArm)
	for _, experiment := range experiments {
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		h.setAttribute("city", "Lisboa")
		h.setAttribute("items", []interface{}{map[string]interface{}{"city": "Lisbon"}})
		for i := int64(0); i < 3; i++ {
			if got := h.at(sbAnchor + i*sbHour).treatment("exp_sb"); got != 0 {
				t.Errorf("%+v: variant %d", experiment.SwitchbackClusters, got)
			}
		}
		h.expectPending(0)
	}
}

// A missing or empty cluster map takes the legacy path: a unit of the
// declared unitType only identifies an ineligible control (variant 0's
// config, no exposure); without one the experiment does not apply.
func TestSwitchbackCluster_MissingOrEmptyClusterMapIsOnlyALegacyIneligibleControl(t *testing.T) {
	for _, clusters := range []jsonmodels.SwitchbackClusters{nil, {}} {
		var experiment = sbAttributeSwitchback("city")
		experiment.SwitchbackClusters = clusters
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		h.setAttribute("city", "Lisboa")
		expectInt(t, 0, h.treatment("exp_sb"))
		expectValue(t, 0, h.variable("sb.arm"))
		h.expectPending(0)

		var other = sbCreateBare(t, map[string]string{"user_id": "u1"}, sbData(experiment))
		other.setAttribute("city", "Lisboa")
		expectInt(t, 0, other.treatment("exp_sb"))
		expectValue(t, -1, other.variable("sb.arm"))
		other.expectPending(0)
	}
}

// The attribute set at or before the resolution, latest setAt first, is
// read; system attributes are not visible to cluster paths.
func TestSwitchbackCluster_LatestAttributeSetAtOrBeforeTheResolutionInstantIsUsed(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor+2*sbHour).setAttribute("city", "Porto")
	h.at(sbAnchor+4*sbHour).setAttribute("city", "Lisboa")
	expectInt(t, 0, h.at(sbAnchor+sbHour).peek("exp_sb"))
	expectInt(t, sbDraw("Porto", 3, sbClusterArms), h.at(sbAnchor+3*sbHour).peek("exp_sb"))
	expectInt(t, sbDraw("Lisboa", 4, sbClusterArms), h.at(sbAnchor+4*sbHour).peek("exp_sb"))
}

// Among attributes with the same setAt the one set last wins, and a name
// matches after trimming, as the Collector extracts them.
func TestSwitchbackCluster_RepeatedAttributeNamesResolveToTheLatest(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.setAttribute("city", "Porto")
	h.setAttribute(" city ", "Lisboa")
	expectValue(t, sbExpectedSchedule("Lisboa"), h.peekSchedule("exp_sb", sbHour, 12))
}

func TestSwitchbackCluster_SystemAttributesAreNotVisibleToClusterPaths(t *testing.T) {
	var experiment = sbAttributeSwitchback("environment")
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	h.ctx.environment_ = "production"
	h.ctx.application_ = "website"
	expectInt(t, 0, h.at(sbAnchor+10).treatment("exp_sb"))
	h.expectPending(0)
}

// Within a period the cluster key is fixed at first resolution; the next
// period reads the attribute again.
func TestSwitchbackCluster_SetAttributeMidPeriodDoesNotReassignWithinThePeriod(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor+10).setAttribute("city", "Lisboa")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.at(sbAnchor+20).setAttribute("city", "Porto")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.peek("exp_sb"))
	h.expectPending(1)

	var differing = 0
	for i := int64(1); i < 12; i++ {
		expectInt(t, sbDraw("Porto", i, sbClusterArms), h.at(sbAnchor+i*sbHour).treatment("exp_sb"))
		if sbDraw("Porto", i, sbClusterArms) != sbDraw("Lisboa", i, sbClusterArms) {
			differing++
		}
	}
	if differing == 0 {
		t.Fatal("fixture: Porto and Lisboa agree in every period")
	}
	h.expectPending(12)
}

// A unit first resolved without a key stays ineligible (and unrecorded) for
// the rest of that period.
func TestSwitchbackCluster_AttributeSetAfterAnIneligibleResolutionWaitsForTheNextPeriod(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	expectInt(t, 0, h.at(sbAnchor+10).treatment("exp_sb"))
	h.setAttribute("city", "Lisboa")
	expectInt(t, 0, h.at(sbAnchor+20).treatment("exp_sb"))
	h.expectPending(0)
	expectInt(t, sbDraw("Lisboa", 1, sbClusterArms), h.at(sbAnchor+sbHour).treatment("exp_sb"))
	h.expectPublished(sbClusterExposure(sbDraw("Lisboa", 1, sbClusterArms), sbAnchor+sbHour, true, false))
}

// A slot replaced through a traffic change keeps the period's first key.
func TestSwitchbackCluster_TrafficChangesWithinThePeriodKeepThePeriodsBucket(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor+10).setAttribute("city", "Lisboa")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))

	h.at(sbAnchor+20).setAttribute("city", "Porto")
	var noTraffic = sbAttributeSwitchback("city")
	noTraffic.TrafficSplit = []float64{1, 0}
	h.refresh(noTraffic)
	expectInt(t, 0, h.treatment("exp_sb"))
	h.refresh(sbAttributeSwitchback("city"))
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPending(3)
	expectInt(t, sbDraw("Porto", 1, sbClusterArms), h.at(sbAnchor+sbHour).treatment("exp_sb"))
}

// The cluster is resolved before traffic, so a traffic miss of an eligible
// cluster is recorded under the serving unit, and traffic restored later in
// the period serves the key read first.
func TestSwitchbackCluster_TrafficRestoredWithinThePeriodKeepsTheKey(t *testing.T) {
	var noTraffic = sbAttributeSwitchback("city")
	noTraffic.TrafficSplit = []float64{1, 0}
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(noTraffic))
	h.at(sbAnchor+10).setAttribute("city", "Faro")
	expectInt(t, 0, h.treatment("exp_sb"))
	h.at(sbAnchor+20).setAttribute("city", "Lisboa")
	h.refresh(sbAttributeSwitchback("city"))
	expectInt(t, 1, sbDraw("Faro", 0, sbClusterArms))
	expectInt(t, sbDraw("Faro", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPublished(sbClusterExposure(0, sbAnchor+10, false, false),
		sbClusterExposure(sbDraw("Faro", 0, sbClusterArms), sbAnchor+20, true, false))
}

// An audience change resolves the period slot again (new exposure state)
// but keeps its key.
func TestSwitchbackCluster_AudienceRevalidationAfterSetAttributeKeepsThePeriodsBucket(t *testing.T) {
	var experiment = sbAttributeSwitchback("city")
	experiment.Audience = sbStrictGoldAudience
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	h.at(sbAnchor + 10)
	h.setAttribute("city", "Lisboa")
	h.setAttribute("tier", "gold")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.at(sbAnchor + 20)
	h.setAttribute("city", "Porto")
	h.setAttribute("tier", "silver")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPublished(sbClusterExposure(sbDraw("Lisboa", 0, sbClusterArms), sbAnchor+10, true, false),
		sbClusterExposure(sbDraw("Lisboa", 0, sbClusterArms), sbAnchor+20, true, true))
}

// A slot first resolved without a key carries the missing key into its
// replacement.
func TestSwitchbackCluster_AudienceRevalidationAfterAnIneligibleResolutionKeepsTheMissingKey(t *testing.T) {
	var experiment = sbAttributeSwitchback("city")
	experiment.Audience = sbStrictGoldAudience
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	h.at(sbAnchor+10).setAttribute("tier", "gold")
	expectInt(t, 0, h.treatment("exp_sb"))
	h.at(sbAnchor + 20)
	h.setAttribute("city", "Faro")
	h.setAttribute("tier", "silver")
	expectInt(t, 1, sbDraw("Faro", 0, sbClusterArms))
	expectInt(t, 0, h.treatment("exp_sb"))
	h.expectPending(0)
	expectInt(t, sbDraw("Faro", 1, sbClusterArms), h.at(sbAnchor+sbHour).peek("exp_sb"))
	expectInt(t, 2, sbDraw("Faro", 2, sbClusterArms))
	expectInt(t, sbDraw("Faro", 2, sbClusterArms), h.at(sbAnchor+2*sbHour).peek("exp_sb"))
}

func TestSwitchbackCluster_ClusterPathChangesInvalidateThePeriod(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor + 10)
	h.setAttribute("city", "Lisboa")
	h.setAttribute("geo", map[string]interface{}{"city": "Porto"})
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPending(1)
	h.refresh(sbAttributeSwitchback("city"))
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPending(1)
	h.refresh(sbAttributeSwitchback("geo/city"))
	expectInt(t, sbDraw("Porto", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPending(2)
	var filtered = sbAttributeSwitchback("geo/city")
	filtered.SwitchbackClusterValues = []string{"Porto", "Faro"}
	h.refresh(filtered)
	expectInt(t, sbDraw("Porto", 0, sbClusterArms), h.treatment("exp_sb"))
	h.expectPending(3)
}

// Linking the experiment to another unit type re-keys the period and moves
// the exposure unit.
func TestSwitchbackCluster_LinkedUnitTypeChangeRekeysThePeriod(t *testing.T) {
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID, "user_id": "user-0002"}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor + 1)
	h.setAttribute("city", "Lisboa")
	h.setAttribute("ucity", "Faro")
	expectInt(t, sbDraw("Lisboa", 0, sbClusterArms), h.treatment("exp_sb"))
	var byUser = sbAttributeSwitchback("ucity")
	byUser.SwitchbackClusters = sbClusters("user_id", "ucity")
	h.refresh(byUser)
	expectInt(t, sbDraw("Faro", 0, sbClusterArms), h.treatment("exp_sb"))
	var exposures = h.publishExposures()
	expectInt(t, 2, len(exposures))
	expectValue(t, sbUnitType, exposures[0].Unit)
	expectValue(t, "user_id", exposures[1].Unit)
}

func TestSwitchbackCluster_CustomAssignmentAppliesOnlyToUnitsWithAClusterKey(t *testing.T) {
	var keyed = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	keyed.setAttribute("city", "Lisboa")
	_ = keyed.ctx.SetCustomAssignment("exp_sb", 2)
	expectInt(t, 2, keyed.treatment("exp_sb"))

	var unkeyed = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	_ = unkeyed.ctx.SetCustomAssignment("exp_sb", 2)
	expectInt(t, 0, unkeyed.treatment("exp_sb"))
	unkeyed.expectPending(0)
}

// Inertness: a unit experiment ("unit" or absent) carrying every switchback
// field serves and publishes exactly what the same experiment without them
// does.
func TestSwitchbackCluster_UnitExperimentsIgnoreClusterFields(t *testing.T) {
	for _, randomization := range []string{"unit", "", "future_kind"} {
		var unit = sbAttributeSwitchback("city")
		unit.RandomizationType = randomization
		unit.SwitchbackClusters = sbClusters("user_id", "city", sbUnitType, "'x")
		unit.SwitchbackClusterValues = []string{"nothing"}
		unit.SwitchbackPeriodMs = sbInt64(0)
		// A split rule hashes unitType for a unit experiment, never a
		// cluster unit.
		unit.AssignmentRules = `{"rules":[{"type":"split","environments":[],"percentages":"34/33/33"}]}`
		var reference = sbUnit(unit)
		reference.SwitchbackClusterValues = nil
		reference.SwitchbackCarryoverDurationMs, reference.SwitchbackBurnInDurationMs, reference.SwitchbackBurnOutDurationMs = nil, nil, nil
		reference.SwitchbackDesignType, reference.SwitchbackEstimand = "", ""
		reference.RandomizationType = randomization

		var published [2][]jsonmodels.Exposure
		for e, experiment := range []jsonmodels.Experiment{unit, reference} {
			var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID, "user_id": "u"}, sbData(experiment))
			h.setAttribute("city", "Lisboa")
			h.at(sbAnchor - sbHour)
			var first = h.treatment("exp_sb")
			for i := int64(1); i < 6; i++ {
				expectInt(t, first, h.at(sbAnchor-sbHour+i*sbHour).treatment("exp_sb"))
			}
			published[e] = h.publishExposures()
		}
		expectInt(t, 1, len(published[0]))
		if !published[0][0].Eligible || !published[0][0].RuleOverride || published[0][0].ExposedAt != sbAnchor-sbHour {
			t.Fatalf("%+v", published[0][0])
		}
		expectValue(t, published[1], published[0])
	}
}

// An ineligible or absent switchback slot never evaluates its audience, also
// when a SetAttribute makes it revalidate, and is not resolved again within
// the period.
func TestSwitchbackCluster_IneligibleSlotNeverEvaluatesTheAudience(t *testing.T) {
	var ineligible = sbExperiment(1, "exp_ineligible", 2, sbHour)
	ineligible.Audience = sbStrictGoldAudience
	ineligible.SwitchbackClusterValues = []string{"other-store"}
	var absent = sbExperiment(2, "exp_absent", 2, sbHour)
	absent.Audience = sbStrictGoldAudience
	absent.SwitchbackClusters = sbClusters("device_id", sbClusterPath)
	var h = sbCreate(t, sbData(ineligible, absent))
	var counter = &hoCountingDeserializer{}
	h.ctx.AudienceMatcher_ = AudienceMatcher{counter}
	h.at(sbAnchor + 10)
	for _, name := range []string{"exp_ineligible", "exp_absent"} {
		expectInt(t, 0, h.treatment(name))
		var first = h.ctx.GetAssignment(name)
		h.setAttribute("tier", "gold")
		expectInt(t, 0, h.treatment(name))
		if h.ctx.GetAssignment(name) != first {
			t.Fatalf("%s was resolved again", name)
		}
	}
	expectInt(t, 0, counter.count)
	h.expectPending(0)
}

// A pointer or interface cycle has no JSON encoding, so it yields no cluster
// value instead of hanging the read; a long but finite pointer chain is read
// through.
func TestSwitchbackCluster_PointerAndInterfaceCyclesHaveNoKey(t *testing.T) {
	var self interface{}
	self = &self
	var a, b interface{}
	a, b = &b, &a
	var cyclicInMap interface{}
	cyclicInMap = map[string]interface{}{"city": &cyclicInMap}
	for _, value := range []interface{}{self, a, cyclicInMap} {
		var done = make(chan string, 1)
		go func() { done <- parseSwitchbackPath("geo/city").resolveAttribute(value) }()
		select {
		case key := <-done:
			expectValue(t, "", key)
		case <-time.After(5 * time.Second):
			t.Fatalf("%T: resolveAttribute did not return", value)
		}
		var keyDone = make(chan string, 1)
		go func() { keyDone <- switchbackClusterKey(value) }()
		select {
		case key := <-keyDone:
			expectValue(t, "", key)
		case <-time.After(5 * time.Second):
			t.Fatalf("%T: switchbackClusterKey did not return", value)
		}
	}
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.setAttribute("city", self)
	expectInt(t, 0, h.at(sbAnchor+10).treatment("exp_sb"))
	h.expectPending(0)

	var porto interface{} = "Porto"
	var chain interface{} = &porto
	for i := 0; i < 10; i++ {
		var next = chain
		chain = &next
	}
	expectValue(t, "Porto", parseSwitchbackPath("city").resolveAttribute(chain))
}

type sbPointerCity string

func (c *sbPointerCity) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"city": string(*c)})
}

type sbPointerText string

func (c *sbPointerText) MarshalText() ([]byte, error) {
	return []byte("text-" + string(*c)), nil
}

// A path is read through the attribute's published JSON shape: encoding/json
// encodes an addressable slice element through its pointer type's
// marshaler, so the path reads that encoding. An array or map value element
// is not addressable, and is encoded and read as itself.
func TestSwitchbackCluster_TypedSliceElementsAreReadAsPublished(t *testing.T) {
	for _, c := range []struct {
		path  string
		value interface{}
		key   string
	}{
		{"items/0/city", []sbPointerCity{"Porto"}, "Porto"},
		{"geo/items/0/city", map[string]interface{}{"items": []sbPointerCity{"Faro"}}, "Faro"},
		{"geo/items/0/text", map[string]interface{}{"items": []map[string]sbPointerText{{"text": "x"}}}, "x"},
		{"geo/items/1/city", map[string]interface{}{"items": []sbPointerCity{"Faro", "Beja"}}, "Beja"},
	} {
		var published, _ = json.Marshal(c.value)
		if got := parseSwitchbackPath(c.path).resolveAttribute(c.value); got != c.key {
			t.Errorf("%s over %s: %q, want %q", c.path, published, got, c.key)
		}
	}
	// Through a context: the cluster served is the published one.
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("items/0/city")))
	h.setAttribute("items", []sbPointerCity{"Porto"})
	expectValue(t, sbExpectedSchedule("Porto"), h.peekSchedule("exp_sb", sbHour, 12))
}

// One json.Number attribute (a JSON document decoded with UseNumber) is both
// an exact switchback cluster key and a number to audiences, beyond int64
// too: a strict audience store_number > 99 matches it and the cluster is
// keyed by its digits.
func TestSwitchbackCluster_JSONNumberIsAKeyAndANumber(t *testing.T) {
	for _, digits := range []string{"4000000002", "123456789012345678901234567890"} {
		var experiment = sbAttributeSwitchback("store_number")
		experiment.Audience = `{"filter":[{"gt":[{"var":"store_number"},{"value":99}]}]}`
		experiment.AudienceStrict = true
		experiment.SwitchbackClusterValues = []string{digits}
		var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		h.setAttribute("store_number", json.Number(digits))
		expectInt(t, sbDraw(digits, 0, sbClusterArms), h.at(sbAnchor+5).treatment("exp_sb"))
		h.expectPublished(sbClusterExposure(sbDraw(digits, 0, sbClusterArms), sbAnchor+5, true, false))
	}
}

type sbTextCity struct{ City string }

func (c *sbTextCity) MarshalText() ([]byte, error) {
	return []byte("text-" + c.City), nil
}

func TestSwitchbackCluster_PointerTextMarshalerSliceUsesPublishedShape(t *testing.T) {
	var value = map[string]interface{}{"items": []sbTextCity{{City: "Porto"}}}
	var encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	expectValue(t, `{"items":["text-Porto"]}`, string(encoded))
	expectValue(t, "", parseSwitchbackPath("geo/items/0/City").resolveAttribute(value))
}

// A value with its own JSON encoding is read from that encoding, as the
// collector reads the published attribute: a struct's whole float64 field
// is published as 3 and keys a cluster, while the same float in a map or as
// the attribute itself is a float and gives none.
func TestSwitchbackCluster_StructFieldsAreReadFromTheirEncoding(t *testing.T) {
	type geo struct{ City float64 }
	expectValue(t, "3", parseSwitchbackPath("geo/City").resolveAttribute(geo{City: 3}))
	expectValue(t, "3", parseSwitchbackPath("geo/0/City").resolveAttribute([]geo{{City: 3}}))
	expectValue(t, "", parseSwitchbackPath("geo/City").resolveAttribute(geo{City: 3.5}))
	expectValue(t, "", parseSwitchbackPath("geo/City").resolveAttribute(map[string]interface{}{"City": 3.0}))
	expectValue(t, "", parseSwitchbackPath("geo").resolveAttribute(3.0))
	// A map with non-string keys is encoded with string keys.
	expectValue(t, "Porto", parseSwitchbackPath("geo/'1'").resolveAttribute(map[int]string{1: "Porto"}))
}

// Changing a cluster attribute mid-period is not supported: the arm cached
// at the period's first read stays for the period, even when that read was
// a peek with no exposure yet, or when its exposure was rejected and is
// queued again. The change takes effect from the next period.
func TestSwitchbackCluster_FirstReadFixesThePeriodsArmBeforeAnyExposure(t *testing.T) {
	var lisboa, faro = sbDraw("Lisboa", 0, sbClusterArms), sbDraw("Faro", 0, sbClusterArms)
	if lisboa == faro {
		t.Fatal("fixture: Lisboa and Faro agree in period 0")
	}
	// A peek fixes the arm before the exposure.
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor+10).setAttribute("city", "Lisboa")
	expectInt(t, lisboa, h.peek("exp_sb"))
	h.at(sbAnchor+20).setAttribute("city", "Faro")
	expectInt(t, lisboa, h.treatment("exp_sb"))
	h.expectPublished(sbClusterExposure(lisboa, sbAnchor+20, true, false))
	expectInt(t, sbDraw("Faro", 1, sbClusterArms), h.at(sbAnchor+sbHour).peek("exp_sb"))

	// An exposure rejected by a close, queued again after the change, keeps
	// the arm.
	h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("city")))
	h.at(sbAnchor+10).setAttribute("city", "Lisboa")
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp_sb"))
	h.ctx.Closing_.Store(false)
	h.expectPending(0)
	h.at(sbAnchor+20).setAttribute("city", "Faro")
	expectInt(t, lisboa, h.treatment("exp_sb"))
	h.expectPublished(sbClusterExposure(lisboa, sbAnchor+20, true, false))
}

// sbFuncCity cannot be encoded as itself (a func field), but its pointer
// marshaler encodes it, as encoding/json does for an addressable element.
type sbFuncCity struct {
	Name string
	Hook func()
}

func (c *sbFuncCity) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"city": c.Name})
}

// sbFailingPointerCity encodes as itself, but its pointer marshaler fails,
// so a slice holding it cannot be published.
type sbFailingPointerCity string

func (c *sbFailingPointerCity) MarshalJSON() ([]byte, error) {
	return nil, errors.New("unencodable")
}

// Validation reads an addressable element through its pointer marshaler,
// as encoding/json publishes it: an element encodable only through that
// marshaler yields its key, and an element whose marshaler fails makes the
// whole attribute unencodable, so it yields none.
func TestSwitchbackCluster_ValidationUsesThePublishedElementEncoding(t *testing.T) {
	var valid = []sbFuncCity{{Name: "Porto", Hook: func() {}}}
	if _, err := json.Marshal(valid); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	expectValue(t, "Porto", parseSwitchbackPath("items/0/city").resolveAttribute(valid))
	expectValue(t, "Porto", parseSwitchbackPath("geo/items/0/city").resolveAttribute(map[string]interface{}{"items": valid}))
	expectValue(t, "Porto", parseSwitchbackPath("geo/Items/0/city").resolveAttribute(&struct{ Items []sbFuncCity }{valid}))

	var failing = []interface{}{"Porto", []sbFailingPointerCity{"x"}}
	if _, err := json.Marshal(failing); err == nil {
		t.Fatal("fixture: the failing element encoded")
	}
	expectValue(t, "", parseSwitchbackPath("items/0").resolveAttribute(failing))
	expectValue(t, "", parseSwitchbackPath("geo/city").resolveAttribute(map[string]interface{}{"city": "Porto",
		"other": []sbFailingPointerCity{"x"}}))
	// Not addressable, so encoded as itself: a map value or an array held in
	// an interface.
	expectValue(t, "Porto", parseSwitchbackPath("geo/city").resolveAttribute(map[string]interface{}{"city": "Porto",
		"other": map[string]sbFailingPointerCity{"k": "x"}, "arr": [1]sbFailingPointerCity{"x"}}))

	// Through a context: the published key serves.
	var h = sbCreateBare(t, map[string]string{sbUnitType: sbUID}, sbData(sbAttributeSwitchback("items/0/city")))
	h.setAttribute("items", valid)
	expectValue(t, sbExpectedSchedule("Porto"), h.peekSchedule("exp_sb", sbHour, 12))
}
