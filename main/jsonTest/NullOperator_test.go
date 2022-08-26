package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"testing"
)

func TestNullOperator(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.UnaryOperator{
		Unary: operators.NullOperator{},
	}

	assert(true, binary.Unary.Unary(eval, nil), t)
	assert(false, binary.Unary.Unary(eval, true), t)
	assert(false, binary.Unary.Unary(eval, false), t)
	assert(false, binary.Unary.Unary(eval, 0), t)
}
