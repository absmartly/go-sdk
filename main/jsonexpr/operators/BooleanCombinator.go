package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"reflect"
)

type BooleanCombinatorInterface interface {
	Combine(evaluator eval.Evaluator, args reflect.Value) bool
}

type BooleanCombinator struct {
	CombineOp BooleanCombinatorInterface
}

func (v BooleanCombinator) Evaluate(evaluator eval.Evaluator, args interface{}) interface{} {
	var rt = reflect.TypeOf(args)
	if rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		var values = reflect.ValueOf(args)
		elemSlice := reflect.MakeSlice(reflect.SliceOf(rt), 0, values.Len())
		for i := 0; i < values.Len(); i++ {
			elemSlice = reflect.Append(elemSlice, reflect.ValueOf([]interface{}{values.Index(i)}))
		}
		return v.CombineOp.Combine(evaluator, reflect.ValueOf(args))
	}
	return nil
}
