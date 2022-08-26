package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"math"
	"reflect"
	"testing"
)

func TestCompareStrings(t *testing.T) {
	var eval = eval.Evaluator{}

	assert(0, eval.Compare(reflect.ValueOf(""), reflect.ValueOf("")), t)
	assert(0, eval.Compare(reflect.ValueOf("abc"), reflect.ValueOf("abc")), t)
	assert(0, eval.Compare(reflect.ValueOf("0"), reflect.ValueOf(0)), t)
	assert(0, eval.Compare(reflect.ValueOf("1"), reflect.ValueOf(1)), t)
	assert(0, eval.Compare(reflect.ValueOf("true"), reflect.ValueOf(true)), t)
	assert(0, eval.Compare(reflect.ValueOf("false"), reflect.ValueOf(false)), t)

	assert(nil, eval.Compare(reflect.ValueOf(""), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(nil, eval.Compare(reflect.ValueOf("abc"), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(nil, eval.Compare(reflect.ValueOf(""), reflect.ValueOf([]interface{}{})), t)
	assert(nil, eval.Compare(reflect.ValueOf("abc"), reflect.ValueOf([]interface{}{})), t)

	assert(-1, eval.Compare(reflect.ValueOf("abc"), reflect.ValueOf("bcd")), t)
	assert(1, eval.Compare(reflect.ValueOf("bcd"), reflect.ValueOf("abc")), t)
	assert(-1, eval.Compare(reflect.ValueOf("0"), reflect.ValueOf("1")), t)
	assert(1, eval.Compare(reflect.ValueOf("1"), reflect.ValueOf("0")), t)
	assert(1, eval.Compare(reflect.ValueOf("9"), reflect.ValueOf("100")), t)
	assert(-1, eval.Compare(reflect.ValueOf("100"), reflect.ValueOf("9")), t)
}

func TestCompareNumbers(t *testing.T) {
	var eval = eval.Evaluator{}

	assert(0, eval.Compare(reflect.ValueOf(0), reflect.ValueOf(0)), t)
	assert(-1, eval.Compare(reflect.ValueOf(0), reflect.ValueOf(1)), t)
	assert(-1, eval.Compare(reflect.ValueOf(0), reflect.ValueOf(true)), t)
	assert(0, eval.Compare(reflect.ValueOf(0), reflect.ValueOf(false)), t)

	assert(nil, eval.Compare(reflect.ValueOf(1), reflect.ValueOf("")), t)
	assert(nil, eval.Compare(reflect.ValueOf(1), reflect.ValueOf("abc")), t)
	assert(nil, eval.Compare(reflect.ValueOf(1), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(nil, eval.Compare(reflect.ValueOf(1), reflect.ValueOf([]interface{}{})), t)

	assert(0, eval.Compare(reflect.ValueOf(1.0), reflect.ValueOf(1)), t)
	assert(1, eval.Compare(reflect.ValueOf(1.5), reflect.ValueOf(1)), t)
	assert(1, eval.Compare(reflect.ValueOf(2.0), reflect.ValueOf(1)), t)
	assert(1, eval.Compare(reflect.ValueOf(3.0), reflect.ValueOf(1)), t)

	assert(0, eval.Compare(reflect.ValueOf(1), reflect.ValueOf(1)), t)
	assert(-1, eval.Compare(reflect.ValueOf(1), reflect.ValueOf(1.5)), t)
	assert(-1, eval.Compare(reflect.ValueOf(1), reflect.ValueOf(2.0)), t)
	assert(-1, eval.Compare(reflect.ValueOf(1), reflect.ValueOf(3.0)), t)

	assert(0, eval.Compare(reflect.ValueOf(9007199254740991), reflect.ValueOf(9007199254740991)), t)
	assert(-1, eval.Compare(reflect.ValueOf(0.0), reflect.ValueOf(9007199254740991)), t)
	assert(1, eval.Compare(reflect.ValueOf(9007199254740991), reflect.ValueOf(0.0)), t)

	assert(0, eval.Compare(reflect.ValueOf(9007199254740991.0), reflect.ValueOf(9007199254740991.0)), t)
	assert(-1, eval.Compare(reflect.ValueOf(0.0), reflect.ValueOf(9007199254740991.0)), t)
	assert(1, eval.Compare(reflect.ValueOf(9007199254740991.0), reflect.ValueOf(0.0)), t)

}

func TestCompareBoolean(t *testing.T) {
	var eval = eval.Evaluator{}

	assert(0, eval.Compare(reflect.ValueOf(false), reflect.ValueOf(0)), t)
	assert(-1, eval.Compare(reflect.ValueOf(false), reflect.ValueOf(1)), t)
	assert(-1, eval.Compare(reflect.ValueOf(false), reflect.ValueOf(true)), t)
	assert(0, eval.Compare(reflect.ValueOf(false), reflect.ValueOf(false)), t)
	assert(0, eval.Compare(reflect.ValueOf(false), reflect.ValueOf("")), t)
	assert(-1, eval.Compare(reflect.ValueOf(false), reflect.ValueOf("abc")), t)
	assert(-1, eval.Compare(reflect.ValueOf(false), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(-1, eval.Compare(reflect.ValueOf(false), reflect.ValueOf([]interface{}{})), t)

	assert(1, eval.Compare(reflect.ValueOf(true), reflect.ValueOf(0)), t)
	assert(0, eval.Compare(reflect.ValueOf(true), reflect.ValueOf(1)), t)
	assert(0, eval.Compare(reflect.ValueOf(true), reflect.ValueOf(true)), t)
	assert(1, eval.Compare(reflect.ValueOf(true), reflect.ValueOf(false)), t)
	assert(1, eval.Compare(reflect.ValueOf(true), reflect.ValueOf("")), t)
	assert(0, eval.Compare(reflect.ValueOf(true), reflect.ValueOf("abc")), t)
	assert(0, eval.Compare(reflect.ValueOf(true), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(0, eval.Compare(reflect.ValueOf(true), reflect.ValueOf([]interface{}{})), t)
}

func TestCompareObjects(t *testing.T) {
	var eval = eval.Evaluator{}

	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf(0)), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf(1)), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf(true)), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf(false)), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf("")), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf("abc")), t)
	assert(0, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(0, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{"a": 1}), reflect.ValueOf(map[interface{}]interface{}{"a": 1})), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{"a": 1}), reflect.ValueOf(map[interface{}]interface{}{"b": 2})), t)
	assert(nil, eval.Compare(reflect.ValueOf(map[interface{}]interface{}{}), reflect.ValueOf([]interface{}{})), t)

	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf(0)), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf(1)), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf(true)), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf(false)), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf("")), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf("abc")), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(0, eval.Compare(reflect.ValueOf([]interface{}{}), reflect.ValueOf([]interface{}{})), t)
	assert(0, eval.Compare(reflect.ValueOf([]interface{}{1, 2}), reflect.ValueOf([]interface{}{1, 2})), t)
	assert(nil, eval.Compare(reflect.ValueOf([]interface{}{1, 2}), reflect.ValueOf([]interface{}{3, 4})), t)
}

func TestCompareNull(t *testing.T) {
	var eval = eval.Evaluator{}
	assert(0, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(nil)), t)

	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(0)), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(1)), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(true)), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(false)), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf("")), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf("abc")), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(nil, eval.Compare(reflect.ValueOf(nil), reflect.ValueOf([]interface{}{})), t)

}

func TestExtractVar(t *testing.T) {
	var eval = eval.Evaluator{
		Operators: map[string]eval.Operator{},
		Vars: map[string]interface{}{
			"d": []interface{}{1, 2, 3},
			"e": []interface{}{1, map[string]interface{}{"z": 2}, 3},
			"f": map[string]interface{}{"y": map[string]interface{}{"x": 3, "0": 10}},
			"c": false,
			"b": true,
			"a": 1,
		},
	}

	assert(1, eval.ExtractVar("a"), t)
	assert(true, eval.ExtractVar("b"), t)
	assert(false, eval.ExtractVar("c"), t)
	assert([]int{1, 2, 3}, eval.ExtractVar("d"), t)
	assert([]interface{}{1, map[string]int{"z": 2}, 3}, eval.ExtractVar("e"), t)
	assert(map[string]interface{}{"y": map[string]int{"x": 3, "0": 10}}, eval.ExtractVar("f"), t)

	assert(nil, eval.ExtractVar("a/0"), t)
	assert(nil, eval.ExtractVar("a/b"), t)
	assert(nil, eval.ExtractVar("b/0"), t)
	assert(nil, eval.ExtractVar("b/e"), t)

	assert(1, eval.ExtractVar("d/0"), t)
	assert(2, eval.ExtractVar("d/1"), t)
	assert(3, eval.ExtractVar("d/2"), t)
	assert(nil, eval.ExtractVar("d/3"), t)

	assert(1, eval.ExtractVar("e/0"), t)
	assert(2, eval.ExtractVar("e/1/z"), t)
	assert(3, eval.ExtractVar("e/2"), t)
	assert(nil, eval.ExtractVar("e/1/0"), t)

	assert(map[string]interface{}{"x": 3, "0": 10}, eval.ExtractVar("f/y"), t)
	assert(3, eval.ExtractVar("f/y/x"), t)
	assert(10, eval.ExtractVar("f/y/0"), t)

}

func TestStringConvert(t *testing.T) {
	var eval = eval.Evaluator{}

	result, error := eval.StringConvert(reflect.ValueOf(nil))
	assert("can't convert string", error.Error(), t)
	assert("", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(map[interface{}]interface{}{}))
	assert("can't convert string", error.Error(), t)
	assert("", result, t)

	result, error = eval.StringConvert(reflect.ValueOf([]interface{}{}))
	assert("can't convert string", error.Error(), t)
	assert("", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(true))
	assert(nil, error, t)
	assert("true", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(false))
	assert(nil, error, t)
	assert("false", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(""))
	assert(nil, error, t)
	assert("", result, t)

	result, error = eval.StringConvert(reflect.ValueOf("abc"))
	assert(nil, error, t)
	assert("abc", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-1.0))
	assert(nil, error, t)
	assert("-1", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(0.0))
	assert(nil, error, t)
	assert("0", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(1.0))
	assert(nil, error, t)
	assert("1", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(1.5))
	assert(nil, error, t)
	assert("1.5", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(2.0))
	assert(nil, error, t)
	assert("2", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(3.0))
	assert(nil, error, t)
	assert("3", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(2147483647))
	assert(nil, error, t)
	assert("2147483647", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-2147483647))
	assert(nil, error, t)
	assert("-2147483647", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(9007199254740991.0))
	assert(nil, error, t)
	assert("9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-9007199254740991.0))
	assert(nil, error, t)
	assert("-9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(0.9007199254740991))
	assert(nil, error, t)
	assert("0.9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(0.9007199254740991))
	assert(nil, error, t)
	assert("0.9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-0.9007199254740991))
	assert(nil, error, t)
	assert("-0.9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-1))
	assert(nil, error, t)
	assert("-1", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(0))
	assert(nil, error, t)
	assert("0", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(1))
	assert(nil, error, t)
	assert("1", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(2))
	assert(nil, error, t)
	assert("2", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(3))
	assert(nil, error, t)
	assert("3", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(2147483647))
	assert(nil, error, t)
	assert("2147483647", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-2147483647))
	assert(nil, error, t)
	assert("-2147483647", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(9007199254740991))
	assert(nil, error, t)
	assert("9007199254740991", result, t)

	result, error = eval.StringConvert(reflect.ValueOf(-9007199254740991))
	assert(nil, error, t)
	assert("-9007199254740991", result, t)

}

func TestNumberConvert(t *testing.T) {
	var eval = eval.Evaluator{}

	result, error := eval.NumberConvert(reflect.ValueOf(nil))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(map[interface{}]interface{}{}))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf([]interface{}{}))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(""))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("abcd"))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("x1234"))
	assert("can't convert number", error.Error(), t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(true))
	assert(nil, error, t)
	assert(1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(false))
	assert(nil, error, t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-1.0))
	assert(nil, error, t)
	assert(-1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(0.0))
	assert(nil, error, t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(1.0))
	assert(nil, error, t)
	assert(1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(1.5))
	assert(nil, error, t)
	assert(1.5, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(2.0))
	assert(nil, error, t)
	assert(2.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(3.0))
	assert(nil, error, t)
	assert(3.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(2147483647))
	assert(nil, error, t)
	assert(2147483647.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-2147483647))
	assert(nil, error, t)
	assert(-2147483647.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(9007199254740991.0))
	assert(nil, error, t)
	assert(9007199254740991.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-9007199254740991.0))
	assert(nil, error, t)
	assert(-9007199254740991.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(0.9007199254740991))
	assert(nil, error, t)
	assert(0.9007199254740991, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(0.9007199254740991))
	assert(nil, error, t)
	assert(0.9007199254740991, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-0.9007199254740991))
	assert(nil, error, t)
	assert(-0.9007199254740991, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-1))
	assert(nil, error, t)
	assert(-1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(0))
	assert(nil, error, t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(1))
	assert(nil, error, t)
	assert(1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(2))
	assert(nil, error, t)
	assert(2.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(3))
	assert(nil, error, t)
	assert(3.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(2147483647))
	assert(nil, error, t)
	assert(2147483647.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-2147483647))
	assert(nil, error, t)
	assert(-2147483647.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(9007199254740991))
	assert(nil, error, t)
	assert(9007199254740991.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-9007199254740991))
	assert(nil, error, t)
	assert(-9007199254740991.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(math.MaxFloat64))
	assert(nil, error, t)
	assert(math.MaxFloat64, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf(-math.MaxFloat64))
	assert(nil, error, t)
	assert(-math.MaxFloat64, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("-1"))
	assert(nil, error, t)
	assert(-1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("0"))
	assert(nil, error, t)
	assert(0.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("1"))
	assert(nil, error, t)
	assert(1.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("1.5"))
	assert(nil, error, t)
	assert(1.5, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("2.0"))
	assert(nil, error, t)
	assert(2.0, result, t)

	result, error = eval.NumberConvert(reflect.ValueOf("3.0"))
	assert(nil, error, t)
	assert(3.0, result, t)

}

func TestBooleanConvert(t *testing.T) {
	var eval = eval.Evaluator{}

	assert(true, eval.BooleanConvert(reflect.ValueOf(map[interface{}]interface{}{})), t)
	assert(true, eval.BooleanConvert(reflect.ValueOf([]interface{}{})), t)
	assert(false, eval.BooleanConvert(reflect.ValueOf(nil)), t)

	assert(true, eval.BooleanConvert(reflect.ValueOf(true)), t)
	assert(true, eval.BooleanConvert(reflect.ValueOf(1)), t)
	assert(true, eval.BooleanConvert(reflect.ValueOf(2)), t)
	assert(true, eval.BooleanConvert(reflect.ValueOf("abc")), t)
	assert(true, eval.BooleanConvert(reflect.ValueOf("1")), t)

	assert(false, eval.BooleanConvert(reflect.ValueOf(false)), t)
	assert(false, eval.BooleanConvert(reflect.ValueOf(0)), t)
	assert(false, eval.BooleanConvert(reflect.ValueOf("")), t)
	assert(false, eval.BooleanConvert(reflect.ValueOf("0")), t)
	assert(false, eval.BooleanConvert(reflect.ValueOf("false")), t)

}

func TestEvaluateCallsOperatorWithArgs(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": []int{1, 2, 3}}}

	assert([]int{1, 2, 3}, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"value": []int{1, 2, 3}})), t)
	assert([]int{1, 2, 3}, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"value": []int{1, 2, 3}})), t)
	assert([]int{1, 2, 3}, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"value": []int{1, 2, 3}})), t)
}

func TestEvaluateReturnsNullIfOperatorNotFound(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": []int{1, 2, 3}}}

	assert(nil, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"not_found": []int{1, 2, 3}})), t)
	assert(nil, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"not_found": []int{1, 2, 3}})), t)
	assert(nil, eval.Evaluate(reflect.ValueOf(map[string]interface{}{"not_found": []int{1, 2, 3}})), t)
}

func TestEvaluateConsidersListAsAndCombinator(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	assert(false, eval.Evaluate(reflect.ValueOf([]interface{}{map[string]interface{}{"value": true}, map[string]interface{}{"value": false}})), t)
}

func assert(want interface{}, got interface{}, t *testing.T) {
	var wanttp = reflect.ValueOf(want)
	var gottp = reflect.ValueOf(got)
	if wanttp.Kind() != gottp.Kind() {
		t.Errorf("got %q, wanted %q", got, want)
		return
	}
	if gottp.Kind() == reflect.Array || gottp.Kind() == reflect.Slice {

		if gottp.Len() != wanttp.Len() {
			t.Errorf("got %q, wanted %q", got, want)
			return
		}
		for i := 0; i < gottp.Len(); i++ {
			if reflect.ValueOf(gottp.Index(i).Interface()).Kind() == reflect.ValueOf(wanttp.Index(i).Interface()).Kind() &&
				reflect.ValueOf(gottp.Index(i).Interface()).Kind() == reflect.Array ||
				reflect.ValueOf(gottp.Index(i).Interface()).Kind() == reflect.Slice ||
				reflect.ValueOf(gottp.Index(i).Interface()).Kind() == reflect.Map {
				assert(wanttp.Index(i).Interface(), gottp.Index(i).Interface(), t)
				return
			}
			if gottp.Index(i).Interface() != wanttp.Index(i).Interface() {
				t.Errorf("got %q, wanted %q", got, want)
				return
			}
		}
		return
	} else if gottp.Kind() == reflect.Map {
		if gottp.Len() != wanttp.Len() {
			t.Errorf("got %q, wanted %q", got, want)
			return
		}

		var entry = gottp.MapRange()
		for entry.Next() != false {
			var rentry = wanttp.MapIndex(reflect.ValueOf(entry.Key().Interface()))
			if reflect.ValueOf(entry.Value().Interface()).Kind() == reflect.ValueOf(rentry.Interface()).Kind() &&
				reflect.ValueOf(entry.Value().Interface()).Kind() == reflect.Array ||
				reflect.ValueOf(entry.Value().Interface()).Kind() == reflect.Slice ||
				reflect.ValueOf(entry.Value().Interface()).Kind() == reflect.Map {
				assert(entry.Value().Interface(), rentry.Interface(), t)
				continue
			}
			if rentry.Interface() != entry.Value().Interface() {
				t.Errorf("got %q, wanted %q", got, want)
				return
			}
		}
		return
	} else if got != want {
		t.Errorf("got %q, wanted %q", got, want)
	}
}
