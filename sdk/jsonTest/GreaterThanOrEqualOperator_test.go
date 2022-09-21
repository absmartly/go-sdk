package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"testing"
)

func TestGTEEvaluate(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.GreaterThanOrEqualOperator{},
	}

	assert(nil, binary.Evaluate(eval, []interface{}{0, 0}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{1, 0}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{0, 1}), t)

	assert(nil, binary.Evaluate(eval, []interface{}{nil, nil}), t)
	assert(true, binary.Evaluate(eval, []interface{}{[]interface{}{1, 2}, []interface{}{1, 2}}), t)
	assert(true, binary.Evaluate(eval, []interface{}{[]interface{}{1, 2}, []interface{}{2, 3}}), t)

	assert(nil, binary.Evaluate(eval, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}}}), t)
	assert(nil, binary.Evaluate(eval, []interface{}{map[interface{}]interface{}{"eq": 1, "b": 2}, []interface{}{map[interface{}]interface{}{"eq": 3, "b": 4}}}), t)
}
