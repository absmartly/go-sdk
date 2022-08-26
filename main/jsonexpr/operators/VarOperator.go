package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"reflect"
)

type VarOperator struct {
	eval.Operator
}

func (v VarOperator) Evaluate(evaluator eval.Evaluator, path interface{}) interface{} {
	var tp = reflect.ValueOf(path)

	if tp.Kind() == reflect.Map {
		path = tp.MapIndex(reflect.ValueOf("path")).Interface()
	}

	var pth = reflect.ValueOf(path)
	if pth.Kind() == reflect.String {
		return evaluator.ExtractVar(pth.String())
	} else {
		return nil
	}
}
