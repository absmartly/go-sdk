package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"reflect"
)

type AndCombinator struct {
	BooleanCombinator
}

func (v AndCombinator) Combine(evaluator eval.Evaluator, args reflect.Value) bool {
	for i := 0; i < args.Len(); i++ {
		if !evaluator.BooleanConvert(reflect.ValueOf(evaluator.Evaluate(reflect.ValueOf(args.Index(i).Interface())))) {
			return false
		}
	}
	return true
}
