package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"testing"
)

func TestValueOperator(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.ValueOperator{}

	assert(0, binary.Evaluate(eval, 0), t)
	assert(1, binary.Evaluate(eval, 1), t)
	assert(true, binary.Evaluate(eval, true), t)
	assert(false, binary.Evaluate(eval, false), t)
	assert("", binary.Evaluate(eval, ""), t)
	assert("abc", binary.Evaluate(eval, "abc"), t)
	assert(map[interface{}]interface{}{}, binary.Evaluate(eval, map[interface{}]interface{}{}), t)
	assert([]interface{}{}, binary.Evaluate(eval, []interface{}{}), t)
	assert(nil, binary.Evaluate(eval, nil), t)

}
