package operators

import (
	"github.com/absmartly/go-sdk/main/jsonexpr/eval"
)

type ValueOperator struct {
	eval.Operator
}

func (v ValueOperator) Evaluate(evaluator eval.Evaluator, value interface{}) interface{} {
	return value
}
