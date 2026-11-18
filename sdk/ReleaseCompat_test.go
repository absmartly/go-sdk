package sdk

import (
	"encoding/json"
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

// A payload that fails to decode returns what v1.1.1 returned: the partial
// data and error of decoding it straight into jsonmodels.ContextData, even
// when an experiment also has a non-string assignmentRules.
func TestDeserializeErrorMatchesV111(t *testing.T) {
	var payload = []byte(`{"experiments":[{"id":1,"name":"a","assignmentRules":5},{"id":"x","name":"b"}]}`)
	var want jsonmodels.ContextData
	var wantErr = json.Unmarshal(payload, &want)
	if wantErr == nil {
		t.Fatal("payload decoded without error")
	}
	var got, err = DefaultContextDataDeserializer{}.Deserialize(payload)
	assertAny(wantErr.Error(), err.Error(), t)
	assertAny(want, got, t)
}
