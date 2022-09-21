package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
	"strings"
)

type InOperator struct {
	BinaryOperator
}

func (v InOperator) Binary(evaluator eval.Evaluator, haystack interface{}, needle interface{}) interface{} {
	var tp = reflect.ValueOf(haystack)
	if tp.Kind() == reflect.Array || tp.Kind() == reflect.Slice {
		for i := 0; i < tp.Len(); i++ {
			var result = evaluator.Compare(reflect.ValueOf(tp.Index(i).Interface()), reflect.ValueOf(needle))
			if result != nil && reflect.ValueOf(result).Int() == 0 {
				return true
			}
		}
		return false
	} else if tp.Kind() == reflect.String {
		var needleString, error = evaluator.StringConvert(reflect.ValueOf(needle))
		return error == nil && strings.Contains(tp.String(), needleString)
	} else if tp.Kind() == reflect.Map {
		var needleString, error = evaluator.StringConvert(reflect.ValueOf(needle))
		return error == nil && tp.MapIndex(reflect.ValueOf(needleString)).IsValid()
	}
	return nil
}
