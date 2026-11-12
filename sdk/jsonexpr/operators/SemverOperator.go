package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
)

type SemverOperator struct{ Mode int }

func (o SemverOperator) Binary(_ eval.Evaluator, lhs, rhs interface{}) interface{} {
	c, ok := eval.CompareSemver(lhs, rhs)
	if !ok {
		return nil
	}
	switch o.Mode {
	case 0:
		return c == 0
	case 1:
		return c > 0
	case 2:
		return c >= 0
	case 3:
		return c < 0
	default:
		return c <= 0
	}
}
