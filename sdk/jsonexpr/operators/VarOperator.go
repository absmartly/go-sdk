package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type VarOperator struct {
	eval.Operator
}

func (v VarOperator) Evaluate(evaluator eval.Evaluator, path interface{}) interface{} {
	var tp = reflect.ValueOf(path)

	if tp.Kind() == reflect.Map {
		pathValue := tp.MapIndex(reflect.ValueOf("path"))
		if !pathValue.IsValid() {
			return nil
		}
		path = pathValue.Interface()
	}

	var pth = reflect.ValueOf(path)
	if pth.Kind() == reflect.String {
		return evaluator.ExtractVar(pth.String())
	}
	return nil
}
