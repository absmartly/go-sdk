package sdk

// Switchback context data decoding, ported from the Java SDK's
// DefaultContextDataDeserializerTest and ExperimentSwitchbackFieldsTest
// (java-sdk main 07807b5), and the guarantees for payloads without
// switchbacks: nothing new is published, allocated or created.
// testAssets/switchback_context.json is a copy of java-sdk
// core-api/src/test/resources/switchback_context.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

func TestSwitchbackDeserialize_AllFieldsInDeclaredOrder(t *testing.T) {
	var raw, err = os.ReadFile("testAssets/switchback_context.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := DefaultContextDataDeserializer{}.Deserialize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var switchback = jsonmodels.Experiment{
		Id: 1, Name: "exp_test_switchback", UnitType: "store_id", Iteration: 1, SeedHi: 3603515, SeedLo: 233373850,
		Split: []float64{0.5, 0.5}, TrafficSeedHi: 449867249, TrafficSeedLo: 455443629, TrafficSplit: []float64{0, 1},
		Applications:      []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants:          []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B", Config: `{"banner.border":1}`}},
		RandomizationType: "switchback", SwitchbackClusters: sbClusters("store_id", "store/key", "device_id", "geo/city"),
		SwitchbackClusterValues: []string{"porto", "lisbon"}, SwitchbackStartAt: sbInt64(1767225600000),
		SwitchbackPeriodMs: sbInt64(3600000), SwitchbackCarryoverDurationMs: sbInt64(900000),
		SwitchbackBurnInDurationMs: sbInt64(600000), SwitchbackBurnOutDurationMs: sbInt64(0),
		SwitchbackDesignType: "balanced_block", SwitchbackEstimand: "global_average_treatment_effect",
	}
	expectInt(t, 2, len(data.Experiments))
	expectValue(t, switchback, data.Experiments[0])
	expectValue(t, "unit", data.Experiments[1].RandomizationType)
	expectValue(t, jsonmodels.SwitchbackClusters(nil), data.Experiments[1].SwitchbackClusters)
}

func TestSwitchbackDeserialize_WithoutSwitchbackFieldsLeavesThemUnset(t *testing.T) {
	var raw, _ = os.ReadFile("testAssets/context.json")
	var data, err = DefaultContextDataDeserializer{}.Deserialize(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, experiment := range data.Experiments {
		var unset = experiment
		unset.RandomizationType, unset.SwitchbackClusters, unset.SwitchbackClusterValues = "", nil, nil
		unset.SwitchbackStartAt, unset.SwitchbackPeriodMs = nil, nil
		unset.SwitchbackCarryoverDurationMs, unset.SwitchbackBurnInDurationMs, unset.SwitchbackBurnOutDurationMs = nil, nil, nil
		unset.SwitchbackDesignType, unset.SwitchbackEstimand = "", ""
		expectValue(t, unset, experiment)
	}
}

// A switchback field of the wrong type drops only that field, never the
// payload: a bad start or period makes the schedule unservable, a bad map
// takes the legacy path, a bad type makes a unit experiment, and a bad
// allow-list allows nothing.
func TestSwitchbackDeserialize_WronglyTypedFieldsDropOnlyThatField(t *testing.T) {
	var base = `"id":1,"name":"exp_sb","unitType":"store_id","iteration":1,"seedHi":-5,"seedLo":-7,"split":[0.5,0.5],
		"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}]`
	for fields, check := range map[string]func(jsonmodels.Experiment) bool{
		`"randomizationType":1`: func(e jsonmodels.Experiment) bool { return e.RandomizationType == "" },
		`"randomizationType":"switchback","switchbackStartAt":"1767225600000"`:      func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackStartAt":1767225600000.5`:                                       func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackStartAt":1.7672256e12`:                                          func(e jsonmodels.Experiment) bool { return *e.SwitchbackStartAt == 1767225600000 },
		`"switchbackStartAt":null`:                                                  func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackStartAt":3600000.0000000001`:                                    func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackStartAt":3600000.000`:                                           func(e jsonmodels.Experiment) bool { return *e.SwitchbackStartAt == 3600000 },
		`"switchbackPeriodMs":1.00000000000000001`:                                  func(e jsonmodels.Experiment) bool { return e.SwitchbackPeriodMs == nil },
		`"switchbackPeriodMs":36e5`:                                                 func(e jsonmodels.Experiment) bool { return *e.SwitchbackPeriodMs == 3600000 },
		`"switchbackPeriodMs":36e0005`:                                              func(e jsonmodels.Experiment) bool { return e.SwitchbackPeriodMs == nil },
		`"switchbackStartAt":1e999999`:                                              func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackStartAt":3600000.000000000000000000000000000000000000000000000`: func(e jsonmodels.Experiment) bool { return e.SwitchbackStartAt == nil },
		`"switchbackPeriodMs":{"a":1}`:                                              func(e jsonmodels.Experiment) bool { return e.SwitchbackPeriodMs == nil },
		`"switchbackPeriodMs":99999999999999999999`:                                 func(e jsonmodels.Experiment) bool { return e.SwitchbackPeriodMs == nil },
		`"switchbackClusters":["store_id"]`:                                         func(e jsonmodels.Experiment) bool { return e.SwitchbackClusters == nil },
		`"switchbackClusters":{"store_id":{"a":1}}`:                                 func(e jsonmodels.Experiment) bool { return e.SwitchbackClusters == nil },
		// A repeated key keeps its first position and its last value, so an
		// overwritten malformed value leaves the map readable.
		`"switchbackClusters":{"store_id":null,"device_id":"store_key","store_id":"store_key"}`: func(e jsonmodels.Experiment) bool {
			return reflect.DeepEqual(e.SwitchbackClusters, jsonmodels.SwitchbackClusters{{UnitType: "store_id", Path: "store_key"}, {UnitType: "device_id", Path: "store_key"}})
		},
		`"switchbackClusters":{"store_id":"store_key","store_id":null}`: func(e jsonmodels.Experiment) bool { return e.SwitchbackClusters == nil },
		`"switchbackClusterValues":"porto"`:                             func(e jsonmodels.Experiment) bool { return reflect.DeepEqual(e.SwitchbackClusterValues, []string{""}) },
		`"switchbackClusterValues":["a",7,true,null,{}]`: func(e jsonmodels.Experiment) bool {
			return reflect.DeepEqual(e.SwitchbackClusterValues, []string{"a", "7", "true", "", ""})
		},
		`"switchbackClusterValues":null`:     func(e jsonmodels.Experiment) bool { return e.SwitchbackClusterValues == nil },
		`"switchbackClusterValues":[]`:       func(e jsonmodels.Experiment) bool { return reflect.DeepEqual(e.SwitchbackClusterValues, []string{}) },
		`"switchbackDesignType":["x"]`:       func(e jsonmodels.Experiment) bool { return e.SwitchbackDesignType == "" },
		`"switchbackEstimand":5`:             func(e jsonmodels.Experiment) bool { return e.SwitchbackEstimand == "" },
		`"switchbackBurnInDurationMs":"600"`: func(e jsonmodels.Experiment) bool { return e.SwitchbackBurnInDurationMs == nil },
	} {
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(`{"experiments":[{` + base + `,` + fields + `}]}`))
		if err != nil || len(data.Experiments) != 1 || data.Experiments[0].Name != "exp_sb" {
			t.Fatalf("%s: %v, %+v", fields, err, data)
		}
		if !check(data.Experiments[0]) {
			t.Errorf("%s: %+v", fields, data.Experiments[0])
		}
	}
}

// A context with a malformed switchback is served: an allow-list that is
// not an array allows nothing, so the experiment is an ineligible control.
func TestSwitchbackDeserialize_MalformedAllowListIsServedAsIneligible(t *testing.T) {
	var payload = `{"experiments":[{"id":1,"name":"exp_sb","unitType":"store_id","iteration":1,"seedHi":-5,"seedLo":-7,
		"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],
		"randomizationType":"switchback","switchbackClusters":{"store_id":"store_key"},"switchbackClusterValues":"store-42",
		"switchbackStartAt":1767225600000,"switchbackPeriodMs":3600000}]}`
	var data, _ = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	var h = sbCreate(t, data)
	expectInt(t, 0, h.at(sbAnchor+sbHour).treatment("exp_sb"))
	h.expectPending(0)
}

// Forward compatibility: unknown fields inside a switchback experiment and
// its cluster map shapes from Phase 1E are ignored.
func TestSwitchbackDeserialize_UnknownAndPhase1EFieldsAreIgnored(t *testing.T) {
	var payload = `{"experiments":[{"id":1,"name":"exp_sb","unitType":"store_id","iteration":1,"seedHi":-5,"seedLo":-7,
		"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],
		"randomizationType":"switchback","switchbackClusters":{"store_id":"store_key"},"switchbackStartAt":1767225600000,
		"switchbackPeriodMs":3600000,"switchbackClusterUnitType":"user_id","switchbackClusterCount":50,
		"switchbackClusterAttribute":"geo/city","switchback":{"period":60},"futureSwitchback":{"periodMinutes":5,"clusters":[1]},
		"switchbackFutureField":{"nested":[1,2,3]}}],"futureRoot":[1]}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var h = sbCreate(t, data)
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour).treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour, true, true))
}

// The standard decoder (json.Unmarshal into the public model) decodes the
// switchback fields too, cluster order included, with no custom
// UnmarshalJSON on Experiment.
func TestSwitchbackDeserialize_StandardDecoderReadsTheModel(t *testing.T) {
	var raw, _ = os.ReadFile("testAssets/switchback_context.json")
	var data jsonmodels.ContextData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	expectValue(t, sbClusters("store_id", "store/key", "device_id", "geo/city"), data.Experiments[0].SwitchbackClusters)
	expectValue(t, int64(3600000), *data.Experiments[0].SwitchbackPeriodMs)
	// The decoder looks up UnmarshalJSON on the pointer's method set.
	if _, ok := reflect.TypeOf(&jsonmodels.Experiment{}).MethodByName("UnmarshalJSON"); ok {
		t.Fatal("Experiment has an UnmarshalJSON method")
	}
}

// A payload without switchbacks decodes with no more allocations than the
// fields it has, and a context serving it never creates switchback state.
func TestSwitchback_PayloadWithoutSwitchbacksCostsNothing(t *testing.T) {
	var raw, _ = os.ReadFile("testAssets/context.json")
	var withoutFields = func() {
		var wire wireExperiment
		wire.switchbackFields(&wire.Experiment)
	}
	if n := hoLeastAllocs(withoutFields); n != 0 {
		t.Errorf("absent switchback fields: %.0f allocations", n)
	}
	var data, _ = DefaultContextDataDeserializer{}.Deserialize(raw)
	var h = hoCreateFor(t, hoUID, data)
	for _, experiment := range data.Experiments {
		h.treatment(experiment.Name)
		h.peek(experiment.Name)
	}
	var keys, _ = h.ctx.GetVariableKeys()
	for key := range keys {
		h.variable(key)
	}
	h.ctx.ContextLock_.RLock()
	defer h.ctx.ContextLock_.RUnlock()
	if h.ctx.state().switchbackEpochs_ != nil || h.ctx.state().switchbackGeneration_ != 0 {
		t.Fatal("switchback state created without switchbacks")
	}
	for name, indexed := range h.ctx.state().loadIndex().experiments {
		if indexed.switchback != nil {
			t.Fatalf("%s has a switchback definition", name)
		}
	}
	for key, entry := range h.ctx.state().assignmentCache_ {
		if entry.switchback != nil || key != entry.assignment.Name {
			t.Fatalf("%s: switchback slot", key)
		}
	}
}

// The cached read of a unit experiment allocates as before switchbacks,
// that is not at all: the clock reading is only taken, and only kept, for a
// switchback.
func TestSwitchback_CachedUnitReadsDoNotAllocateMore(t *testing.T) {
	var data = hoData(nil, hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0), "color"))
	var h = hoCreateFor(t, hoUID, data)
	h.treatment("exp")
	var reads = map[string]func(){
		"GetTreatment":     func() { _, _ = h.ctx.GetTreatment("exp") },
		"PeekTreatment":    func() { _, _ = h.ctx.PeekTreatment("exp") },
		"GetVariableValue": func() { _, _ = h.ctx.GetVariableValue("color", 1) },
	}
	for name, read := range reads {
		if n := hoLeastAllocs(read); n != 0 {
			t.Errorf("%s: %.0f allocations", name, n)
		}
	}
}

// A cached switchback read takes the read lock only and allocates nothing,
// as a unit read: its period's slot key is built once per period.
func TestSwitchback_CachedSwitchbackReadsDoNotAllocateMore(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5).treatment("exp_sb")
	var reads = map[string]func(){
		"GetTreatment":      func() { _, _ = h.ctx.GetTreatment("exp_sb") },
		"PeekTreatment":     func() { _, _ = h.ctx.PeekTreatment("exp_sb") },
		"GetVariableValue":  func() { _, _ = h.ctx.GetVariableValue("sb.arm", 0) },
		"PeekVariableValue": func() { _, _ = h.ctx.PeekVariableValue("sb.arm", 0) },
	}
	for name, read := range reads {
		if n := hoLeastAllocs(read); n != 0 {
			t.Errorf("%s: %.0f allocations", name, n)
		}
	}
}

// The slot key cached for one period is never used for another: alternating
// between two cached periods serves and keeps each period's own slot.
func TestSwitchback_SlotKeyCacheFollowsThePeriod(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+5).treatment("exp_sb"))
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).treatment("exp_sb"))
	for i := 0; i < 3; i++ {
		expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+6).treatment("exp_sb"))
		expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+6).treatment("exp_sb"))
	}
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true),
		sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour+5, true, true))
}

// Every switchback scenario of the publish golden probe: a payload without
// switchbacks publishes byte for byte what it did before, so
// testAssets/publish-no-holdouts.golden still matches (see
// TestHoldout_PublishWithoutHoldoutsIsUnchanged), and the same payload with
// randomizationType "unit" on every experiment publishes the same bytes.
func TestSwitchback_ExplicitUnitRandomizationPublishesTheSameBytes(t *testing.T) {
	var raw, _ = os.ReadFile("testAssets/context.json")
	var data, _ = DefaultContextDataDeserializer{}.Deserialize(raw)
	var unit = jsonmodels.ContextData{Experiments: append([]jsonmodels.Experiment(nil), data.Experiments...)}
	for i := range unit.Experiments {
		unit.Experiments[i].RandomizationType = "unit"
	}
	var want, _ = os.ReadFile("testAssets/publish-no-holdouts.golden")
	if got := hoPublishProbe(t, unit); got != string(want) {
		t.Fatalf("publish changed:\n got %s\nwant %s", got, want)
	}
}

// Cluster paths are parsed once, when the data is installed: resolving a new
// period does not parse them again, however many sources the map has.
func TestSwitchback_ClusterPathsAreParsedAtInstall(t *testing.T) {
	var experiment = sbDefault(3, sbHour)
	for i := 0; i < 20; i++ {
		experiment.SwitchbackClusters = append(experiment.SwitchbackClusters, sbClusters(fmt.Sprintf("unit_%d", i), "store_key")...)
	}
	var h = sbCreate(t, sbData(experiment))
	h.at(sbAnchor).peek("exp_sb")
	var allocations = testing.AllocsPerRun(100, func() {
		h.clock.set(h.clock.Millis() + sbHour)
		h.ctx.GetAssignment("exp_sb")
	})
	// About 13 per resolution; reparsing 21 paths adds about 4 each.
	if allocations > 40 {
		t.Fatalf("a period resolution made %.0f allocations; cluster paths are parsed again", allocations)
	}
}
