package jsonTest

import (
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
)

func versionCompare(lhs interface{}, rhs interface{}) interface{} {
	return eval.Evaluator{}.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
}

func TestVersionCompare(t *testing.T) {
	var cases = []struct {
		lhs  interface{}
		rhs  interface{}
		want interface{}
	}{
		{"1.0.0", "1.0.0", 0},
		{"0.0.0", "0.0.0", 0},
		{"999.999.999", "999.999.999", 0},

		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.2.0", "1.1.0", 1},
		{"1.1.0", "1.2.0", -1},
		{"1.0.2", "1.0.1", 1},
		{"1.0.1", "1.0.2", -1},

		{"1.10.0", "1.9.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"10.0.0", "9.0.0", 1},

		{"1.2", "1.2.0", 0},
		{"1", "1.0.0", 0},
		{"1.2.0", "1.2", 0},
		{"1.0.0", "1", 0},

		{"1.02.0", "1.2.0", 0},
		{"1.002.030", "1.2.30", 0},
		{"01.0.0", "1.0.0", 0},
		{"1.02.0", "1.3.0", -1},
		{"1.00.0", "1.0.0", 0},

		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-alpha.2", "1.0.0-alpha.1", 1},
		{"1.0.0-1", "1.0.0-2", -1},
		{"1.0.0-2", "1.0.0-1", 1},
		{"1.0.0-rc-1", "1.0.0-rc-1", 0},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},
		{"1.0.0-", "1.0.0", 0},

		{"v1.0.0", "1.0.0", 0},
		{"V1.0.0", "1.0.0", 0},
		{"v1.0.0", "V1.0.0", 0},

		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0+build1", "1.0.0", 0},
		{"1.0.0-alpha+build1", "1.0.0-alpha+build2", 0},
		{"1.0.0-alpha+build1", "1.0.0-beta", -1},
		{"1.0.0-alpha+build1", "1.0.0", -1},

		// Non-numeric core parts compare as alphanumeric identifiers.
		{"1.x", "1.0", 1},
		{"abc", "1.0.0", 1},

		{1, "1.0.0", 0},
		{"1.0.0", 1, 0},
		{1.0, "1.0.0", 0},
		{2.5, "2.5.0", 0},
		{true, "true", 0},

		{nil, "1.0.0", nil},
		{"1.0.0", nil, nil},
		{nil, nil, nil},
		{map[string]interface{}{}, "1.0.0", nil},
		{"1.0.0", []interface{}{}, nil},

		{"", "1.0.0", nil},
		{"1.0.0", "", nil},
		{"", "", nil},
		{"v", "1.0.0", nil},
		{"V", "1.0.0", nil},
		{"+build", "1.0.0", nil},
		{"v+build", "1.0.0", nil},
		{"1.0.0", "v", nil},
		{"-alpha", "1.0.0", nil},
	}
	for _, tc := range cases {
		if got := versionCompare(tc.lhs, tc.rhs); !reflect.DeepEqual(tc.want, got) {
			t.Errorf("VersionCompare(%#v, %#v) = %#v, want %#v", tc.lhs, tc.rhs, got, tc.want)
		}
	}
}

func TestSemverOperators(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{}}
	var cases = []struct {
		op   operators.BinaryOperatorInterface
		want [3]interface{} // lhs less than, equal to and greater than rhs
	}{
		{operators.SemverEqualsOperator{}, [3]interface{}{false, true, false}},
		{operators.SemverGreaterThanOperator{}, [3]interface{}{false, false, true}},
		{operators.SemverGreaterThanOrEqualOperator{}, [3]interface{}{false, true, true}},
		{operators.SemverLessThanOperator{}, [3]interface{}{true, false, false}},
		{operators.SemverLessThanOrEqualOperator{}, [3]interface{}{true, true, false}},
	}
	for _, tc := range cases {
		var binary = operators.BinaryOperator{BinaryOp: tc.op}
		assert(tc.want[0], binary.Evaluate(eval, []interface{}{map[string]interface{}{"value": "1.0.0"}, map[string]interface{}{"value": "2.0.0"}}), t)
		assert(tc.want[1], binary.Evaluate(eval, []interface{}{map[string]interface{}{"value": "v1.0.0"}, map[string]interface{}{"value": "1.0+build"}}), t)
		assert(tc.want[2], binary.Evaluate(eval, []interface{}{map[string]interface{}{"value": "1.10.0"}, map[string]interface{}{"value": "1.9.0"}}), t)

		assert(nil, binary.Evaluate(eval, []interface{}{nil, map[string]interface{}{"value": "1.0.0"}}), t)
		assert(nil, binary.Evaluate(eval, []interface{}{map[string]interface{}{"value": "1.0.0"}, nil}), t)
		assert(nil, binary.Evaluate(eval, []interface{}{map[string]interface{}{"value": ""}, map[string]interface{}{"value": "1.0.0"}}), t)
	}
}

func TestSemverOperatorsInExpressions(t *testing.T) {
	var expr = []interface{}{
		map[string]interface{}{"semver_gte": []interface{}{map[string]interface{}{"var": "app_version"}, map[string]interface{}{"value": "2.4.0"}}},
		map[string]interface{}{"semver_lt": []interface{}{map[string]interface{}{"var": "app_version"}, map[string]interface{}{"value": "3.0.0"}}},
	}
	for version, want := range map[string]bool{"2.4.0": true, "2.10.1": true, "2.4.0-beta": false, "3.0.0": false, "1.9": false} {
		if got := jsonexpr.EvaluateBooleanExpr(expr, map[string]interface{}{"app_version": version}); got != want {
			t.Errorf("app_version %s: got %v, want %v", version, got, want)
		}
	}
	assert(false, jsonexpr.EvaluateBooleanExpr(expr, map[string]interface{}{}), t)
}
