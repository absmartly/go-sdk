package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
)

type NullOperator struct {
	UnaryOperator
}

func (v NullOperator) Unary(evaluator eval.Evaluator, arg interface{}) interface{} {
	return arg == nil
}
