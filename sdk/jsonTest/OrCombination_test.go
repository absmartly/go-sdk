package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"reflect"
	"testing"
)

func TestOrOperator(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BooleanCombinator{
		CombineOp: operators.OrCombinator{},
	}

	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{false}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true}})), t)

	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true, true}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true, true, true}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{[]interface{}{true, false}})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, true})), t)

	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, false})), t)
	assert(false, binary.CombineOp.Combine(eval, reflect.ValueOf([]interface{}{false, false, false})), t)
}
