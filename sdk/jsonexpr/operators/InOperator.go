package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
	"strings"
)

type InOperator struct {
	BinaryOperator
}

func (v InOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	if lhs == nil {
		return nil
	}

	var lhsVal = reflect.ValueOf(lhs)

	if rhs == nil {
		if lhsVal.Kind() == reflect.String {
			return false
		}
		return nil
	}

	var rhsVal = reflect.ValueOf(rhs)

	if rhsVal.Kind() == reflect.Array || rhsVal.Kind() == reflect.Slice {
		for i := 0; i < rhsVal.Len(); i++ {
			var result = evaluator.Compare(reflect.ValueOf(rhsVal.Index(i).Interface()), reflect.ValueOf(lhs))
			if result != nil && reflect.ValueOf(result).Int() == 0 {
				return true
			}
		}
		return false
	} else if lhsVal.Kind() == reflect.Array || lhsVal.Kind() == reflect.Slice {
		for i := 0; i < lhsVal.Len(); i++ {
			var result = evaluator.Compare(reflect.ValueOf(lhsVal.Index(i).Interface()), reflect.ValueOf(rhs))
			if result != nil && reflect.ValueOf(result).Int() == 0 {
				return true
			}
		}
		return false
	} else if lhsVal.Kind() == reflect.Map {
		var rhsString, error = evaluator.StringConvert(rhsVal)
		return error == nil && lhsVal.MapIndex(reflect.ValueOf(rhsString)).IsValid()
	} else if rhsVal.Kind() == reflect.Map {
		var lhsString, error = evaluator.StringConvert(lhsVal)
		return error == nil && rhsVal.MapIndex(reflect.ValueOf(lhsString)).IsValid()
	} else if lhsVal.Kind() == reflect.String {
		var rhsString, error = evaluator.StringConvert(rhsVal)
		return error == nil && strings.Contains(lhsVal.String(), rhsString)
	} else if rhsVal.Kind() == reflect.String {
		var lhsString, error = evaluator.StringConvert(lhsVal)
		return error == nil && strings.Contains(rhsVal.String(), lhsString)
	}
	return nil
}
