package sdk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// countingOperator is a jsonexpr operator that counts its evaluations and is
// always true, so a test can tell how often rule conditions are re-evaluated.
type countingOperator struct {
	count *int64
}

func (o countingOperator) Evaluate(eval.Evaluator, interface{}) interface{} {
	atomic.AddInt64(o.count, 1)
	return true
}

// withCountingOperator registers the "counted" operator for the test.
func withCountingOperator(t *testing.T) *int64 {
	t.Helper()
	var count int64
	jsonexpr.Operators["counted"] = countingOperator{&count}
	t.Cleanup(func() { delete(jsonexpr.Operators, "counted") })
	return &count
}

// After a same-outcome revalidation, the cached assignment's rule set and
// attribute sequence are acknowledged, so later reads do not re-evaluate the
// rules. Without the acknowledgement every read after the first revalidation
// re-evaluates forever.
func TestRules_RevalidationIsAcknowledged(t *testing.T) {
	var count = withCountingOperator(t)
	for _, tc := range []struct {
		name  string
		rules string
		want  int
	}{
		{"matching assign", rulesJSON(`{"type":"assign","variant":2,"conditions":{"counted":null}}`), 2},
		{"non-matching", rulesJSON(`{"type":"assign","variant":2,"conditions":{"and":[{"counted":null},{"value":false}]}}`), 0},
		{"matching split", rulesJSON(`{"type":"split","percentages":"0/50/50","conditions":{"counted":null}}`),
			splitVariant([]float64{0, 0.5, 0.5}, 55006150, 47189152)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var experiment = ruleExperiment("exp", tc.rules)
			var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
			assertAny(tc.want, treatment(t, ctx, "exp"), t)

			var steady = func(step string) {
				t.Helper()
				var before = atomic.LoadInt64(count)
				for i := 0; i < 100; i++ {
					assertAny(tc.want, treatment(t, ctx, "exp"), t)
					_, _ = ctx.PeekTreatment("exp")
				}
				if evaluated := atomic.LoadInt64(count) - before; evaluated != 0 {
					t.Errorf("%s: %d condition evaluations over 200 steady reads, want 0", step, evaluated)
				}
			}
			steady("fresh")

			assertAny(nil, ctx.SetAttribute("other", 1), t)
			var before = atomic.LoadInt64(count)
			assertAny(tc.want, treatment(t, ctx, "exp"), t)
			if atomic.LoadInt64(count) == before {
				t.Error("an attribute change did not revalidate the rules")
			}
			steady("after an unrelated attribute")

			var refreshed = experiment
			refreshed.AssignmentRules = tc.rules + " "
			ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{refreshed}})
			assertAny(tc.want, treatment(t, ctx, "exp"), t)
			steady("after a same-outcome refresh")

			ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{refreshed}})
			steady("after an identical refresh")

			expectExposures(t, ctx, 1)
		})
	}
}

// The acknowledgement keeps the assignment the caller already holds, so its
// exposed flag (and the single exposure) survive.
func TestRules_RevalidationKeepsTheSameAssignment(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
	var first = ctx.getAssignment("exp").assignment
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	if ctx.getAssignment("exp").assignment != first {
		t.Error("a same-outcome revalidation replaced the cached assignment")
	}
}

// An audience revalidated after an unrelated attribute is acknowledged too.
func TestAudienceRevalidationIsAcknowledged(t *testing.T) {
	var count = withCountingOperator(t)
	var experiment = audienceExperiment("exp", `{"filter":[{"counted":null}]}`, true)
	var ctx, _ = newSystemAttrContext("website", "production", experiment)
	assertAny(1, treatment(t, ctx, "exp"), t)
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(1, treatment(t, ctx, "exp"), t)
	var before = atomic.LoadInt64(count)
	for i := 0; i < 100; i++ {
		assertAny(1, treatment(t, ctx, "exp"), t)
	}
	assertAny(int64(0), atomic.LoadInt64(count)-before, t)
}

// Rule scope uses the configured environment; a user "environment" attribute
// only reaches conditions. A staging-scoped rule must not activate in
// production because the user set environment=staging, while a condition on
// environment == staging does match it.
func TestRules_ScopeIgnoresUserEnvironmentAttribute(t *testing.T) {
	var scoped = ruleExperiment("scoped", rulesJSON(`{"type":"assign","variant":1,"environments":["staging"]}`))
	var conditioned = ruleExperiment("conditioned", rulesJSON(
		`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"environment"},{"value":"staging"}]}}`))
	conditioned.Id = 8
	var ctx, _ = newRuleContext(t, "production", withUnit(), scoped, conditioned)
	assertAny(nil, ctx.SetAttribute("environment", "staging"), t)
	assertAny(0, treatment(t, ctx, "scoped"), t)
	assertAny(2, treatment(t, ctx, "conditioned"), t)

	// And the other way round: a production-scoped rule still applies when
	// the user sets environment to something else.
	var prod = ruleExperiment("prod", rulesJSON(`{"type":"assign","variant":1,"environments":["production"]}`))
	ctx, _ = newRuleContext(t, "production", withUnit(), prod)
	assertAny(nil, ctx.SetAttribute("environment", "staging"), t)
	assertAny(1, treatment(t, ctx, "prod"), t)
}

// The production Client passes its application and environment through the
// real Create → CreateContextWith path, not a test stub.
func TestSystemAttributes_ProductionClientWiring(t *testing.T) {
	var client = CreateClient(ClientConfig{Endpoint_: "https://localhost/v1", ApiKey_: "key",
		Application_: "website", Environment_: "production"}, HttpMock{bodyString: "{}"})
	assertAny("website", client.GetApplication(), t)
	assertAny("production", client.GetEnvironment(), t)

	var abs = Create(ABsmartlyConfig{Client_: client})
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var app = audienceExperiment("app", appFilter, true)
	var env = audienceExperiment("env", envFilter, true)
	env.Id = 2
	var scoped = ruleExperiment("scoped", rulesJSON(`{"type":"assign","variant":2,"environments":["production"]}`))
	scoped.Id = 3
	var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{app, env, scoped}})
	ctx.WaitUntilReady()
	defer ctx.Close()

	assertAny(1, treatment(t, ctx, "app"), t)
	assertAny(1, treatment(t, ctx, "env"), t)
	assertAny(2, treatment(t, ctx, "scoped"), t)
}

// The same through the builder, which creates the production Client.
func TestSystemAttributes_BuilderWiring(t *testing.T) {
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experiments":[]}`))
	}))
	defer server.Close()
	var abs, err = NewBuilder().Endpoint(server.URL + "/v1").ApiKey("key").Application("website").
		Environment("production").Build()
	assertAny(nil, err, t)
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("env", envFilter, true)}})
	ctx.WaitUntilReady()
	defer ctx.Close()
	assertAny(1, treatment(t, ctx, "env"), t)
}

// A ContextConfig carries no application or environment, so a context built
// with the public CreateContext has no system attributes.
func TestSystemAttributes_PublicCreateContextHasNone(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ft = dataFutureFor(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("env", envFilter, true)}})
	var ctx = CreateTestContext(config, ft)
	ctx.WaitUntilReady()
	assertAny(0, treatment(t, ctx, "env"), t)
}

// Unknown fields at every payload level are ignored by the default decoder,
// across two experiments, and both still assign.
func TestDefaultDeserializerIgnoresUnknownFieldsAtEveryLevel(t *testing.T) {
	var payload = `{
		"experiments": [
			{"id":1,"name":"first","unitType":"session_id","iteration":1,"seedHi":55006150,"seedLo":47189152,
			 "split":[1,0,0],"trafficSeedHi":705671872,"trafficSeedLo":212903484,"trafficSplit":[0,1],
			 "fullOnVariant":0,"futureScalar":1,"futureObject":{"a":[1,{"b":2}]},"futureArray":[{"x":1}],
			 "holdouts":[{"id":9,"unknown":true}],"switchback":{"period":60},
			 "applications":[{"name":"website","futureField":{"x":1}}],
			 "variants":[{"name":"A","config":"{}","futureField":[1]},{"name":"B","config":"{}","x":{}},{"name":"C","config":"{}"}],
			 "customFieldValues":[{"name":"f","value":"v","type":"text","futureField":1}],
			 "assignmentRules":"{\"rules\":[{\"type\":\"assign\",\"variant\":2,\"futureRuleField\":{\"a\":1},\"conditions\":{\"value\":true}}],\"futureRootField\":[1]}"},
			{"id":2,"name":"second","unitType":"session_id","iteration":1,"seedHi":55006150,"seedLo":47189152,
			 "split":[1,0,0],"trafficSeedHi":705671872,"trafficSeedLo":212903484,"trafficSplit":[0,1],
			 "applications":[{"name":"website"}],
			 "variants":[{"name":"A","config":"{}"},{"name":"B","config":"{}"},{"name":"C","config":"{}"}],
			 "assignmentRules":"{\"rules\":[{\"type\":\"future\",\"variant\":1},{\"type\":\"assign\",\"variant\":1,\"conditions\":{\"future_op\":[1]}},{\"type\":\"assign\",\"variant\":1}]}"}
		],
		"futureRoot": {"nested": [1, 2]},
		"futureRootScalar": "x"
	}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	assertAny(nil, err, t)
	assertAny(2, len(data.Experiments), t)

	var logger = &recordingLogger{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var abs = Create(ABsmartlyConfig{Client_: client, ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client}, ContextEventLogger_: logger})
	var config = CreateDefaultContextConfig()
	config.Units_ = withUnit()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, data)
	ctx.WaitUntilReady()
	assertAny(false, ctx.IsFailed(), t)
	assertAny(2, treatment(t, ctx, "first"), t)
	assertAny(1, treatment(t, ctx, "second"), t)
	assertAny(0, len(logger.errors()), t)
}

// Each assignmentRules representation that is not a rules document gives no
// rules for that experiment only.
func TestDefaultDeserializerToleratesAssignmentRulesShapes(t *testing.T) {
	for _, raw := range []string{``, `"assignmentRules":null,`, `"assignmentRules":"",`, `"assignmentRules":"{}",`,
		`"assignmentRules":"{",`, `"assignmentRules":{"rules":[{"type":"assign","variant":2}]},`,
		`"assignmentRules":[1],`, `"assignmentRules":5,`, `"assignmentRules":true,`} {
		var payload = `{"experiments":[{"id":1,"name":"bad",` + raw + `"unitType":"session_id","split":[1,0,0],"trafficSplit":[0,1],
			"variants":[{"name":"A"},{"name":"B"},{"name":"C"}]},
			{"id":2,"name":"good","unitType":"session_id","split":[1,0,0],"trafficSplit":[0,1],
			"variants":[{"name":"A"},{"name":"B"},{"name":"C"}],"assignmentRules":"{\"rules\":[{\"type\":\"assign\",\"variant\":2}]}"}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		var ctx, _ = newRuleContext(t, "production", withUnit(), data.Experiments...)
		if got := treatment(t, ctx, "bad"); got != 0 {
			t.Errorf("%s: bad variant %d, want 0", raw, got)
		}
		if got := treatment(t, ctx, "good"); got != 2 {
			t.Errorf("%s: good variant %d, want 2", raw, got)
		}
	}
}

// jsonmodels.Experiment has no UnmarshalJSON, so encoding/json keeps its
// standard behaviour: decoding into a used value only sets the fields present,
// null is a no-op, and customer structs embedding it decode their own fields.
func TestExperimentKeepsStandardJSONDecoding(t *testing.T) {
	var existing = jsonmodels.Experiment{Id: 7, Name: "existing", UnitType: "user_id", Iteration: 3,
		Split: []float64{0.5, 0.5}, Variants: []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B"}},
		AssignmentRules: `{"rules":[]}`}
	var reused = existing
	assertAny(nil, json.Unmarshal([]byte(`{"id":8}`), &reused), t)
	assertAny(8, reused.Id, t)
	assertAny("existing", reused.Name, t)
	assertAny("user_id", reused.UnitType, t)
	assertAny(3, reused.Iteration, t)
	assertAny(2, len(reused.Variants), t)
	assertAny(`{"rules":[]}`, reused.AssignmentRules, t)

	var nulled = existing
	assertAny(nil, json.Unmarshal([]byte(`null`), &nulled), t)
	assertAny(existing.Name, nulled.Name, t)
	assertAny(existing.Id, nulled.Id, t)

	var slice = []jsonmodels.Experiment{existing}
	assertAny(nil, json.Unmarshal([]byte(`[{"id":9}]`), &slice), t)
	assertAny(9, slice[0].Id, t)
	assertAny("existing", slice[0].Name, t)

	var embedded struct {
		jsonmodels.Experiment
		CustomerField string `json:"customerField"`
	}
	assertAny(nil, json.Unmarshal([]byte(`{"id":7,"customerField":"keep","assignmentRules":"{}"}`), &embedded), t)
	assertAny(7, embedded.Id, t)
	assertAny("keep", embedded.CustomerField, t)
	assertAny("{}", embedded.AssignmentRules, t)

	if _, ok := interface{}(&existing).(json.Unmarshaler); ok {
		t.Error("jsonmodels.Experiment implements json.Unmarshaler; embedding it would stop sibling fields decoding")
	}
}

// An assign rule with an integral variant outside int32 is selected and found
// out of range, so rule evaluation stops and normal assignment applies, as in
// JS. It is not skipped in favour of a later rule.
func TestRules_HugeIntegralVariantStopsAndFallsBack(t *testing.T) {
	for _, variant := range []string{"2147483648", "-2147483649", "1e20"} {
		var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp",
			rulesJSON(`{"type":"assign","variant":`+variant+`}`, `{"type":"assign","variant":2}`)))
		if got := treatment(t, ctx, "exp"); got != 0 {
			t.Errorf("variant %s: got %d, want the normal assignment 0", variant, got)
		}
		assertAny(false, ctx.getAssignment("exp").ruleOverride, t)
	}
}

func dataFutureFor(data jsonmodels.ContextData) *future.Future {
	var ft, done = future.New()
	done(data, nil)
	return ft
}
