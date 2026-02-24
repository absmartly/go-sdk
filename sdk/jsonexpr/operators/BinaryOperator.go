package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type BinaryOperatorInterface interface {
	Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{}
}

type BinaryOperator struct {
	BinaryOp BinaryOperatorInterface
}

func (v BinaryOperator) Evaluate(evaluator eval.Evaluator, args interface{}) interface{} {
	var rt = reflect.TypeOf(args)
	if rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		var argsList = reflect.ValueOf(args)

		if argsList.Len() > 0 {
			var lhs = evaluator.Evaluate(reflect.ValueOf(argsList.Index(0).Interface()))
			var rhs interface{}
			if argsList.Len() > 1 {
				rhs = evaluator.Evaluate(reflect.ValueOf(argsList.Index(1).Interface()))
			}
			if lhs != nil && rhs != nil {
				return v.BinaryOp.Binary(evaluator, lhs, rhs)
			}
			if lhs == nil && rhs == nil {
				return v.BinaryOp.Binary(evaluator, lhs, rhs)
			}
		}

	}
	return nil
}
