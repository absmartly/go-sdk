package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"testing"
)

func TestLTvaluate(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.LessThanOperator{},
	}

	assert(false, binary.BinaryOp.Binary(eval, 0, 0), t)
	assert(true, binary.BinaryOp.Binary(eval, 0, 1), t)
	assert(false, binary.BinaryOp.Binary(eval, 1, 0), t)

	assert(false, binary.BinaryOp.Binary(eval, nil, nil), t)
}
