package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"testing"
)

func TestVarOperator(t *testing.T) {

	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{
		"d": []interface{}{1, 2, 3},
		"e": []interface{}{1, map[string]interface{}{"z": 2}, 3},
		"f": map[string]interface{}{"y": map[string]interface{}{"x": 3, "0": 10}},
		"c": false,
		"b": true,
		"a": 1}}
	var binary = operators.VarOperator{}

	assert(1, binary.Evaluate(eval, "e/0"), t)
	assert(nil, binary.Evaluate(eval, "a/0"), t)
	assert(nil, binary.Evaluate(eval, "a/b"), t)
	assert(nil, binary.Evaluate(eval, "b/0"), t)
	assert(nil, binary.Evaluate(eval, "b/e"), t)

	assert(1, binary.Evaluate(eval, "d/0"), t)
	assert(2, binary.Evaluate(eval, "d/1"), t)
	assert(3, binary.Evaluate(eval, "d/2"), t)
	assert(nil, binary.Evaluate(eval, "d/3"), t)

	assert(1, binary.Evaluate(eval, "e/0"), t)
	assert(2, binary.Evaluate(eval, "e/1/z"), t)
	assert(3, binary.Evaluate(eval, "e/2"), t)
	assert(nil, binary.Evaluate(eval, "e/1/0"), t)

}
