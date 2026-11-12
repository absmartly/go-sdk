package sdk

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/absmartly/go-sdk/sdk/jsonexpr"
)

type AudienceMatcher struct{ Deserializer_ AudienceDeserializer }
type Result struct{ result bool }

func (r Result) Get() bool            { return r.result }
func CreateResult(result bool) Result { return Result{result: result} }

type RuleAction struct {
	Variant *int
	Split   []float64
}

func (am AudienceMatcher) Evaluate(audience string, attributes map[string]interface{}) (Result, error) {
	result, err := am.Deserializer_.Deserialize([]byte(audience), 0, len(audience))
	if err != nil {
		return Result{}, errors.New("can't evaluate data")
	}
	filter := result["filter"]
	kind := reflect.ValueOf(filter).Kind()
	if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array {
		return CreateResult(jsonexpr.EvaluateBooleanExpr(filter, attributes)), nil
	}
	return Result{}, errors.New("can't evaluate data")
}

func (am AudienceMatcher) EvaluateRules(rulesString, environment string, attributes map[string]interface{}) *RuleAction {
	var document struct {
		Rules []map[string]interface{} `json:"rules"`
	}
	if json.Unmarshal([]byte(rulesString), &document) != nil {
		return nil
	}
	for _, rule := range document.Rules {
		var action RuleAction
		switch rule["type"] {
		case "assign":
			value, ok := rule["variant"].(float64)
			if !ok || value != math.Trunc(value) {
				continue
			}
			variant := int(value)
			action.Variant = &variant
		case "split":
			text, ok := rule["percentages"].(string)
			if !ok {
				continue
			}
			values := make([]float64, 0)
			valid := true
			for _, part := range strings.Split(text, "/") {
				value, err := parseFloat(part)
				if err != nil || value < 0 {
					valid = false
					break
				}
				values = append(values, value/100)
			}
			if !valid || len(values) == 0 {
				continue
			}
			total := 0.0
			for _, value := range values {
				total += value
			}
			if math.Abs(100-total*100) > 0.01+1e-9 {
				continue
			}
			action.Split = values
		default:
			continue
		}
		if raw, exists := rule["environments"]; exists {
			environments, ok := raw.([]interface{})
			if !ok {
				continue
			}
			if len(environments) > 0 {
				matched := false
				for _, item := range environments {
					if item == environment {
						matched = true
					}
				}
				if !matched {
					continue
				}
			}
		}
		if raw, exists := rule["conditions"]; exists {
			conditions, ok := raw.(map[string]interface{})
			if !ok || !jsonexpr.EvaluateBooleanExpr(conditions, attributes) {
				continue
			}
		}
		return &action
	}
	return nil
}
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
