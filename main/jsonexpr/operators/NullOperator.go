package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
)

type NullOperator struct {
	UnaryOperator
}

func (v NullOperator) Unary(evaluator eval.Evaluator, arg interface{}) interface{} {
	return arg == nil
}
