package sdk

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

const ruleUnit = "e791e240fcd3df7d238cfc285f475e8152fcc0ec"

// ruleExperiment builds a running three-variant experiment whose normal
// assignment is pinned by trafficSplit/split, with the given assignment rules.
func ruleExperiment(name string, rules string) jsonmodels.Experiment {
	return jsonmodels.Experiment{
		Id:              7,
		Name:            name,
		UnitType:        "session_id",
		Iteration:       1,
		SeedHi:          55006150,
		SeedLo:          47189152,
		Split:           []float64{1, 0, 0},
		TrafficSeedHi:   705671872,
		TrafficSeedLo:   212903484,
		TrafficSplit:    []float64{0, 1},
		Applications:    []jsonmodels.ExperimentApplication{{Name: "website"}},
		AssignmentRules: rules,
		Variants: []jsonmodels.ExperimentVariant{
			{Name: "A", Config: `{"color":"red"}`},
			{Name: "B", Config: `{"color":"green"}`},
			{Name: "C", Config: `{"color":"blue"}`},
		},
	}
}

func rulesJSON(rules ...string) string {
	var out = `{"rules":[`
	for i, rule := range rules {
		if i > 0 {
			out += ","
		}
		out += rule
	}
	return out + `]}`
}

// newRuleContext creates a ready context through the full ABsmartly path with
// the given environment and units, and no publish delay or refresh timer.
func newRuleContext(t *testing.T, environment string, units map[string]string, experiments ...jsonmodels.Experiment) (*Context, *systemAttrClient) {
	t.Helper()
	var client = &systemAttrClient{application: "website", environment: environment}
	var abs = Create(ABsmartlyConfig{
		Client_:              client,
		ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client},
	})
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: experiments})
	ctx.WaitUntilReady()
	return ctx, client
}

func withUnit() map[string]string {
	return map[string]string{"session_id": ruleUnit}
}

func treatment(t *testing.T, ctx *Context, name string) int {
	t.Helper()
	var variant, err = ctx.GetTreatment(name)
	if err != nil {
		t.Fatalf("GetTreatment(%s): %v", name, err)
	}
	return variant
}

func queuedExposures(ctx *Context) []jsonmodels.Exposure {
	ctx.EventLock_.Lock()
	defer ctx.EventLock_.Unlock()
	return append([]jsonmodels.Exposure(nil), ctx.Exposures_...)
}

func expectExposures(t *testing.T, ctx *Context, n int) []jsonmodels.Exposure {
	t.Helper()
	var exposures = queuedExposures(ctx)
	if len(exposures) != n {
		t.Fatalf("queued %d exposures, want %d: %+v", len(exposures), n, exposures)
	}
	return exposures
}

// splitVariant is the variant a split rule gives ruleUnit: the unit hashed
// with the experiment's own seed.
func splitVariant(split []float64, seedHi int, seedLo int) int {
	var buf [12]int8
	return NewVariantAssigner(HashUnit(ruleUnit)).Assign(split, seedHi, seedLo, buf[:])
}

func TestRules_AssignRuleSetsVariant(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))

	assertAny(2, treatment(t, ctx, "exp"), t)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(true, exposure.RuleOverride, t)
	assertAny(false, exposure.Assigned, t)
	assertAny(true, exposure.Eligible, t)
	assertAny(false, exposure.Overridden, t)
	assertAny(false, exposure.Custom, t)
	assertAny(false, exposure.FullOn, t)
	assertAny(false, exposure.AudienceMismatch, t)
}

func TestRules_NoMatchUsesNormalAssignment(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"value":false}}`)))

	assertAny(0, treatment(t, ctx, "exp"), t)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(false, exposure.RuleOverride, t)
	assertAny(true, exposure.Assigned, t)
}

func TestRules_ConditionsUseAttributesAndSystemAttributes(t *testing.T) {
	var rules = rulesJSON(
		`{"type":"assign","variant":1,"conditions":{"and":[{"eq":[{"var":"environment"},{"value":"production"}]},{"eq":[{"var":"application"},{"value":"website"}]},{"eq":[{"var":"country"},{"value":"PT"}]}]}}`)
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", rules))

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("country", "PT"), t)
	assertAny(1, treatment(t, ctx, "exp"), t)
}

func TestRules_EnvironmentScoping(t *testing.T) {
	var rules = rulesJSON(`{"type":"assign","variant":1,"environments":["staging"]}`, `{"type":"assign","variant":2,"environments":["production"]}`)
	for _, tc := range []struct {
		environment string
		want        int
	}{{"staging", 1}, {"production", 2}, {"Production", 0}, {"", 0}} {
		var ctx, _ = newRuleContext(t, tc.environment, withUnit(), ruleExperiment("exp", rules))
		if got := treatment(t, ctx, "exp"); got != tc.want {
			t.Errorf("environment %q: variant %d, want %d", tc.environment, got, tc.want)
		}
	}
}

func TestRules_EmptyEnvironmentsMatchesEveryEnvironment(t *testing.T) {
	var ctx, _ = newRuleContext(t, "", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":1,"environments":[]}`)))
	assertAny(1, treatment(t, ctx, "exp"), t)
}

func TestRules_OverrideBeatsRule(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
	assertAny(nil, ctx.SetOverride("exp", 1), t)

	assertAny(1, treatment(t, ctx, "exp"), t)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(true, exposure.Overridden, t)
	assertAny(false, exposure.RuleOverride, t)
}

func TestRules_RuleSkipsStrictAudience(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`))
	experiment.Audience = `{"filter":[{"value":false}]}`
	experiment.AudienceStrict = true
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)

	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(false, expectExposures(t, ctx, 1)[0].AudienceMismatch, t)
}

func TestRules_RuleBeatsTrafficAndFullOn(t *testing.T) {
	var notEligible = ruleExperiment("not_eligible", rulesJSON(`{"type":"assign","variant":2}`))
	notEligible.TrafficSplit = []float64{1, 0}
	var fullOn = ruleExperiment("full_on", rulesJSON(`{"type":"assign","variant":2}`))
	fullOn.FullOnVariant = 1
	var ctx, _ = newRuleContext(t, "production", withUnit(), notEligible, fullOn)

	assertAny(2, treatment(t, ctx, "not_eligible"), t)
	assertAny(2, treatment(t, ctx, "full_on"), t)
	for _, exposure := range expectExposures(t, ctx, 2) {
		assertAny(true, exposure.Eligible, t)
		assertAny(false, exposure.FullOn, t)
	}
}

// A rule that wins over a different custom assignment must not look stale on
// every call: rebuilding it reset its exposed flag and queued a duplicate
// exposure per GetTreatment (javascript-sdk#66).
func TestRules_RuleWinsOverCustomAssignmentAndIsExposedOnce(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
	assertAny(nil, ctx.SetCustomAssignment("exp", 1), t)

	for i := 0; i < 3; i++ {
		assertAny(2, treatment(t, ctx, "exp"), t)
	}
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(false, exposure.Custom, t)
	assertAny(true, exposure.RuleOverride, t)

	assertAny(nil, ctx.SetCustomAssignment("exp", 0), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_RuleForcingControlIsARuleOverride(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":0}`))
	experiment.Split = []float64{0, 0, 1}
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

// An out-of-range assign variant stops rule evaluation and falls back to
// normal assignment, as on javascript-sdk main and in the collector.
func TestRules_OutOfRangeAssignStopsAndFallsBack(t *testing.T) {
	for _, variant := range []int{3, -1} {
		var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp",
			rulesJSON(fmt.Sprintf(`{"type":"assign","variant":%d}`, variant), `{"type":"assign","variant":1}`)))
		assertAny(0, treatment(t, ctx, "exp"), t)
		assertAny(false, expectExposures(t, ctx, 1)[0].RuleOverride, t)
	}
}

func TestRules_InvalidRulesFallBack(t *testing.T) {
	for _, rules := range []string{`not json`, `{"rules":{}}`, `[]`, `{"rules":[{"type":"assign","variant":1.5}]}`} {
		var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", rules))
		if got := treatment(t, ctx, "exp"); got != 0 {
			t.Errorf("rules %s: variant %d, want 0", rules, got)
		}
	}
}

func TestRules_VariableValueFollowsRuleVariant(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))

	var value, err = ctx.PeekVariableValue("color", "none")
	assertAny(nil, err, t)
	assertAny("blue", value, t)
	expectExposures(t, ctx, 0)

	value, err = ctx.GetVariableValue("color", "none")
	assertAny(nil, err, t)
	assertAny("blue", value, t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

func TestRules_AttributeChangeReResolves(t *testing.T) {
	var rules = rulesJSON(
		`{"type":"assign","variant":1,"conditions":{"eq":[{"var":"tier"},{"value":"gold"}]}}`,
		`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"tier"},{"value":"silver"}]}}`)
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", rules))

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("tier", "gold"), t)
	assertAny(1, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("tier", "silver"), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("tier", "none"), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 4)
}

func TestRules_UnrelatedAttributeChangeKeepsExposedRuleAssignment(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))

	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_RefreshChangingRulesReResolves(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`))
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
	assertAny(2, treatment(t, ctx, "exp"), t)

	// Unchanged rules keep the cached, exposed assignment.
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)

	experiment.AssignmentRules = rulesJSON(`{"type":"assign","variant":1}`)
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(1, treatment(t, ctx, "exp"), t)

	experiment.AssignmentRules = ""
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(0, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 3)
}

func TestRules_RefreshAddingRulesToExposedAssignmentReExposes(t *testing.T) {
	var experiment = ruleExperiment("exp", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
	assertAny(0, treatment(t, ctx, "exp"), t)

	experiment.AssignmentRules = rulesJSON(`{"type":"assign","variant":2}`)
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 2)[1].RuleOverride, t)
}

func TestRules_RuleVariantDroppedWhenExperimentIsRemoved(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
	assertAny(2, treatment(t, ctx, "exp"), t)

	ctx.SetData(jsonmodels.ContextData{})
	assertAny(0, treatment(t, ctx, "exp"), t)
	var exposure = expectExposures(t, ctx, 2)[1]
	assertAny(false, exposure.RuleOverride, t)
}

func TestRules_SplitHashesUnitWithExperimentSeed(t *testing.T) {
	// With 40/30/30, hashing ruleUnit with the experiment seed, hashing it
	// with the traffic seed and normal assignment (always 0 here) all give
	// different variants.
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"40/30/30"}`))
	var split = []float64{0.4, 0.3, 0.3}
	var bySeed = splitVariant(split, experiment.SeedHi, experiment.SeedLo)
	var byTraffic = splitVariant(split, experiment.TrafficSeedHi, experiment.TrafficSeedLo)
	if bySeed == 0 || bySeed == byTraffic {
		t.Fatalf("fixture no longer separates the seeds: experiment seed %d, traffic seed %d", bySeed, byTraffic)
	}

	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
	assertAny(bySeed, treatment(t, ctx, "exp"), t)
}

// A split equal to the experiment split keeps every unit on its variant.
func TestRules_SplitEqualToExperimentSplitKeepsVariant(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"20/30/50"}`))
	experiment.Split = []float64{0.2, 0.3, 0.5}
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)

	assertAny(splitVariant(experiment.Split, experiment.SeedHi, experiment.SeedLo), treatment(t, ctx, "exp"), t)
}

func TestRules_SplitAssignsOnlyNonZeroShare(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"0/0/100"}`)))

	assertAny(2, treatment(t, ctx, "exp"), t)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(true, exposure.RuleOverride, t)
	assertAny(false, exposure.Assigned, t)
	assertAny(true, exposure.Eligible, t)
}

func TestRules_SplitWinsOverCustomAssignment(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"0/100/0"}`)))
	assertAny(nil, ctx.SetCustomAssignment("exp", 2), t)

	assertAny(1, treatment(t, ctx, "exp"), t)
	assertAny(1, treatment(t, ctx, "exp"), t)
	assertAny(false, expectExposures(t, ctx, 1)[0].Custom, t)
}

func TestRules_SplitNotCoveringEveryVariantFallsBackWithoutLaterRules(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp",
		rulesJSON(`{"type":"split","percentages":"50/50"}`, `{"type":"assign","variant":2}`)))

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(false, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

func TestRules_MalformedSplitIsSkippedForTheNextRule(t *testing.T) {
	for _, percentages := range []string{`"abc/50/50"`, `"-10/60/50"`, `"20/30/40"`, `"50/50.015/0"`, `50`, `null`} {
		var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp",
			rulesJSON(`{"type":"split","percentages":`+percentages+`}`, `{"type":"assign","variant":1}`)))
		if got := treatment(t, ctx, "exp"); got != 1 {
			t.Errorf("percentages %s: variant %d, want 1 from the next rule", percentages, got)
		}
	}
}

func TestRules_SplitWithoutUnitGivesNoUnitResultThenReResolves(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", map[string]string{},
		ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"0/0/100"}`, `{"type":"assign","variant":1}`)))

	// The split matches without its unit: evaluation stops there with the
	// experiment's no-unit result instead of trying the assign rule.
	var variant, err = ctx.PeekTreatment("exp")
	assertAny(nil, err, t)
	assertAny(0, variant, t)
	assertAny(true, ctx.GetAssignment("exp").RuleOverride, t)

	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_SplitWithoutUnitOnFullOnExperimentGivesFullOnVariant(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"100/0/0"}`))
	experiment.FullOnVariant = 2
	var ctx, _ = newRuleContext(t, "production", map[string]string{}, experiment)

	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

func TestRules_AssignRuleKeepsCachedAssignmentWhenUnitIsSetLater(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", map[string]string{},
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))

	var before = ctx.GetAssignment("exp")
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	if ctx.GetAssignment("exp") != before {
		t.Error("setting the unit rebuilt an assign-rule assignment")
	}
}

// When a split starts matching on the same variant the previous rule gave, its
// missing-unit flag must still be refreshed, or setting the unit later never
// re-resolves it (javascript-sdk#66).
func TestRules_ReResolvesWhenSplitStartsMatchingOnSameVariantAsPreviousRule(t *testing.T) {
	var rules = rulesJSON(
		`{"type":"split","percentages":"0/0/100","conditions":{"eq":[{"var":"split"},{"value":true}]}}`,
		`{"type":"assign","variant":0}`)
	var ctx, _ = newRuleContext(t, "production", map[string]string{}, ruleExperiment("exp", rules))

	var variant, _ = ctx.PeekTreatment("exp")
	assertAny(0, variant, t)
	assertAny(nil, ctx.SetAttribute("split", true), t)
	variant, _ = ctx.PeekTreatment("exp")
	assertAny(0, variant, t)

	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	variant, _ = ctx.PeekTreatment("exp")
	assertAny(2, variant, t)
}

func TestRules_ExposedSplitWithoutUnitStaysPinned(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", map[string]string{},
		ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"0/0/100"}`)))

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	assertAny(0, treatment(t, ctx, "exp"), t)

	// An attribute change re-resolves the rules, but still as if the unit
	// were missing, so the user keeps the variant they saw.
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_ExposedSplitWithoutUnitStaysPinnedWhenCustomAssignmentLost(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", map[string]string{},
		ruleExperiment("exp", rulesJSON(`{"type":"split","percentages":"0/0/100"}`)))
	assertAny(nil, ctx.SetCustomAssignment("exp", 1), t)

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

// An exposed assign win followed by a split that matches without its unit on
// the same variant is pinned in place, without a second exposure.
func TestRules_ExposedAssignThenSplitWithoutUnitOnSameVariantStaysPinned(t *testing.T) {
	var rules = rulesJSON(
		`{"type":"split","percentages":"0/0/100","conditions":{"eq":[{"var":"split"},{"value":true}]}}`,
		`{"type":"assign","variant":0}`)
	var ctx, _ = newRuleContext(t, "production", map[string]string{}, ruleExperiment("exp", rules))

	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("split", true), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_PanickingConditionIsSkipped(t *testing.T) {
	var matcher = AudienceMatcher{Deserializer_: DefaultAudienceDeserializer{}}
	var parsed = &assignmentRules{rules: []interface{}{
		map[string]interface{}{"type": "assign", "variant": 1.0, "conditions": map[string]interface{}{"var": 5}},
		map[string]interface{}{"type": "assign", "variant": 2.0},
	}}
	assertAny(&ruleAction{variant: 2}, matcher.evaluateRules(parsed, "", nil), t)
}

type recordingLogger struct {
	mu     sync.Mutex
	events []error
}

func (l *recordingLogger) HandleEvent(_ Context, event EventType, data interface{}) {
	if err, ok := data.(error); ok && event == Error {
		l.mu.Lock()
		l.events = append(l.events, err)
		l.mu.Unlock()
	}
}

func (l *recordingLogger) errors() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.events...)
}

func (l *recordingLogger) hasError(text string) bool {
	for _, err := range l.errors() {
		if strings.Contains(err.Error(), text) {
			return true
		}
	}
	return false
}

type panickingAudienceDeserializer struct{}

func (panickingAudienceDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	panic("boom")
}

func TestRules_ParsingFailureOnlyDropsThatExperimentsRules(t *testing.T) {
	var logger = &recordingLogger{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var abs = Create(ABsmartlyConfig{
		Client_:               client,
		ContextDataProvider_:  DefaultContextDataProvider{client_: client},
		ContextEventHandler_:  DefaultContextEventHandler{client_: client},
		ContextEventLogger_:   logger,
		AudienceDeserializer_: panickingAudienceDeserializer{},
	})
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)),
	}})
	ctx.WaitUntilReady()

	assertAny(false, ctx.IsFailed(), t)
	assertAny(0, treatment(t, ctx, "exp"), t)
	if !logger.hasError("Failed to parse assignmentRules for experiment 'exp'") {
		t.Errorf("no parse error logged: %v", logger.errors())
	}
}

func TestRules_NonStringAssignmentRulesOnlySkipsThatExperiment(t *testing.T) {
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(`{"experiments":[
		{"id":1,"name":"bad","assignmentRules":[1,2]},
		{"id":2,"name":"good","assignmentRules":"{\"rules\":[]}"}
	]}`))
	assertAny(nil, err, t)
	assertAny("", data.Experiments[0].AssignmentRules, t)
	assertAny(true, data.Experiments[0].AssignmentRulesIgnored(), t)
	assertAny(`{"rules":[]}`, data.Experiments[1].AssignmentRules, t)
	assertAny(false, data.Experiments[1].AssignmentRulesIgnored(), t)
}

func TestRules_NonStringAssignmentRulesInPayloadKeepsOtherExperimentsRules(t *testing.T) {
	var bad, good = ruleExperiment("bad", ""), ruleExperiment("good", "")
	bad.Id, good.Id = 1, 2
	var raw, err = json.Marshal(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{bad, good}})
	assertAny(nil, err, t)
	var payload = strings.Replace(string(raw), `"name":"bad",`, `"name":"bad","assignmentRules":{"rules":[{"type":"assign","variant":2}]},`, 1)
	payload = strings.Replace(payload, `"name":"good",`, `"name":"good","assignmentRules":"{\"rules\":[{\"type\":\"assign\",\"variant\":2}]}",`, 1)
	var data jsonmodels.ContextData
	data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	assertAny(nil, err, t)

	var logger = &recordingLogger{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var abs = Create(ABsmartlyConfig{
		Client_:              client,
		ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client},
		ContextEventLogger_:  logger,
	})
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, data)
	ctx.WaitUntilReady()

	assertAny(false, ctx.IsFailed(), t)
	assertAny(0, treatment(t, ctx, "bad"), t)
	assertAny(2, treatment(t, ctx, "good"), t)
	if !logger.hasError("Ignoring assignmentRules for experiment 'bad'") {
		t.Errorf("no warning for the non-string rules: %v", logger.errors())
	}
}

type numberAudienceDeserializer struct{}

func (numberAudienceDeserializer) Deserialize(data []byte, _ int, _ int) (map[string]interface{}, error) {
	var decoder = json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var out map[string]interface{}
	return out, decoder.Decode(&out)
}

func TestRules_JSONNumberVariantFromCustomDeserializer(t *testing.T) {
	var matcher = AudienceMatcher{Deserializer_: numberAudienceDeserializer{}}
	for rules, want := range map[string]string{
		rulesJSON(`{"type":"assign","variant":2}`):                                    "&{2 []}",
		rulesJSON(`{"type":"assign","variant":2.0}`):                                  "&{2 []}",
		rulesJSON(`{"type":"assign","variant":1.5}`, `{"type":"assign","variant":0}`): "&{0 []}",
		rulesJSON(`{"type":"assign","variant":1e20}`):                                 "<nil>",
	} {
		var parsed, err = matcher.parseRules(rules)
		assertAny(nil, err, t)
		if got := fmt.Sprint(matcher.evaluateRules(parsed, "", nil)); got != want {
			t.Errorf("%s: got %s, want %s", rules, got, want)
		}
	}
}

// When an exposed rule win is rebuilt on the same variant (here, a split
// starts matching without its unit and gives the full-on variant the assign
// rule gave), the rebuilt assignment serves the refreshed variables and the
// unit is not exposed again.
func TestRules_RebuiltExposedRuleAssignmentServesRefreshedVariables(t *testing.T) {
	var experiment = ruleExperiment("exp", rulesJSON(
		`{"type":"split","percentages":"100/0/0","conditions":{"eq":[{"var":"split"},{"value":true}]}}`,
		`{"type":"assign","variant":2}`))
	experiment.FullOnVariant = 2
	var ctx, _ = newRuleContext(t, "production", map[string]string{}, experiment)
	var value, _ = ctx.GetVariableValue("color", "none")
	assertAny("blue", value, t)

	experiment.Variants = append([]jsonmodels.ExperimentVariant(nil), experiment.Variants...)
	experiment.Variants[2].Config = `{"color":"navy"}`
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(nil, ctx.SetAttribute("split", true), t)

	value, _ = ctx.GetVariableValue("color", "none")
	assertAny("navy", value, t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)

	// Still pinned: the split matched without its unit and was exposed.
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	expectExposures(t, ctx, 1)
}

func TestRules_ExposureSerializesRuleOverride(t *testing.T) {
	for _, want := range []bool{true, false} {
		var body, err = json.Marshal(jsonmodels.Exposure{Name: "exp", RuleOverride: want})
		assertAny(nil, err, t)
		var decoded map[string]interface{}
		assertAny(nil, json.Unmarshal(body, &decoded), t)
		assertAny(want, decoded["ruleOverride"], t)
	}
}

func TestRules_PublishedExposureCarriesRuleOverride(t *testing.T) {
	var ctx, client = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
	treatment(t, ctx, "exp")
	assertAny(nil, ctx.Publish(), t)

	if len(client.published) != 1 || len(client.published[0].Exposures) != 1 {
		t.Fatalf("published %+v", client.published)
	}
	assertAny(true, client.published[0].Exposures[0].RuleOverride, t)
}

func TestRules_ParseRules(t *testing.T) {
	var matcher = AudienceMatcher{Deserializer_: DefaultAudienceDeserializer{}}

	var parsed, err = matcher.parseRules("")
	assertAny((*assignmentRules)(nil), parsed, t)
	assertAny(nil, err, t)

	parsed, err = matcher.parseRules(`{"other":1}`)
	assertAny((*assignmentRules)(nil), parsed, t)
	assertAny(nil, err, t)

	_, err = matcher.parseRules(`{`)
	if err == nil {
		t.Error("invalid JSON parsed without an error")
	}

	_, err = matcher.parseRules(`null`)
	if err == nil {
		t.Error("null parsed without an error")
	}
}

func evaluate(t *testing.T, rules string, environment string, attrs map[string]interface{}) *ruleAction {
	t.Helper()
	var matcher = AudienceMatcher{Deserializer_: DefaultAudienceDeserializer{}}
	var parsed, err = matcher.parseRules(rules)
	if err != nil {
		t.Fatalf("parseRules(%s): %v", rules, err)
	}
	return matcher.evaluateRules(parsed, environment, attrs)
}

func TestRules_EvaluateRules(t *testing.T) {
	var gold = map[string]interface{}{"tier": "gold"}
	var cases = []struct {
		name        string
		rules       string
		environment string
		want        *ruleAction
	}{
		{"no rules", `{"rules":[]}`, "", nil},
		{"first match wins", rulesJSON(`{"type":"assign","variant":1}`, `{"type":"assign","variant":2}`), "", &ruleAction{variant: 1}},
		{"conditions match", rulesJSON(`{"type":"assign","variant":1,"conditions":{"eq":[{"var":"tier"},{"value":"gold"}]}}`), "", &ruleAction{variant: 1}},
		{"conditions do not match", rulesJSON(`{"type":"assign","variant":1,"conditions":{"eq":[{"var":"tier"},{"value":"silver"}]}}`), "", nil},
		{"null conditions match", rulesJSON(`{"type":"assign","variant":1,"conditions":null}`), "", &ruleAction{variant: 1}},
		{"non-object conditions skipped", rulesJSON(`{"type":"assign","variant":1,"conditions":[{"value":true}]}`, `{"type":"assign","variant":2}`), "", &ruleAction{variant: 2}},
		{"non-object rule skipped", rulesJSON(`5`, `null`, `{"type":"assign","variant":2}`), "", &ruleAction{variant: 2}},
		{"unknown type skipped", rulesJSON(`{"type":"other","variant":1}`, `{"type":"assign","variant":2}`), "", &ruleAction{variant: 2}},
		{"missing type skipped", rulesJSON(`{"variant":1}`), "", nil},
		{"integral float variant", rulesJSON(`{"type":"assign","variant":2.0}`), "", &ruleAction{variant: 2}},
		{"fractional variant skipped", rulesJSON(`{"type":"assign","variant":1.5}`, `{"type":"assign","variant":0}`), "", &ruleAction{variant: 0}},
		{"string variant skipped", rulesJSON(`{"type":"assign","variant":"1"}`), "", nil},
		{"negative variant kept for the caller", rulesJSON(`{"type":"assign","variant":-1}`), "", &ruleAction{variant: -1}},
		{"non-list environments skipped", rulesJSON(`{"type":"assign","variant":1,"environments":"production"}`, `{"type":"assign","variant":2}`), "production", &ruleAction{variant: 2}},
		{"null environments match", rulesJSON(`{"type":"assign","variant":1,"environments":null}`), "", &ruleAction{variant: 1}},
		{"environment matches", rulesJSON(`{"type":"assign","variant":1,"environments":["staging","production"]}`), "production", &ruleAction{variant: 1}},
		{"environment is case-sensitive", rulesJSON(`{"type":"assign","variant":1,"environments":["production"]}`), "PRODUCTION", nil},
		{"no environment never matches a list", rulesJSON(`{"type":"assign","variant":1,"environments":["production"]}`), "", nil},
		{"split", rulesJSON(`{"type":"split","percentages":"20/30/50"}`), "", &ruleAction{split: []float64{0.2, 0.3, 0.5}}},
		{"split conditions do not match", rulesJSON(`{"type":"split","percentages":"20/30/50","conditions":{"value":false}}`), "", nil},
	}
	for _, tc := range cases {
		var got = evaluate(t, tc.rules, tc.environment, gold)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestRules_ParseSplitPercentages(t *testing.T) {
	var cases = []struct {
		percentages interface{}
		want        []float64
	}{
		{"20/30/50", []float64{0.2, 0.3, 0.5}},
		{"100", []float64{1}},
		{" 50/50", []float64{0.5, 0.5}},
		{"50abc/50", []float64{0.5, 0.5}},
		{"5e1/50", []float64{0.5, 0.5}},
		{"0/0/100", []float64{0, 0, 1}},
		// The gap from 100 is rounded to two decimals before the tolerance.
		{"50/50.014", []float64{0.5, 0.50014}},
		{"50/49.99", []float64{0.5, 0.4999}},
		{"50/50.015", nil},
		{"50/49.985", nil},
		{"20/30/40", nil},
		{"abc/100", nil},
		{"/100", nil},
		{"-10/110", nil},
		{"Infinity/0", nil},
		{"", nil},
		{100, nil},
		{nil, nil},
	}
	for _, tc := range cases {
		var got = parseSplitPercentages(tc.percentages)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("parseSplitPercentages(%#v) = %v, want %v", tc.percentages, got, tc.want)
		}
	}
}

// Concurrent reads, attribute changes and refreshes that change the rules must
// not race. In this fixture normal assignment always gives variant 0, while
// every rule (the gold split, the fallback assign and the refreshed assign)
// gives 1 or 2, so every exposure must be a rule win, and once the writers
// stop the settled assignment must follow the installed rules.
func TestRules_Concurrency(t *testing.T) {
	var rules = rulesJSON(
		`{"type":"split","percentages":"0/50/50","conditions":{"eq":[{"var":"tier"},{"value":"gold"}]}}`,
		`{"type":"assign","variant":1}`)
	var experiment = ruleExperiment("exp", rules)
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				switch (g + i) % 4 {
				case 0:
					_, _ = ctx.GetTreatment("exp")
				case 1:
					_, _ = ctx.GetVariableValue("color", "none")
				case 2:
					_ = ctx.SetAttribute("tier", []string{"gold", "silver"}[i%2])
				default:
					var refreshed = experiment
					if i%2 == 0 {
						refreshed.AssignmentRules = rulesJSON(`{"type":"assign","variant":2}`)
					}
					ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{refreshed}})
				}
			}
		}(g)
	}
	wg.Wait()

	var exposures = queuedExposures(ctx)
	if len(exposures) == 0 {
		t.Fatal("no exposures queued")
	}
	for _, exposure := range exposures {
		if !exposure.RuleOverride || exposure.Variant < 1 || exposure.Variant > 2 {
			t.Fatalf("exposure not chosen by a rule: %+v", exposure)
		}
	}

	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	assertAny(nil, ctx.SetAttribute("tier", "gold"), t)
	var variant, _ = ctx.PeekTreatment("exp")
	assertAny(splitVariant([]float64{0, 0.5, 0.5}, experiment.SeedHi, experiment.SeedLo), variant, t)
	assertAny(true, ctx.GetAssignment("exp").RuleOverride, t)

	assertAny(nil, ctx.SetAttribute("tier", "silver"), t)
	variant, _ = ctx.PeekTreatment("exp")
	assertAny(1, variant, t)

	var refreshed = experiment
	refreshed.AssignmentRules = rulesJSON(`{"type":"assign","variant":2}`)
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{refreshed}})
	variant, _ = ctx.PeekTreatment("exp")
	assertAny(2, variant, t)
}
