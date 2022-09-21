package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type GreaterThanOperator struct {
	BinaryOperator
}

func (v GreaterThanOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.Compare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return reflect.ValueOf(result).Int() > 0
	} else {
		return nil
	}
}
