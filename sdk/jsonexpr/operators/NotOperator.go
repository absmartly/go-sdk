package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type NotOperator struct {
	UnaryOperator
}

func (v NotOperator) Unary(evaluator eval.Evaluator, arg interface{}) interface{} {
	if arg == nil {
		return nil
	}
	return !evaluator.BooleanConvert(reflect.ValueOf(arg))
}
