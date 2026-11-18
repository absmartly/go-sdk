package sdk

// Port of the Collector's ContextAPISwitchbackTest ClusterRule section
// (absmartly/abs 9bd2b5ca35) through the public Context API, as the Java
// SDK's ContextSwitchbackClusterRuleTest (java-sdk main 07807b5) ports it.
// The switchback "sbc" (id 900, 34/33/33, one hour) is linked to user_id at
// "profile/store" and to session_id at "session_store", declared in unit
// type id order, so session_id comes first.
//
// A context holds one uid per unit type and keeps its units in a map, so the
// declared source order alone decides which unit serves.

import (
	"strconv"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

const (
	crPeriod  int64 = 5
	crNow           = sbAnchor + crPeriod*sbHour + 17*60_000
	crSeedHi        = 0x1234
	crSeedLo        = 0x5678abcd
	crSession       = "session_id"
	crUser          = "user_id"
)

func crExperiment(clusters jsonmodels.SwitchbackClusters) jsonmodels.Experiment {
	var variants = make([]jsonmodels.ExperimentVariant, 3)
	for i := range variants {
		variants[i] = jsonmodels.ExperimentVariant{Name: strconv.Itoa(i), Config: `{"sbc.config":"test config ` + strconv.Itoa(i) + `"}`}
	}
	return jsonmodels.Experiment{Id: 900, Name: "sbc", UnitType: crUser, Iteration: 1, SeedHi: crSeedHi, SeedLo: crSeedLo,
		Split: []float64{0.34, 0.33, 0.33}, TrafficSeedHi: 7, TrafficSeedLo: 11, TrafficSplit: []float64{0, 1},
		Applications: []jsonmodels.ExperimentApplication{{Name: "website"}}, Variants: variants,
		RandomizationType: "switchback", SwitchbackClusters: clusters, SwitchbackStartAt: sbInt64(sbAnchor),
		SwitchbackPeriodMs: sbInt64(sbHour), SwitchbackDesignType: "balanced_block"}
}

func crDefault() jsonmodels.Experiment {
	return crExperiment(sbClusters(crSession, "session_store", crUser, "profile/store"))
}

func crScheduled(key string) int {
	var variant, _ = SwitchbackAssign(key, crPeriod, int64(switchbackSeed(crSeedHi, crSeedLo)), 3)
	return variant
}

func crStore(value interface{}) map[string]interface{} {
	return map[string]interface{}{"store": value}
}

// crAssigned is the Collector's assignment for "sbc", or nil when it lists
// none (the experiment does not apply).
type crAssigned struct {
	unit         string
	eligible     bool
	silent       bool
	variant      int
	fullOn       bool
	ruleOverride bool
	config       interface{}
	exposures    []jsonmodels.Exposure
}

// crResolve creates a context with the attributes and units, resolves every
// other experiment first, then "sbc" through GetTreatment and
// GetVariableValue, and reports what the Collector would list for it.
func crResolve(t *testing.T, attributes map[string]interface{}, experiments []jsonmodels.Experiment, units map[string]string) *crAssigned {
	t.Helper()
	var h = sbCreateAt(t, crNow, units, sbData(experiments...), sbNoClusterAttribute())
	for name, value := range attributes {
		h.setAttribute(name, value)
	}
	for _, other := range experiments {
		if other.Name != "sbc" {
			h.treatment(other.Name)
		}
	}
	var variant = h.treatment("sbc")
	var entry = h.ctx.getAssignment("sbc")
	var config, _ = h.ctx.GetVariableValue("sbc.config", nil)
	var exposures []jsonmodels.Exposure
	for _, exposure := range h.publishExposures() {
		if exposure.Name == "sbc" {
			exposures = append(exposures, exposure)
		}
	}
	if entry.switchback == nil {
		t.Fatal("sbc resolved as a unit experiment")
	}
	if entry.switchback.serving.status == switchbackAbsent {
		if variant != 0 || config != nil || len(exposures) != 0 {
			t.Fatalf("not applying, but variant %d, config %v, exposures %+v", variant, config, exposures)
		}
		return nil
	}
	var assignment = entry.assignment
	return &crAssigned{unit: assignment.UnitType, eligible: assignment.Eligible, silent: entry.switchback.silent,
		variant: variant, fullOn: assignment.FullOn, ruleOverride: assignment.RuleOverride, config: config, exposures: exposures}
}

func crSbc(t *testing.T, experiment jsonmodels.Experiment, attributes map[string]interface{}, units map[string]string) *crAssigned {
	t.Helper()
	return crResolve(t, attributes, []jsonmodels.Experiment{experiment}, units)
}

// crExpectIneligible: an ineligible cluster is still an assignment, as
// control under the first linked unit in declared order, and records nothing.
func crExpectIneligible(t *testing.T, experiment jsonmodels.Experiment, attributes map[string]interface{}, unit string, units map[string]string) {
	t.Helper()
	var assignment = crSbc(t, experiment, attributes, units)
	if assignment == nil || assignment.unit != unit || assignment.eligible || !assignment.silent || assignment.variant != 0 ||
		assignment.fullOn || assignment.ruleOverride || assignment.config != "test config 0" || len(assignment.exposures) != 0 {
		t.Fatalf("%v: %+v, want an ineligible control under %s", attributes, assignment, unit)
	}
}

func TestSwitchbackClusterRule_MissingOrUnreadableClusterConfigurationReturnsOnlyLegacyIneligibleControl(t *testing.T) {
	var attributes = map[string]interface{}{"geo": map[string]interface{}{"city": "Porto"}, "geo.city": "Faro", "session_store": "s-1"}
	for _, clusters := range []jsonmodels.SwitchbackClusters{nil, {}, sbClusters(crSession, "'geo"), sbClusters(crSession, "geo.city"),
		sbClusters(crSession, "'geo.city'"), sbClusters(crSession, `"geo.city"`)} {
		var experiment = crExperiment(clusters)
		// The declared unit (user_id) only identifies an ineligible control.
		crExpectIneligible(t, experiment, attributes, crUser, map[string]string{crSession: "x1", crUser: "u1"})
		// Without a unit of the declared type there is no assignment at all.
		if assignment := crSbc(t, experiment, attributes, map[string]string{crSession: "x1"}); assignment != nil {
			t.Fatalf("%v: %+v", clusters, assignment)
		}
	}
	// A readable map whose only linked type is absent from the context.
	if assignment := crSbc(t, crExperiment(sbClusters("device_id", "session_store")), map[string]interface{}{"session_store": "s-1"},
		map[string]string{crSession: "x1", crUser: "u1"}); assignment != nil {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_TrafficUsesServingUidNotExperimentUid(t *testing.T) {
	var experiment = crDefault()
	experiment.TrafficSplit = []float64{0.6, 0.4}
	var ordinary = sbUnit(crExperiment(nil))
	ordinary.Id, ordinary.Name, ordinary.Split = 901, "ordinary", []float64{0.5, 0.5}
	ordinary.Variants = []jsonmodels.ExperimentVariant{{Name: "0"}, {Name: "1"}}
	var witnesses [2]string
	for i := 0; i < 1000 && (witnesses[0] == "" || witnesses[1] == ""); i++ {
		var uid = "traffic-" + strconv.Itoa(i)
		var buf [12]int8
		witnesses[NewVariantAssigner(HashUnit(uid)).Assign([]float64{0.6, 0.4}, 7, 11, buf[:])] = uid
	}
	var key = "s-18"
	if crScheduled(key) == 0 {
		t.Fatal("fixture: the draw is control")
	}
	for _, servingEligible := range []bool{false, true} {
		var serving, other = witnesses[0], witnesses[1]
		if servingEligible {
			serving, other = other, serving
		}
		var assignment = crResolve(t, map[string]interface{}{"session_store": key, "profile": crStore(key)},
			[]jsonmodels.Experiment{experiment, ordinary}, map[string]string{crSession: serving, crUser: other})
		var want = 0
		if servingEligible {
			want = crScheduled(key)
		}
		if assignment.unit != crSession || assignment.eligible != servingEligible || assignment.variant != want ||
			len(assignment.exposures) != 1 || assignment.exposures[0].Unit != crSession || assignment.exposures[0].Eligible != servingEligible {
			t.Fatalf("serving eligible %v: %+v", servingEligible, assignment)
		}
	}
}

func TestSwitchbackClusterRule_OneMatchingUnitServesItsValue(t *testing.T) {
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"profile": crStore("s-17")}, map[string]string{crUser: "u1"})
	if !assignment.eligible || assignment.unit != crUser || assignment.variant != crScheduled("s-17") {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_StrictAudienceMismatchOnAnEligibleClusterIsListedAsControlAndRecorded(t *testing.T) {
	var experiment = crDefault()
	experiment.Audience, experiment.AudienceStrict = sbStrictGoldAudience, true
	var assignment = crSbc(t, experiment, map[string]interface{}{"session_store": "s-17"}, map[string]string{crSession: "x1"})
	if assignment == nil || assignment.unit != crSession || !assignment.eligible || assignment.silent || assignment.variant != 0 ||
		len(assignment.exposures) != 1 || !assignment.exposures[0].AudienceMismatch || assignment.exposures[0].Assigned {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_TwoAgreeingUnitsServeTheValue(t *testing.T) {
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"profile": crStore("s-17"), "session_store": "s-17"},
		map[string]string{crUser: "u1", crSession: "x1"})
	if !assignment.eligible || assignment.variant != crScheduled("s-17") {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_TwoDisagreeingUnitsAreIneligible(t *testing.T) {
	crExpectIneligible(t, crDefault(), map[string]interface{}{"profile": crStore("s-17"), "session_store": "s-18"}, crSession,
		map[string]string{crUser: "u1", crSession: "x1"})
}

// The Collector serves two session_id units agreeing on one value; a context
// holds one uid per unit type, so the second is rejected.
func TestSwitchbackClusterRule_ASecondUidOfOneTypeIsRejected(t *testing.T) {
	var h = sbCreateAt(t, crNow, map[string]string{crSession: "x1"}, sbData(crDefault()))
	if err := h.ctx.SetUnit(crSession, "x2"); err == nil {
		t.Fatal("a second session_id uid was accepted")
	}
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"session_store": "s-17"}, map[string]string{crSession: "x1"})
	if !assignment.eligible || assignment.variant != crScheduled("s-17") {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_NoMatchingUnitDoesNotApply(t *testing.T) {
	for _, units := range []map[string]string{{"device_id": "d1"}, nil} {
		if assignment := crSbc(t, crDefault(), map[string]interface{}{"session_store": "s-1"}, units); assignment != nil {
			t.Fatalf("%v: %+v", units, assignment)
		}
	}
}

func TestSwitchbackClusterRule_MatchingUnitWithoutAValueIsIneligible(t *testing.T) {
	crExpectIneligible(t, crDefault(), map[string]interface{}{"other": "s-1"}, crUser, map[string]string{crUser: "u1"})
	crExpectIneligible(t, crDefault(), map[string]interface{}{"other": "s-1"}, crSession, map[string]string{crUser: "u1", crSession: "x1"})
}

func TestSwitchbackClusterRule_NonStringNonIntegerValuesAreIneligible(t *testing.T) {
	for _, value := range []interface{}{1.5, 3.0, true, map[string]interface{}{"a": 1}, []interface{}{"a"}, ""} {
		crExpectIneligible(t, crDefault(), map[string]interface{}{"session_store": value}, crSession, map[string]string{crSession: "x1"})
	}
}

func TestSwitchbackClusterRule_WholeNumbersAreKeyedByTheirDecimalText(t *testing.T) {
	if crScheduled("17") == 0 {
		t.Fatal("fixture: 17 draws control")
	}
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"session_store": 17}, map[string]string{crSession: "x1"})
	if !assignment.eligible || assignment.variant != crScheduled("17") {
		t.Fatalf("%+v", assignment)
	}
	// Equal to the string "17" from another unit type, so the two agree.
	if !crSbc(t, crDefault(), map[string]interface{}{"session_store": 17, "profile": crStore("17")},
		map[string]string{crSession: "x1", crUser: "u1"}).eligible {
		t.Fatal("17 and \"17\" disagree")
	}
}

func TestSwitchbackClusterRule_NestedAndArrayPathsAreRead(t *testing.T) {
	var assignment = crSbc(t, crExperiment(sbClusters(crSession, "items/1/store")),
		map[string]interface{}{"items": []interface{}{crStore("a"), crStore("b")}}, map[string]string{crSession: "x1"})
	if assignment.variant != crScheduled("b") || crScheduled("a") == crScheduled("b") {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_ExposureUnitIsTheFirstMatchingUnitInDeclaredOrder(t *testing.T) {
	var key = "s-18"
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"profile": crStore(key), "session_store": key},
		map[string]string{crUser: "u1", crSession: "x1"})
	if assignment.unit != crSession || assignment.exposures[0].Unit != crSession || assignment.variant != crScheduled(key) {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_IneligibleClusterNeverPublishesAnExposure(t *testing.T) {
	var h = sbCreateAt(t, crNow, map[string]string{crUser: "u1", crSession: "x1"}, sbData(crDefault()), sbNoClusterAttribute())
	h.setAttribute("session_store", "a")
	h.setAttribute("profile", crStore("b"))
	expectInt(t, 0, h.treatment("sbc"))
	h.expectPending(0)
	expectInt(t, 0, len(h.publishEvents()))
}

func TestSwitchbackClusterRule_AUnitWithoutAValueDoesNotServeEvenIfFirstInDeclaredOrder(t *testing.T) {
	var key = "s-17"
	var assignment = crSbc(t, crDefault(), map[string]interface{}{"profile": crStore(key)}, map[string]string{crSession: "x1", crUser: "u1"})
	if assignment.unit != crUser || assignment.exposures[0].Unit != crUser || assignment.variant != crScheduled(key) {
		t.Fatalf("%+v", assignment)
	}
}

func TestSwitchbackClusterRule_ValueNotInTheFilterIsIneligible(t *testing.T) {
	var experiment = crDefault()
	experiment.SwitchbackClusterValues = []string{"s-1", "s-2"}
	crExpectIneligible(t, experiment, map[string]interface{}{"session_store": "s-3"}, crSession, map[string]string{crSession: "x1"})
	var allowed = crSbc(t, experiment, map[string]interface{}{"session_store": "s-2"}, map[string]string{crSession: "x1"})
	if !allowed.eligible || allowed.variant != crScheduled("s-2") {
		t.Fatalf("%+v", allowed)
	}
}

// A nil or empty allow-list admits every value.
func TestSwitchbackClusterRule_NullOrEmptyFilterAssignsEveryValue(t *testing.T) {
	for _, values := range [][]string{nil, {}} {
		var experiment = crDefault()
		experiment.SwitchbackClusterValues = values
		for i := 0; i < 20; i++ {
			var key = "s-" + strconv.Itoa(i)
			var assignment = crSbc(t, experiment, map[string]interface{}{"session_store": key}, map[string]string{crSession: "x1"})
			if !assignment.eligible || assignment.variant != crScheduled(key) {
				t.Fatalf("%v %s: %+v", values, key, assignment)
			}
		}
	}
}

// A valid cluster map whose linked types are all absent does not fall back
// to the experiment's declared unitType.
func TestSwitchbackClusterRule_ValidClusterWithoutLinkedUnitDoesNotFallBackToExperimentUnit(t *testing.T) {
	var experiment = crExperiment(sbClusters(crSession, "session_store"))
	var key = "s-17"
	if assignment := crSbc(t, experiment, map[string]interface{}{"session_store": key, "profile": crStore(key)},
		map[string]string{crUser: "u1"}); assignment != nil {
		t.Fatalf("%+v", assignment)
	}
	var linked = crSbc(t, experiment, map[string]interface{}{"session_store": key}, map[string]string{crUser: "u1", crSession: "x1"})
	if linked.unit != crSession || linked.variant != crScheduled(key) {
		t.Fatalf("%+v", linked)
	}
}

// crOracle is the Collector's wire-rule oracle SdkSwitchbackRule, as the Java
// test transcribes it: the period from the anchor (re-ceiled to the hour),
// traffic on the serving uid's hash, then the draw over the cluster key. An
// empty key has no draw. It returns the variant and whether it is eligible.
func crOracle(experiment jsonmodels.Experiment, uid string, key string, now int64) (int, bool) {
	if experiment.SwitchbackStartAt == nil || experiment.SwitchbackPeriodMs == nil || *experiment.SwitchbackPeriodMs <= 0 ||
		len(experiment.Split) < 2 || len(key) == 0 {
		return 0, false
	}
	var anchor = -floorDiv(-*experiment.SwitchbackStartAt, sbHour) * sbHour
	if now < anchor {
		return 0, false
	}
	var buf [12]int8
	if NewVariantAssigner(HashUnit(uid)).Assign(experiment.TrafficSplit, experiment.TrafficSeedHi, experiment.TrafficSeedLo, buf[:]) != 1 {
		return 0, false
	}
	var variant, _ = SwitchbackAssign(key, floorDiv(now-anchor, *experiment.SwitchbackPeriodMs),
		int64(switchbackSeed(experiment.SeedHi, experiment.SeedLo)), len(experiment.Split))
	return variant, true
}

func TestSwitchbackClusterRule_ContextMatchesTheWireRuleParityGrid(t *testing.T) {
	var eligible, ineligible, trafficMisses = 0, 0, 0
	type clusterCase int
	const (
		single clusterCase = iota
		disagreeing
		missingValue
		filteredValue
	)
	for _, cluster := range []clusterCase{single, disagreeing, missingValue, filteredValue} {
		for _, anchor := range []int64{sbAnchor, sbAnchor + 7*sbHour} {
			for _, periodMs := range []int64{sbHour, 2 * sbHour, 24 * sbHour} {
				var experiment = crExperiment(sbClusters(crSession, "session_store"))
				experiment.TrafficSplit = []float64{0.3, 0.7}
				experiment.SwitchbackStartAt, experiment.SwitchbackPeriodMs = sbInt64(anchor), sbInt64(periodMs)
				for u := 0; u < 6; u++ {
					var uid = "uid-" + strconv.Itoa(u)
					for _, key := range []string{"s-1", "s-2", "42", "Lisboa"} {
						for _, now := range []int64{anchor - sbHour, anchor - 1, anchor, anchor + periodMs - 1, anchor + periodMs,
							anchor + 3*periodMs + 5, anchor + 11*periodMs} {
							var data = experiment
							var units = map[string]string{crSession: uid}
							var attributes = map[string]interface{}{"session_store": key}
							switch cluster {
							case disagreeing:
								data.SwitchbackClusters = sbClusters(crSession, "session_store", crUser, "profile/store")
								units[crUser] = "u-" + uid
								attributes["profile"] = crStore(key + "-other")
							case missingValue:
								attributes = map[string]interface{}{"other": key}
							case filteredValue:
								data.SwitchbackClusterValues = []string{key + "-allowed"}
							}
							var oracleKey = ""
							if cluster == single {
								oracleKey = key
							}
							var wantVariant, wantEligible = crOracle(data, uid, oracleKey, now)
							var h = sbCreateAt(t, now, units, sbData(data), sbNoClusterAttribute())
							for name, value := range attributes {
								h.setAttribute(name, value)
							}
							var got = h.treatment("sbc")
							var resolved = h.ctx.getAssignment("sbc").assignment
							var exposures = h.publishExposures()
							var wantExposures = 0
							if len(oracleKey) > 0 && now >= anchor {
								wantExposures = 1
							}
							if got != wantVariant || resolved.Eligible != wantEligible || len(exposures) != wantExposures ||
								(wantExposures == 1 && exposures[0].Eligible != wantEligible) {
								t.Fatalf("case %d anchor %d period %d %s %s now %d: variant %d eligible %v exposures %+v; want %d %v",
									cluster, anchor, periodMs, uid, key, now, got, resolved.Eligible, exposures, wantVariant, wantEligible)
							}
							if cluster == single {
								if wantEligible {
									eligible++
								} else {
									ineligible++
									if now >= anchor {
										trafficMisses++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if eligible <= 100 || ineligible <= 100 || trafficMisses == 0 {
		t.Fatalf("eligible %d, ineligible %d, traffic misses %d", eligible, ineligible, trafficMisses)
	}

	// The deliberate divergence: an anchor off the hour is unservable in the
	// SDK (as in the Registry), while the oracle re-ceils it and draws.
	var experiment = crExperiment(sbClusters(crSession, "session_store"))
	experiment.SwitchbackStartAt = sbInt64(sbAnchor + 7*sbHour + 1)
	var now = sbAnchor + 8*sbHour + 5
	if _, ok := crOracle(experiment, "uid-0", "s-1", now); !ok {
		t.Fatal("the oracle does not draw")
	}
	var h = sbCreateAt(t, now, map[string]string{crSession: "uid-0"}, sbData(experiment), sbNoClusterAttribute())
	h.setAttribute("session_store", "s-1")
	expectInt(t, 0, h.treatment("sbc"))
	if h.ctx.getAssignment("sbc").assignment.Eligible {
		t.Fatal("an unaligned anchor is eligible")
	}
	h.expectPending(0)
}
