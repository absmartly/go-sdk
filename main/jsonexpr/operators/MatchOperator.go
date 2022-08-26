package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
	"reflect"
	"regexp"
)

type MatchOperator struct {
	BinaryOperator
}

func (v MatchOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var text, lerror = evaluator.StringConvert(reflect.ValueOf(lhs))
	if lerror == nil {
		var pattern, rerror = evaluator.StringConvert(reflect.ValueOf(rhs))
		if rerror == nil {
			regex, regerror := regexp.Compile(pattern)
			if regerror == nil {
				return regex.MatchString(text)
			}
		}
	}
	return nil
}
