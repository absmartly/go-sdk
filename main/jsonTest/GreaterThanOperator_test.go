package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"testing"
)

func TestGTEvaluate(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.GreaterThanOperator{},
	}

	assert(nil, binary.Evaluate(eval, []interface{}{0, 0}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{1, 0}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{0, 1}), t)

	assert(nil, binary.Evaluate(eval, []interface{}{nil, nil}), t)
	assert(false, binary.Evaluate(eval, []interface{}{[]interface{}{1, 2}, []interface{}{1, 2}}), t)
	assert(false, binary.Evaluate(eval, []interface{}{[]interface{}{1, 2}, []interface{}{2, 3}}), t)

	assert(nil, binary.Evaluate(eval, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}}}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}, []interface{}{map[interface{}]interface{}{"eq": 3, "b": 4}}}), t)
}
