package jsonexpr

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"reflect"
)

var Operators = map[string]eval.Operator{
	"and": operators.BooleanCombinator{
		CombineOp: operators.AndCombinator{},
	},
	"or": operators.BooleanCombinator{
		CombineOp: operators.OrCombinator{},
	},
	"value": operators.ValueOperator{},
	"var":   operators.VarOperator{},
	"null": operators.UnaryOperator{
		Unary: operators.NullOperator{},
	},
	"not": operators.UnaryOperator{
		Unary: operators.NotOperator{},
	},
	"in": operators.BinaryOperator{
		BinaryOp: operators.InOperator{},
	},
	"match": operators.BinaryOperator{
		BinaryOp: operators.MatchOperator{},
	},
	"eq": operators.BinaryOperator{
		BinaryOp: operators.EqualsOperator{},
	},
	"gt": operators.BinaryOperator{
		BinaryOp: operators.GreaterThanOperator{},
	},
	"gte": operators.BinaryOperator{
		BinaryOp: operators.GreaterThanOrEqualOperator{},
	},
	"lt": operators.BinaryOperator{
		BinaryOp: operators.LessThanOperator{},
	},
	"lte": operators.BinaryOperator{
		BinaryOp: operators.LessThanOrEqualOperator{},
	},
	"semver_eq": operators.BinaryOperator{
		BinaryOp: operators.SemverEqualsOperator{},
	},
	"semver_gt": operators.BinaryOperator{
		BinaryOp: operators.SemverGreaterThanOperator{},
	},
	"semver_gte": operators.BinaryOperator{
		BinaryOp: operators.SemverGreaterThanOrEqualOperator{},
	},
	"semver_lt": operators.BinaryOperator{
		BinaryOp: operators.SemverLessThanOperator{},
	},
	"semver_lte": operators.BinaryOperator{
		BinaryOp: operators.SemverLessThanOrEqualOperator{},
	},
}

func EvaluateBooleanExpr(expr interface{}, vars map[string]interface{}) bool {
	var eval = eval.Evaluator{Operators: Operators, Vars: vars}
	return eval.BooleanConvert(reflect.ValueOf(eval.Evaluate(reflect.ValueOf(expr))))
}
