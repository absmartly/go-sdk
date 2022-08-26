package jsonTest

import (
	"github.com/absmartly/go-sdk/main/jsonexpr"
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"github.com/absmartly/go-sdk/main/jsonexpr/operators"
	"testing"
)

func TestString(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.InOperator{},
	}

	assert(true, binary.BinaryOp.Binary(eval, "abcsfbd", "abc"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "def"), t)
	assert(false, binary.BinaryOp.Binary(eval, "abcdefghijk", "xxx"), t)
	assert(false, binary.BinaryOp.Binary(eval, "abcdefghijk", nil), t)
	assert(nil, binary.BinaryOp.Binary(eval, nil, "abc"), t)
}

func TestArrayEmpty(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.InOperator{},
	}

	assert(false, binary.BinaryOp.Binary(eval, []interface{}{}, 1), t)
	assert(false, binary.BinaryOp.Binary(eval, []interface{}{}, "1"), t)
	assert(false, binary.BinaryOp.Binary(eval, []interface{}{}, false), t)
	assert(false, binary.BinaryOp.Binary(eval, []interface{}{}, true), t)
}

func TestArrayCompares(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.InOperator{},
	}

	assert(false, binary.BinaryOp.Binary(eval, []interface{}{0, 1}, 2), t)
	assert(false, binary.BinaryOp.Binary(eval, []interface{}{0, 1}, 3), t)
	assert(true, binary.BinaryOp.Binary(eval, []interface{}{1, 2}, 1), t)
	assert(true, binary.BinaryOp.Binary(eval, []interface{}{1, 2}, 2), t)
}

func TestObject(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.InOperator{},
	}

	assert(false, binary.BinaryOp.Binary(eval, map[string]interface{}{"a": 1, "b": 2}, "c"), t)
	assert(false, binary.BinaryOp.Binary(eval, map[string]interface{}{"b": 3, "c": 3, "0": 100}, "a"), t)
	assert(true, binary.BinaryOp.Binary(eval, map[string]interface{}{"b": 3, "c": 3, "0": 100}, "b"), t)
	assert(true, binary.BinaryOp.Binary(eval, map[string]interface{}{"b": 3, "c": 3, "0": 100}, "c"), t)
	assert(true, binary.BinaryOp.Binary(eval, map[string]interface{}{"b": 3, "c": 3, "0": 100}, 0), t)
}
