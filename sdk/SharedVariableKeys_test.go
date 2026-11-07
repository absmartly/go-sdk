package sdk

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// sharedKeyExperiment builds an experiment that defines the shared key "k" in
// one of its variants. fullOn selects the forced variant (0 means run the
// traffic/split path, which with a missing unit leaves the unit unassigned).
func sharedKeyExperiment(id int, name string, unitType string, fullOn int, variants []string) jsonmodels.Experiment {
	var vs = make([]jsonmodels.ExperimentVariant, 0, len(variants))
	for i, config := range variants {
		vs = append(vs, jsonmodels.ExperimentVariant{Name: string(rune('A' + i)), Config: config})
	}
	return jsonmodels.Experiment{
		Id:            id,
		Name:          name,
		UnitType:      unitType,
		Iteration:     1,
		Split:         []float64{0.5, 0.5},
		TrafficSplit:  []float64{0.0, 1.0},
		FullOnVariant: fullOn,
		Variants:      vs,
	}
}

func sharedKeyContext(t *testing.T, overrides map[string]int, experiments ...jsonmodels.Experiment) *Context {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.Overrides_ = overrides
	var df, done = future.New()
	done(jsonmodels.ContextData{Experiments: experiments}, nil)
	return CreateTestContext(config, df)
}

// (a) both experiments assign the key; the lowest experiment id wins.
func TestSharedKeyLowestIdWins(t *testing.T) {
	var ctx = sharedKeyContext(t, nil,
		sharedKeyExperiment(1, "s_low", "session_id", 1, []string{"", `{"k":"low"}`}),
		sharedKeyExperiment(2, "s_high", "session_id", 1, []string{"", `{"k":"high"}`}),
	)

	var res, err = ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("low", res, t)
}

// (b) the payload lists the higher id first, but the lowest id still wins.
func TestSharedKeyPayloadOrderIgnored(t *testing.T) {
	var ctx = sharedKeyContext(t, nil,
		sharedKeyExperiment(2, "s_high", "session_id", 1, []string{"", `{"k":"high"}`}),
		sharedKeyExperiment(1, "s_low", "session_id", 1, []string{"", `{"k":"low"}`}),
	)

	var res, err = ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("low", res, t)
}

// (c) the first experiment does not assign the unit, so the value falls through
// to the second; both experiments are exposed and the first records assigned:false.
func TestSharedKeyFallsThroughAndExposesBoth(t *testing.T) {
	var ctx = sharedKeyContext(t, nil,
		sharedKeyExperiment(1, "s_unassigned", "no_such_unit", 0, []string{"", `{"k":"low"}`}),
		sharedKeyExperiment(2, "s_assigned", "session_id", 1, []string{"", `{"k":"high"}`}),
	)

	var res, err = ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("high", res, t)

	ctx.EventLock_.Lock()
	defer ctx.EventLock_.Unlock()
	var first, second bool
	for _, exp := range ctx.Exposures_ {
		switch exp.Name {
		case "s_unassigned":
			first = true
			assertAny(false, exp.Assigned, t)
		case "s_assigned":
			second = true
			assertAny(true, exp.Assigned, t)
		}
	}
	assertAny(true, first, t)
	assertAny(true, second, t)
}

// (d) the first experiment is unassigned but overridden, so the override wins.
func TestSharedKeyOverrideWins(t *testing.T) {
	var ctx = sharedKeyContext(t, map[string]int{"s_override": 1},
		sharedKeyExperiment(1, "s_override", "no_such_unit", 0, []string{"", `{"k":"low"}`}),
		sharedKeyExperiment(2, "s_assigned", "session_id", 1, []string{"", `{"k":"high"}`}),
	)

	var res, err = ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("low", res, t)
}

// (e) the assigned variant's config lacks the key, so the value falls through.
func TestSharedKeyAssignedVariantMissingKey(t *testing.T) {
	var ctx = sharedKeyContext(t, nil,
		sharedKeyExperiment(1, "s_otherkey", "session_id", 2, []string{"", `{"k":"low"}`, `{"other":"x"}`}),
		sharedKeyExperiment(2, "s_assigned", "session_id", 1, []string{"", `{"k":"high"}`}),
	)

	var res, err = ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("high", res, t)
}

// (f) peek returns the same values as get and queues no exposures.
func TestSharedKeyPeekMatchesAndQueuesNothing(t *testing.T) {
	var ctx = sharedKeyContext(t, nil,
		sharedKeyExperiment(1, "s_low", "session_id", 1, []string{"", `{"k":"low"}`}),
		sharedKeyExperiment(2, "s_high", "session_id", 1, []string{"", `{"k":"high"}`}),
	)

	var peeked, err = ctx.PeekVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny("low", peeked, t)
	assertAny(int32(0), ctx.GetPendingCount(), t)

	got, err := ctx.GetVariableValue("k", "def")
	assertAny(nil, err, t)
	assertAny(peeked, got, t)
}
