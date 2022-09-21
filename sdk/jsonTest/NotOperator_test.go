package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"testing"
)

func TestNotOperator(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.UnaryOperator{
		Unary: operators.NotOperator{},
	}

	assert(true, binary.Unary.Unary(eval, false), t)
	assert(false, binary.Unary.Unary(eval, true), t)
	assert(nil, binary.Unary.Unary(eval, nil), t)
}
