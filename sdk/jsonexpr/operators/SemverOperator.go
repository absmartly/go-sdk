package operators

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"reflect"
)

type SemverEqualsOperator struct {
	BinaryOperator
}

func (v SemverEqualsOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result.(int) == 0
	}
	return nil
}

type SemverGreaterThanOperator struct {
	BinaryOperator
}

func (v SemverGreaterThanOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result.(int) > 0
	}
	return nil
}

type SemverGreaterThanOrEqualOperator struct {
	BinaryOperator
}

func (v SemverGreaterThanOrEqualOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result.(int) >= 0
	}
	return nil
}

type SemverLessThanOperator struct {
	BinaryOperator
}

func (v SemverLessThanOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result.(int) < 0
	}
	return nil
}

type SemverLessThanOrEqualOperator struct {
	BinaryOperator
}

func (v SemverLessThanOrEqualOperator) Binary(evaluator eval.Evaluator, lhs interface{}, rhs interface{}) interface{} {
	var result = evaluator.VersionCompare(reflect.ValueOf(lhs), reflect.ValueOf(rhs))
	if result != nil {
		return result.(int) <= 0
	}
	return nil
}
