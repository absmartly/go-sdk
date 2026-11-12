package sdk

import (
	"encoding/json"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

func TestAssignmentRuleAssignAndSplit(t *testing.T) {
	matcher := AudienceMatcher{Deserializer_: DefaultAudienceDeserializer{}}
	assign := matcher.EvaluateRules(`{"rules":[{"type":"assign","variant":1}]}`, "prod", nil)
	if assign == nil || assign.Variant == nil || *assign.Variant != 1 {
		t.Fatalf("assign rule did not match: %#v", assign)
	}
	split := matcher.EvaluateRules(`{"rules":[{"type":"split","percentages":"20/30/50"}]}`, "prod", nil)
	if split == nil || len(split.Split) != 3 || split.Split[0] != .2 {
		t.Fatalf("split rule did not parse: %#v", split)
	}
}

func TestExposureRuleOverrideJSON(t *testing.T) {
	b, err := jsonmodels.Exposure{RuleOverride: true}.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ruleOverride"] != true {
		t.Fatalf("ruleOverride = %#v", decoded["ruleOverride"])
	}
}
