package sdk

import (
	"errors"
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"reflect"
)

type AudienceMatcher struct {
	Deserializer_ AudienceDeserializer
}

type Result struct {
	result bool
}

func (r Result) Get() bool {
	return r.result
}

func CreateResult(result bool) Result {
	return Result{result: result}
}

func (am AudienceMatcher) Evaluate(audience string, attributes map[string]interface{}) (Result, error) {
	if result, err := am.Deserializer_.Deserialize([]byte(audience), 0, len(audience)); err != nil {
		return Result{}, errors.New("can't evaluate data")
	} else {
		var filter = result["filter"]
		var kind = reflect.ValueOf(filter).Kind()
		if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array {
			return CreateResult(jsonexpr.EvaluateBooleanExpr(filter, attributes)), nil
		}
	}
	return Result{}, errors.New("can't evaluate data")
}
