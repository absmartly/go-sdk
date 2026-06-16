package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type EqualsOperator struct {
	BinaryOperator
}

func (v EqualsOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.Compare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result == 0
	} else {
		return nil
	}
}
