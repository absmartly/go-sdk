package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"reflect"
)

type UnaryOperatorInterface interface {
	Unary(evaluator eval.Evaluator, arg interface{}) interface{}
}

type UnaryOperator struct {
	Unary UnaryOperatorInterface
}

func (v UnaryOperator) Evaluate(evaluator eval.Evaluator, args interface{}) interface{} {
	var arg = evaluator.Evaluate(reflect.ValueOf(args))
	return v.Unary.Unary(evaluator, arg)
}
