package sdk

import (
	"encoding/json"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	v111 "github.com/absmartly/go-sdk/sdk/internal/v111"
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
// assignmentRules field, so it ignored a malformed one and reported the first
// other invalid field, and malformed JSON stayed malformed. The oracle is the
// published v1.1.1 model (sdk/internal/v111, a byte-for-byte copy of its
// declarations) decoded on this toolchain, because encoding/json words its
// errors differently between Go versions.
func TestDeserializeErrorMatchesV111(t *testing.T) {
	var payloads = []string{
		`{"experiments":[{"id":1,"name":"a","assignmentRules":5},{"id":"x","name":"b"}]}`,
		`{"experiments":[{"assignmentRules":5,"name":1,"id":"x"}]}`,
		`{"experiments":[{"id":1,"assignmentRules":{"a":1}},{"unitType":2}]}`,
		// No repair of a truncated body, no dropping trailing data.
		`{"experiments":[{"id":1,"name":"a"}]`,
		`{"experiments":[{"id":1,"name":"a","assignmentRules":5}]`,
		`{"experiments":[{"id":1,"name":"a"}]}x`,
		`{"experiments":[{"id":1,"name":"a","assignmentRules":5}]}x`,
		`{"experiments":[{"id":1,"name":"a"}]}{}`,
		`{"experiments":[{"id":1,"name":"a",}]}`,
		// Whitespace before the bad value, a duplicate null experiments key.
		`{"experiments": [ {"id": "bad"} ]}`,
		`{"experiments":[{"id":"bad"}],"experiments":null}`,
		`{"experiments": [ {"assignmentRules": 5, "id": "bad"} ]}`,
		`[]`, `5`, `{"experiments":"nope"}`,
		`{"experiments":[{"id":1,"assignmentRules":"{\"rules\":[]}","iteration":2,"seedHi":3,"seedLo":4,"trafficSeedHi":5,"trafficSeedLo":6,"split":[0.4,0.6],"trafficSplit":[0,1],"fullOnVariant":1,"applications":[{"name":"app"}],"variants":[{"name":"A","config":"{}"}],"customFieldValues":[{"name":"field","type":"string","value":"kept"}],"audience":"{}","audienceStrict":true},{"assignmentRules":5,"id":"bad"}]}`,
	}
	for _, payload := range payloads {
		var legacy v111.ContextData
		var wantErr = json.Unmarshal([]byte(payload), &legacy)
		if wantErr == nil {
			t.Fatalf("%s decodes without error in v1.1.1's model", payload)
		}
		var got, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err == nil {
			t.Fatalf("%s decoded without error", payload)
		}
		assertAny(wantErr, err, t)
		assertAny(legacy.Public(), got, t)
		for _, experiment := range got.Experiments {
			assertAny("", experiment.AssignmentRules, t)
		}
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

func TestPartialDecodePreservesAllReleasedFieldsAndDropsRules(t *testing.T) {
	var payload = []byte(`{"experiments":[{"id":1,"name":"a","unitType":"session_id","iteration":2,"seedHi":3,"seedLo":4,"trafficSeedHi":5,"trafficSeedLo":6,"split":[0.4,0.6],"trafficSplit":[0,1],"fullOnVariant":1,"applications":[{"name":"app"}],"variants":[{"name":"A","config":"{}"}],"customFieldValues":[{"name":"field","type":"string","value":"kept"}],"audience":"{}","audienceStrict":true,"assignmentRules":"{\"rules\":[]}"},{"assignmentRules":5,"id":"bad"}]}`)
	var got, err = DefaultContextDataDeserializer{}.Deserialize(payload)
	if err == nil {
		t.Fatal("expected invalid id error")
	}
	assertAny([]jsonmodels.Experiment{{Id: 1, Name: "a", UnitType: "session_id", Iteration: 2,
		SeedHi: 3, SeedLo: 4, TrafficSeedHi: 5, TrafficSeedLo: 6,
		Split: []float64{0.4, 0.6}, TrafficSplit: []float64{0, 1}, FullOnVariant: 1,
		Applications:      []jsonmodels.ExperimentApplication{{Name: "app"}},
		Variants:          []jsonmodels.ExperimentVariant{{Name: "A", Config: "{}"}},
		CustomFieldValues: []jsonmodels.CustomFieldValue{{Name: "field", Type: "string", Value: "kept"}},
		Audience:          "{}", AudienceStrict: true}, {}}, got.Experiments, t)
}
