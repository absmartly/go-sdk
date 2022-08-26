package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"testing"
)

func TestMatch(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.MatchOperator{},
	}

	assert(true, binary.BinaryOp.Binary(eval, 0, 0), t)
	assert(false, binary.BinaryOp.Binary(eval, 0, 1), t)
	assert(false, binary.BinaryOp.Binary(eval, 1, 0), t)

	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", ""), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "abc"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "ijk"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "^abc"), t)

	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "ijk$"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "def"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "b.*j"), t)
	assert(false, binary.BinaryOp.Binary(eval, "abcdefghijk", "xyz"), t)

	assert(nil, binary.BinaryOp.Binary(eval, nil, "abc"), t)
	assert(nil, binary.BinaryOp.Binary(eval, "abcdefghijk", nil), t)
}
