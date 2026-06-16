package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type EqualsOperator struct {
	BinaryOperator
}

// Evaluate overrides the BinaryOperator skip-on-nil behavior: equality must
// evaluate both operands even when one (or both) is nil, so that eq(null, null)
// is true and eq(value, null) is false. This matches the canonical SDKs
// (javascript/python), where EqualsOperator bypasses the null short-circuit.
func (v EqualsOperator) Evaluate(evaluator eval.Evaluator, args interface{}) interface{} {
	var rt = reflect.TypeOf(args)
	if rt != nil && (rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array) {
		var argsList = reflect.ValueOf(args)
		if argsList.Len() >= 2 {
			var lhs = evaluator.Evaluate(reflect.ValueOf(argsList.Index(0).Interface()))
			var rhs = evaluator.Evaluate(reflect.ValueOf(argsList.Index(1).Interface()))
			return v.Binary(evaluator, lhs, rhs)
		}
	}
	return nil
}

func (v EqualsOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.Compare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result == 0
	} else {
		return nil
	}
}
