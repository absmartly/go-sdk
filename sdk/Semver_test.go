package sdk

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonexpr"
)

func TestSemverOperators(t *testing.T) {
	cases := []struct {
		name       string
		expression interface{}
		want       bool
	}{
		{"equal", map[string]interface{}{"semver_eq": []interface{}{map[string]interface{}{"value": "v1.2.3"}, map[string]interface{}{"value": "1.2.3+build"}}}, true},
		{"greater", map[string]interface{}{"semver_gt": []interface{}{map[string]interface{}{"value": "1.2.4"}, map[string]interface{}{"value": "1.2.3"}}}, true},
		{"prerelease", map[string]interface{}{"semver_lt": []interface{}{map[string]interface{}{"value": "1.2.3-alpha"}, map[string]interface{}{"value": "1.2.3"}}}, true},
	}
	for _, tc := range cases {
		if got := jsonexpr.EvaluateBooleanExpr(tc.expression, nil); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
