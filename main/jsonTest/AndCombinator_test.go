package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"reflect"
	"testing"
)

func TestCombineTrue(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var combinator = operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	}
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{true})), t)

}

func TestCombineFalse(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var combinator = operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	}
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false})), t)
}

func TestCombineNull(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var combinator = operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	}
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{nil})), t)
}

func TestCombineShortCircuit(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var combinator = operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	}
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{true, false, true})), t)
}

func TestCombine(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var combinator = operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	}
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{true, true})), t)
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{true, true, true})), t)

	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{true, false})), t)
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, true})), t)
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, false})), t)
	assert(false, combinator.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, false, false})), t)
}
