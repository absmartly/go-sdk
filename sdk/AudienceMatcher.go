package sdk

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/absmartly/go-sdk/sdk/jsonexpr"
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
	result, err := am.deserializer().Deserialize([]byte(audience), 0, len(audience))
	if err != nil {
		return Result{}, errors.New("can't evaluate data")
	}

	var filter = result["filter"]
	var kind = reflect.ValueOf(filter).Kind()
	if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array {
		return CreateResult(jsonexpr.EvaluateBooleanExpr(filter, attributes)), nil
	}

	return Result{}, errors.New("can't evaluate data")
}

func (am AudienceMatcher) deserializer() AudienceDeserializer {
	if am.Deserializer_ == nil {
		return DefaultAudienceDeserializer{}
	}
	return am.Deserializer_
}

// splitPercentagesSumTolerance is how far from 100 a split rule's percentages
// may sum, after rounding the gap to two decimals. The backend applies the
// same check to an experiment's own split and to split rules.
const splitPercentagesSumTolerance = 0.01

// ruleAction is what a matching assignment rule does: assign a fixed variant,
// or, when split is set, divide units across the variants by those fractions.
type ruleAction struct {
	variant int
	split   []float64
}

// assignmentRules holds an experiment's parsed assignmentRules JSON. It is
// parsed once, when context data is installed.
type assignmentRules struct {
	rules []interface{}
}

// parseRules parses an assignmentRules JSON string ({"rules": [...]}). It
// returns nil without an error when there are no rules. A document whose
// "rules" is not a list has no rules, like the JS SDK. A custom
// AudienceDeserializer that panics is contained and reported as an error, so
// only that experiment's rules are dropped.
func (am AudienceMatcher) parseRules(text string) (parsed *assignmentRules, err error) {
	if len(text) == 0 {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			parsed, err = nil, fmt.Errorf("%v", r)
		}
	}()
	document, err := am.deserializer().Deserialize([]byte(text), 0, len(text))
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("assignmentRules is not a JSON object")
	}
	if rules, ok := document["rules"].([]interface{}); ok {
		return &assignmentRules{rules: rules}, nil
	}
	return nil, nil
}

// evaluateRules returns the action of the first rule that applies, or nil.
// A rule is skipped when it is not an object, its type is unknown, an
// "assign" rule's variant is not an integer, a "split" rule's percentages are
// invalid, its environments are present but not a list, its non-empty
// environments list does not contain environment (an empty environment never
// matches a non-empty list), its conditions are not an object, or its
// conditions do not evaluate to true. A rule without conditions always
// matches, and an empty environments list matches every environment. A
// condition that panics is contained and its rule skipped. Checking the
// action against the experiment's variants is the caller's job.
func (am AudienceMatcher) evaluateRules(parsed *assignmentRules, environment string, attributes map[string]interface{}) *ruleAction {
	if parsed == nil {
		return nil
	}
	for _, item := range parsed.rules {
		rule, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		var action = parseRuleAction(rule)
		if action == nil {
			continue
		}

		if raw, found := rule["environments"]; found && raw != nil {
			environments, ok := raw.([]interface{})
			if !ok {
				continue
			}
			if len(environments) > 0 && (len(environment) == 0 || !containsString(environments, environment)) {
				continue
			}
		}

		var conditions, found = rule["conditions"]
		if !found || conditions == nil {
			return action
		}
		if _, ok := conditions.(map[string]interface{}); !ok {
			continue
		}
		if evaluateCondition(conditions, attributes) {
			return action
		}
	}
	return nil
}

func evaluateCondition(conditions interface{}, attributes map[string]interface{}) (result bool) {
	defer func() {
		if recover() != nil {
			result = false
		}
	}()
	return jsonexpr.EvaluateBooleanExpr(conditions, attributes)
}

func containsString(values []interface{}, value string) bool {
	for _, v := range values {
		if s, ok := v.(string); ok && s == value {
			return true
		}
	}
	return false
}

func parseRuleAction(rule map[string]interface{}) *ruleAction {
	switch rule["type"] {
	case "assign":
		if variant, ok := ruleVariant(rule["variant"]); ok {
			return &ruleAction{variant: variant}
		}
	case "split":
		if split := parseSplitPercentages(rule["percentages"]); split != nil {
			return &ruleAction{split: split}
		}
	}
	return nil
}

// ruleVariant accepts integral numbers within int32 range, like JavaScript's
// Number.isInteger (2.0 is an integer).
func ruleVariant(value interface{}) (int, bool) {
	var v = reflect.ValueOf(value)
	var f float64
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		f = v.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f = float64(v.Uint())
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f > math.MaxInt32 || f < math.MinInt32 {
		return 0, false
	}
	return int(f), true
}

// parseSplitPercentages parses a split rule's percentages, such as "20/30/50",
// into fractions of 1. Each value is parsed like JavaScript's parseFloat and
// divided by 100 with no normalisation. It returns nil when percentages is not
// a string, a value is not a number or is negative, or the values do not sum
// to 100 within splitPercentagesSumTolerance once the gap is rounded to two
// decimals.
func parseSplitPercentages(percentages interface{}) []float64 {
	text, ok := percentages.(string)
	if !ok {
		return nil
	}

	var parts = strings.Split(text, "/")
	var values = make([]float64, len(parts))
	var total = 0.0
	for i, part := range parts {
		var value = jsParseFloat(part)
		if math.IsNaN(value) || value < 0 {
			return nil
		}
		values[i] = value
		total += value
	}

	var gap = math.Abs(100 - total)
	if math.IsNaN(gap) || math.IsInf(gap, 0) {
		return nil
	}
	// FormatFloat rounds the exact binary value, as JavaScript's toFixed does.
	var rounded, _ = strconv.ParseFloat(strconv.FormatFloat(gap, 'f', 2, 64), 64)
	if rounded > splitPercentagesSumTolerance {
		return nil
	}

	for i := range values {
		values[i] = values[i] / 100
	}
	return values
}

// jsParseFloat follows JavaScript's parseFloat: leading whitespace is skipped
// and the longest decimal prefix is parsed, so "50abc" is 50 and "abc" is NaN.
func jsParseFloat(s string) float64 {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	var i = 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}

	var digits = false
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits = true
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits = true
		}
	}
	if !digits {
		return math.NaN()
	}

	var end = i
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		var j = i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		var expStart = j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > expStart {
			end = j
		}
	}

	var value, err = strconv.ParseFloat(s[:end], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return math.NaN()
	}
	return value
}
