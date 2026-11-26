package jsonmodels

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ContextData and Experiment keep their v1.2.0 layouts: holdouts and
// switchbacks travel in ContextPayload and ExperimentData instead, so a
// caller's positional literal of either keeps compiling. These literals fail
// to compile, as the caller's would, if a field is added, removed or
// reordered.
var (
	_ = ContextData{nil}
	_ = Experiment{0, "", "", 0, 0, 0, nil, 0, 0, nil, 0, nil, nil, nil, false, "", ""}
)

func TestContextDataKeepsTheV120Layout(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  []string
	}{
		{ContextData{}, []string{"Experiments []jsonmodels.Experiment"}},
		{Experiment{}, []string{"Id int", "Name string", "UnitType string", "Iteration int", "SeedHi int",
			"SeedLo int", "Split []float64", "TrafficSeedHi int", "TrafficSeedLo int", "TrafficSplit []float64",
			"FullOnVariant int", "Applications []jsonmodels.ExperimentApplication",
			"Variants []jsonmodels.ExperimentVariant", "CustomFieldValues []jsonmodels.CustomFieldValue",
			"AudienceStrict bool", "Audience string", "AssignmentRules string"}},
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
}

// A ContextPayload decodes the collector's JSON with the experiment fields
// flattened into each ExperimentData, and encodes back to the same keys.
func TestContextPayloadJSONRoundTrip(t *testing.T) {
	var raw = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":2,"seedHi":3,"seedLo":4,` +
		`"split":[0.5,0.5],"trafficSeedHi":5,"trafficSeedLo":6,"trafficSplit":[0,1],"fullOnVariant":0,` +
		`"applications":[{"name":"website"}],"variants":[{"name":"A","config":null},{"name":"B","config":null}],` +
		`"customFieldValues":null,"audienceStrict":false,"audience":"","holdoutIds":[11],` +
		`"randomizationType":"switchback","switchbackStartAt":1000,"switchbackPeriodMs":3600000}],` +
		`"holdouts":[{"id":11,"name":"h","unitType":"session_id","iteration":1,"seedHi":1,"seedLo":2,` +
		`"split":[0.1,0.9],"holdoutType":"full"}]}`
	var payload ContextPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	var e = payload.Experiments[0]
	if e.Id != 1 || e.Name != "exp" || e.Iteration != 2 || len(e.Variants) != 2 || !reflect.DeepEqual(e.HoldoutIds, []int{11}) ||
		e.RandomizationType != "switchback" || e.SwitchbackStartAt == nil || *e.SwitchbackStartAt != 1000 ||
		e.SwitchbackPeriodMs == nil || *e.SwitchbackPeriodMs != 3600000 || len(payload.Holdouts) != 1 {
		t.Fatalf("decoded %+v", payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var again ContextPayload
	if err := json.Unmarshal(encoded, &again); err != nil || !reflect.DeepEqual(again, payload) {
		t.Fatalf("round trip %s: %+v, %v", encoded, again, err)
	}
}

// ContextData drops only the holdout and switchback fields, and PayloadOf
// carries every v1.2.0 field across; nil stays nil.
func TestContextPayloadConversions(t *testing.T) {
	var start = int64(7)
	var payload = ContextPayload{
		Experiments: []ExperimentData{{Experiment: Experiment{Id: 1, Name: "a", AssignmentRules: "r"},
			HoldoutIds: []int{11}, RandomizationType: "switchback", SwitchbackStartAt: &start}},
		Holdouts: []Holdout{{Id: 11}},
	}
	var data = payload.ContextData()
	if !reflect.DeepEqual(data, ContextData{Experiments: []Experiment{{Id: 1, Name: "a", AssignmentRules: "r"}}}) {
		t.Fatalf("ContextData() = %+v", data)
	}
	var back = PayloadOf(data)
	if !reflect.DeepEqual(back, ContextPayload{Experiments: []ExperimentData{{Experiment: data.Experiments[0]}}}) {
		t.Fatalf("PayloadOf = %+v", back)
	}
	if (ContextPayload{}).ContextData().Experiments != nil || PayloadOf(ContextData{}).Experiments != nil {
		t.Fatal("nil experiments became non-nil")
	}
	if got := (ContextPayload{Experiments: []ExperimentData{}}).ContextData().Experiments; got == nil || len(got) != 0 {
		t.Fatalf("empty experiments became %#v", got)
	}
}
