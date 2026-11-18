package sdk

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// The public QueueExposure, given GetAssignment's copy, publishes the rule
// flag of the cached assignment the copy came from.
func TestQueueExposureOfGetAssignmentCopyKeepsRuleOverride(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":1}`)))

	ctx.QueueExposure(ctx.GetAssignment("exp"))
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(1, exposure.Variant, t)
	assertAny(true, exposure.RuleOverride, t)
}

// A copy taken before a rule started matching is queued with its own
// outcome, not with the rule flag of the assignment that replaced it.
func TestQueueExposureOfStaleCopyDoesNotTakeNewRuleFlag(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))

	var stale = ctx.GetAssignment("exp")
	assertAny(0, stale.Variant, t)
	assertAny(nil, ctx.SetAttribute("country", "US"), t)
	var variant, err = ctx.PeekTreatment("exp")
	assertAny(nil, err, t)
	assertAny(2, variant, t)

	ctx.QueueExposure(stale)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(0, exposure.Variant, t)
	assertAny(false, exposure.RuleOverride, t)
}

// A rule-selected copy also keeps its flag after a later peek has replaced
// the cache with a normal assignment. Its identity outlives that cache entry.
func TestQueueExposureOfRetainedRuleCopyKeepsRuleOverride(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))
	assertAny(nil, ctx.SetAttribute("country", "US"), t)
	var retained = ctx.GetAssignment("exp")
	assertAny(2, retained.Variant, t)
	assertAny(nil, ctx.SetAttribute("country", "CA"), t)
	var variant, err = ctx.PeekTreatment("exp")
	assertAny(nil, err, t)
	assertAny(0, variant, t)

	ctx.QueueExposure(retained)
	var exposure = expectExposures(t, ctx, 1)[0]
	assertAny(2, exposure.Variant, t)
	assertAny(true, exposure.RuleOverride, t)
}

// fetchClient serves the context data on GET, so the context is created
// through ABSmartly.CreateContext (the fetch path), not CreateContextWith.
type fetchClient struct {
	systemAttrClient
	data jsonmodels.ContextData
}

func (c *fetchClient) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) { return c.data, nil })
}

// System attributes reach rules and audiences on the fetch path too.
func TestFetchPathCreateContextInjectsSystemAttributes(t *testing.T) {
	var client = &fetchClient{
		systemAttrClient: systemAttrClient{application: "web", environment: "prod"},
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{ruleExperiment("exp",
			rulesJSON(`{"type":"assign","variant":1,"conditions":{"and":[{"eq":[{"var":"application"},{"value":"web"}]},{"eq":[{"var":"environment"},{"value":"prod"}]}]}}`))}},
	}
	var abs = Create(ABSmartlyConfig{
		Client_:              client,
		ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client},
	})
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContext(config)
	ctx.WaitUntilReady()

	assertAny(1, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

// A payload that fails to decode returns what v1.1.1 returned: v1.1.1 had no
// assignmentRules field, so it ignored a malformed one and reported the
// first other invalid field. The expected texts and partial data were
// measured against the published v1.1.1.
func TestDeserializeErrorMatchesV111(t *testing.T) {
	var cases = []struct {
		payload string
		err     string
		want    []jsonmodels.Experiment
	}{
		{`{"experiments":[{"id":1,"name":"a","assignmentRules":5},{"id":"x","name":"b"}]}`,
			"json: cannot unmarshal string into Go struct field Experiment.experiments.id of type int",
			[]jsonmodels.Experiment{{Id: 1, Name: "a"}, {Name: "b"}}},
		{`{"experiments":[{"assignmentRules":5,"name":1,"id":"x"}]}`,
			"json: cannot unmarshal number into Go struct field Experiment.experiments.name of type string",
			[]jsonmodels.Experiment{{}}},
		{`{"experiments":[{"id":1,"assignmentRules":{"a":1}},{"unitType":2}]}`,
			"json: cannot unmarshal number into Go struct field Experiment.experiments.unitType of type string",
			[]jsonmodels.Experiment{{Id: 1}, {}}},
	}
	for _, tc := range cases {
		var got, err = DefaultContextDataDeserializer{}.Deserialize([]byte(tc.payload))
		if err == nil {
			t.Fatalf("%s decoded without error", tc.payload)
		}
		assertAny(tc.err, err.Error(), t)
		assertAny(tc.want, got.Experiments, t)
	}
}

// A payload whose only problem is a malformed assignmentRules decodes, with
// that experiment's rules dropped.
func TestDeserializeToleratesMalformedAssignmentRulesOnly(t *testing.T) {
	var got, err = DefaultContextDataDeserializer{}.Deserialize(
		[]byte(`{"experiments":[{"id":1,"name":"a","assignmentRules":{"a":1}},{"id":2,"name":"b","assignmentRules":"{\"rules\":[]}"}]}`))
	assertAny(nil, err, t)
	assertAny(2, len(got.Experiments), t)
	assertAny("", got.Experiments[0].AssignmentRules, t)
	assertAny(`{"rules":[]}`, got.Experiments[1].AssignmentRules, t)
}
