package sdk

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// Shared variable keys (owner decision, matching the Java SDK's
// Context.getVariableAssignment): the candidates are the experiments any of
// whose variants define the key, in experiment-id order. The winner is the
// first one the unit is assigned to, overridden in or rule-assigned to. Its
// selected variant supplies the value, or the caller's default when that
// variant lacks the key; later candidates are never consulted. Only the
// winner is exposed, and PeekVariableValue exposes nothing.

const strictUSAudience = `{"filter":[{"eq":[{"var":"country"},{"value":"US"}]}]}`

// keyExperiment is an experiment, assigned through its full-on variant
// unless changed, whose variants have the given configs.
func keyExperiment(id int, name string, configs ...string) jsonmodels.Experiment {
	var variants = make([]jsonmodels.ExperimentVariant, len(configs))
	for i, config := range configs {
		variants[i] = jsonmodels.ExperimentVariant{Name: string(rune('A' + i)), Config: config}
	}
	return jsonmodels.Experiment{Id: id, Name: name, UnitType: "session_id", Iteration: 1,
		Split: make([]float64, len(configs)), TrafficSplit: []float64{0, 1}, FullOnVariant: 1,
		Applications: []jsonmodels.ExperimentApplication{{Name: "website"}}, Variants: variants}
}

func strictMismatch(experiment jsonmodels.Experiment) jsonmodels.Experiment {
	experiment.Audience, experiment.AudienceStrict = strictUSAudience, true
	return experiment
}

func otherUnitType(experiment jsonmodels.Experiment) jsonmodels.Experiment {
	experiment.UnitType, experiment.FullOnVariant = "user_id", 0
	return experiment
}

func exposedNames(ctx *Context) []string {
	var names []string
	for _, exposure := range queuedExposures(ctx) {
		names = append(names, exposure.Name)
	}
	return names
}

func expectExposed(t *testing.T, ctx *Context, names ...string) {
	t.Helper()
	var got = exposedNames(ctx)
	if len(got) != len(names) {
		t.Fatalf("exposed %v, want %v", got, names)
	}
	for i := range names {
		if got[i] != names[i] {
			t.Fatalf("exposed %v, want %v", got, names)
		}
	}
}

func variableValue(t *testing.T, ctx *Context, key string, defaultValue interface{}) interface{} {
	t.Helper()
	var value, err = ctx.GetVariableValue(key, defaultValue)
	if err != nil {
		t.Fatalf("GetVariableValue(%s): %v", key, err)
	}
	return value
}

func peekVariableValue(t *testing.T, ctx *Context, key string, defaultValue interface{}) interface{} {
	t.Helper()
	var value, err = ctx.PeekVariableValue(key, defaultValue)
	if err != nil {
		t.Fatalf("PeekVariableValue(%s): %v", key, err)
	}
	return value
}

// The lower-id candidate is not assigned (outside its strict audience, or
// no unit of its type), so the higher-id one wins and is the only exposure.
func TestSharedKeyUnassignedLowerCandidateIsNotExposed(t *testing.T) {
	for name, low := range map[string]jsonmodels.Experiment{
		"strict audience mismatch": strictMismatch(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
		"no unit for its type":     otherUnitType(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			var ctx, _ = newRuleContext(t, "production", withUnit(), low,
				keyExperiment(2, "high", `{"k":"high0"}`, `{"k":"high1"}`))
			assertAny("high1", variableValue(t, ctx, "k", "def"), t)
			expectExposed(t, ctx, "high")
			assertAny(true, queuedExposures(ctx)[0].Assigned, t)
		})
	}
}

// The winner's selected variant lacks the key: the caller's default, one
// exposure for the winner, none for the later candidate that has the key.
func TestSharedKeyWinnerVariantWithoutKeyGivesDefault(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		keyExperiment(1, "first", `{"k":"first0"}`, `{"other":1}`),
		keyExperiment(2, "second", `{"k":"second0"}`, `{"k":"second1"}`))
	assertAny("def", peekVariableValue(t, ctx, "k", "def"), t)
	assertAny("def", variableValue(t, ctx, "k", "def"), t)
	expectExposed(t, ctx, "first")
}

// No candidate is assigned: the caller's default and no exposure.
func TestSharedKeyNoAssignedCandidateGivesDefaultAndNoExposure(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		strictMismatch(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
		otherUnitType(keyExperiment(2, "high", `{"k":"high0"}`, `{"k":"high1"}`)))
	assertAny("def", variableValue(t, ctx, "k", "def"), t)
	expectExposed(t, ctx)
}

// Peek resolves the same winner as get and queues nothing, including when
// the winner's variant lacks the key and a later candidate has it.
func TestSharedKeyPeekExposesNothing(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		strictMismatch(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
		keyExperiment(2, "mid", `{"k":"mid0"}`, `{"other":1}`),
		keyExperiment(3, "high", `{"k":"high0"}`, `{"k":"high1"}`))
	assertAny("def", peekVariableValue(t, ctx, "k", "def"), t)
	expectExposed(t, ctx)
	assertAny(int32(0), ctx.GetPendingCount(), t)
	assertAny("def", variableValue(t, ctx, "k", "def"), t)
	expectExposed(t, ctx, "mid")
}

// An override makes an otherwise unassigned lower-id candidate the winner;
// an override to a variant without the key gives the default, and still only
// the overridden experiment is exposed.
func TestSharedKeyOverrideWinsAndIsTheOnlyExposure(t *testing.T) {
	for _, tc := range []struct {
		variant int
		want    interface{}
	}{{1, "low1"}, {2, "def"}} {
		var ctx, _ = newRuleContext(t, "production", withUnit(),
			otherUnitType(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`, `{"other":1}`)),
			keyExperiment(2, "high", `{"k":"high0"}`, `{"k":"high1"}`))
		assertAny(nil, ctx.SetOverride("low", tc.variant), t)
		assertAny(tc.want, variableValue(t, ctx, "k", "def"), t)
		expectExposed(t, ctx, "low")
		assertAny(true, queuedExposures(ctx)[0].Overridden, t)
	}
}

// A matching assignment rule makes an otherwise unassigned candidate the
// winner, exposed with its rule flag; the unassigned lower-id candidate
// before it is not exposed.
func TestSharedKeyRuleAssignedCandidateWins(t *testing.T) {
	var ruled = otherUnitType(keyExperiment(2, "ruled", `{"k":"ruled0"}`, `{"k":"ruled1"}`, `{"k":"ruled2"}`))
	ruled.AssignmentRules = rulesJSON(`{"type":"assign","variant":2}`)
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		strictMismatch(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)), ruled,
		keyExperiment(3, "high", `{"k":"high0"}`, `{"k":"high1"}`))
	assertAny("ruled2", variableValue(t, ctx, "k", "def"), t)
	expectExposed(t, ctx, "ruled")
	assertAny(true, queuedExposures(ctx)[0].RuleOverride, t)
}

// A key the winner's variant defines with null, false, 0 or "" returns that
// value, not the default; it is not treated as missing. The unassigned
// lower-id candidate is not exposed.
func TestSharedKeyZeroValuesAreValues(t *testing.T) {
	for _, tc := range []struct {
		config string
		want   interface{}
	}{{`{"k":null}`, nil}, {`{"k":false}`, false}, {`{"k":0}`, float64(0)}, {`{"k":""}`, ""}} {
		var ctx, _ = newRuleContext(t, "production", withUnit(),
			otherUnitType(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
			keyExperiment(2, "winner", `{"k":"winner0"}`, tc.config),
			keyExperiment(3, "later", `{"k":"later0"}`, `{"k":"later1"}`))
		assertAny(tc.want, peekVariableValue(t, ctx, "k", "def"), t)
		assertAny(tc.want, variableValue(t, ctx, "k", "def"), t)
		expectExposed(t, ctx, "winner")
	}
}

// An already exposed winner is not exposed again, and a later read that
// resolves to the same winner queues nothing.
func TestSharedKeyWinnerExposedOnce(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		strictMismatch(keyExperiment(1, "low", `{"k":"low0"}`, `{"k":"low1"}`)),
		keyExperiment(2, "high", `{"k":"high0"}`, `{"k":"high1"}`))
	variableValue(t, ctx, "k", "def")
	variableValue(t, ctx, "k", "def")
	expectExposed(t, ctx, "high")
}
