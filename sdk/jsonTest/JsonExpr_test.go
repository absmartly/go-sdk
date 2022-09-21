package jsonTest

import (
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"testing"
)

var john = map[string]interface{}{"age": 20, "language": "en-US", "returning": false}

var terry = map[string]interface{}{"age": 20, "language": "en-GB", "returning": true}

var kate = map[string]interface{}{"age": 50, "language": "es-ES", "returning": false}

var maria = map[string]interface{}{"age": 52, "language": "pt-PT", "returning": true}

var ageTwentyAndUs = []interface{}{
	map[string]interface{}{"eq": []interface{}{
		map[string]interface{}{"var": map[string]interface{}{"path": "age"}},
		map[string]interface{}{"value": 20},
	}},
	map[string]interface{}{"eq": []interface{}{
		map[string]interface{}{"var": map[string]interface{}{"path": "language"}},
		map[string]interface{}{"value": "en-US"},
	}},
}

var ageOverFifty = []interface{}{
	map[string]interface{}{"gte": []interface{}{
		map[string]interface{}{"var": map[string]interface{}{"path": "age"}},
		map[string]interface{}{"value": 50},
	}},
}

var langEn = []interface{}{
	map[string]interface{}{"in": []interface{}{
		map[string]interface{}{"var": map[string]interface{}{"path": "language"}},
		map[string]interface{}{"value": "en"},
	}},
}

var ageTwentyAndUSOrAgeOverFifty = []interface{}{
	map[string]interface{}{"or": []interface{}{
		ageTwentyAndUs,
		ageOverFifty,
	}},
}

var returning = []interface{}{
	map[string]interface{}{"var": map[string]interface{}{"path": "returning"}},
}

var returningAndAgeTwentyAndUSOrAgeOverFifty = []interface{}{
	ageTwentyAndUSOrAgeOverFifty,
	returning,
}

var notReturningAndSpanish = []interface{}{
	map[string]interface{}{"not": returning},
	map[string]interface{}{"eq": []interface{}{
		map[string]interface{}{"var": map[string]interface{}{"path": "language"}},
		map[string]interface{}{"value": "es-ES"},
	}},
}

func TestAgeTwentyAsUSEnglish(t *testing.T) {
	assert(true, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUs, john), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUs, terry), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUs, kate), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUs, maria), t)
}

func TestAgeOverFifty(t *testing.T) {
	assert(false, jsonexpr.EvaluateBooleanExpr(ageOverFifty, john), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(ageOverFifty, terry), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(ageOverFifty, kate), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(ageOverFifty, maria), t)
}

func TestAgeTwentyAndUS_Or_AgeOverFifty(t *testing.T) {
	assert(true, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUSOrAgeOverFifty, john), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUSOrAgeOverFifty, terry), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUSOrAgeOverFifty, kate), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(ageTwentyAndUSOrAgeOverFifty, maria), t)
}

func TestReturning(t *testing.T) {
	assert(false, jsonexpr.EvaluateBooleanExpr(returning, john), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(returning, terry), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(returning, kate), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(returning, maria), t)
}

func TestReturning_And_AgeTwentyAndUS_Or_AgeOverFifty(t *testing.T) {
	assert(false, jsonexpr.EvaluateBooleanExpr(returningAndAgeTwentyAndUSOrAgeOverFifty, john), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(returningAndAgeTwentyAndUSOrAgeOverFifty, terry), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(returningAndAgeTwentyAndUSOrAgeOverFifty, kate), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(returningAndAgeTwentyAndUSOrAgeOverFifty, maria), t)
}

func TestNotReturning_And_Spanish(t *testing.T) {
	assert(false, jsonexpr.EvaluateBooleanExpr(notReturningAndSpanish, john), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(notReturningAndSpanish, terry), t)
	assert(true, jsonexpr.EvaluateBooleanExpr(notReturningAndSpanish, kate), t)
	assert(false, jsonexpr.EvaluateBooleanExpr(notReturningAndSpanish, maria), t)
}

func TestLangEn(t *testing.T) {
	assert(true, jsonexpr.EvaluateBooleanExpr(langEn, john), t)
	//assert(true, jsonexpr.EvaluateBooleanExpr(langEn, terry), t)
	//assert(false, jsonexpr.EvaluateBooleanExpr(langEn, kate), t)
	//assert(false, jsonexpr.EvaluateBooleanExpr(langEn, maria), t)
}
