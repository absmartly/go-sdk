package jsonmodels

import (
	"encoding/json"
	"reflect"
	"testing"
)

// v1.4.0 keeps v1.3.0's ContextData and adds only the switchback wire fields
// to Experiment, after v1.3.0's last field, HoldoutIds (CHANGELOG, v1.4.0
// Changed). Any other change to either layout would break more callers than
// the CHANGELOG lists.
func TestContextDataLayoutIsV130PlusSwitchbackFields(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  []string
	}{
		{ContextData{}, []string{"Experiments []jsonmodels.Experiment", "Holdouts []jsonmodels.Holdout"}},
		{Experiment{}, []string{"Id int", "Name string", "UnitType string", "Iteration int", "SeedHi int",
			"SeedLo int", "Split []float64", "TrafficSeedHi int", "TrafficSeedLo int", "TrafficSplit []float64",
			"FullOnVariant int", "Applications []jsonmodels.ExperimentApplication",
			"Variants []jsonmodels.ExperimentVariant", "CustomFieldValues []jsonmodels.CustomFieldValue",
			"AudienceStrict bool", "Audience string", "AssignmentRules string", "HoldoutIds []int",
			"RandomizationType string", "SwitchbackClusters jsonmodels.SwitchbackClusters",
			"SwitchbackClusterValues []string", "SwitchbackStartAt *int64", "SwitchbackPeriodMs *int64",
			"SwitchbackCarryoverDurationMs *int64", "SwitchbackBurnInDurationMs *int64",
			"SwitchbackBurnOutDurationMs *int64", "SwitchbackDesignType string", "SwitchbackEstimand string"}},
	} {
		var fields []string
		var typ = reflect.TypeOf(tc.value)
		for i := 0; i < typ.NumField(); i++ {
			fields = append(fields, typ.Field(i).Name+" "+typ.Field(i).Type.String())
		}
		if !reflect.DeepEqual(fields, tc.want) {
			t.Errorf("%s fields %v, want %v", typ.Name(), fields, tc.want)
		}
	}
	var _ = ContextData{Experiments: nil, Holdouts: nil}
}

// The switchback fields use the collector's JSON names and are omitted when
// unset, so an experiment without them encodes exactly as in v1.3.0.
func TestExperimentSwitchbackFieldsJSON(t *testing.T) {
	var raw, _ = json.Marshal(Experiment{Id: 1})
	var fields map[string]interface{}
	_ = json.Unmarshal(raw, &fields)
	for _, name := range []string{"randomizationType", "switchbackClusters", "switchbackClusterValues",
		"switchbackStartAt", "switchbackPeriodMs", "switchbackCarryoverDurationMs", "switchbackBurnInDurationMs",
		"switchbackBurnOutDurationMs", "switchbackDesignType", "switchbackEstimand", "holdoutIds"} {
		if _, found := fields[name]; found {
			t.Errorf("%s marshalled when unset: %s", name, raw)
		}
	}
	var start, period = int64(1000), int64(3600000)
	var set = Experiment{Id: 1, RandomizationType: "switchback", SwitchbackStartAt: &start, SwitchbackPeriodMs: &period,
		SwitchbackClusters: SwitchbackClusters{{UnitType: "store_id", Path: "store"}}, SwitchbackClusterValues: []string{"a"},
		SwitchbackDesignType: "balanced_block", SwitchbackEstimand: "global"}
	raw, _ = json.Marshal(set)
	var again Experiment
	if err := json.Unmarshal(raw, &again); err != nil || !reflect.DeepEqual(again, set) {
		t.Fatalf("round trip %s: %+v, %v", raw, again, err)
	}
	fields = nil
	_ = json.Unmarshal(raw, &fields)
	for _, name := range []string{"randomizationType", "switchbackClusters", "switchbackClusterValues",
		"switchbackStartAt", "switchbackPeriodMs", "switchbackDesignType", "switchbackEstimand"} {
		if _, found := fields[name]; !found {
			t.Errorf("%s missing from %s", name, raw)
		}
	}
}
