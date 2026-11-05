package jsonTest

import (
	"fmt"
	"github.com/absmartly/go-sdk/sdk/jsonexpr"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{"value": false}}

	var binary = operators.BinaryOperator{
		BinaryOp: operators.MatchOperator{},
	}

	assert(true, binary.BinaryOp.Binary(eval, 0, 0), t)
	assert(false, binary.BinaryOp.Binary(eval, 0, 1), t)
	assert(false, binary.BinaryOp.Binary(eval, 1, 0), t)

	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", ""), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "abc"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "ijk"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "^abc"), t)

	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "ijk$"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "def"), t)
	assert(true, binary.BinaryOp.Binary(eval, "abcdefghijk", "b.*j"), t)
	assert(false, binary.BinaryOp.Binary(eval, "abcdefghijk", "xyz"), t)

	assert(nil, binary.BinaryOp.Binary(eval, nil, "abc"), t)
	assert(nil, binary.BinaryOp.Binary(eval, "abcdefghijk", nil), t)
}

func TestMatchLongPattern(t *testing.T) {
	var eval = eval.Evaluator{Operators: jsonexpr.Operators, Vars: map[string]interface{}{}}
	var binary = operators.BinaryOperator{BinaryOp: operators.MatchOperator{}}

	// Long allow-list patterns must keep matching; RE2 is linear-time so
	// there is no pattern length cap.
	var alts = make([]string, 0, 500)
	for i := 0; i < 500; i++ {
		alts = append(alts, fmt.Sprintf("user-%04d", i))
	}
	var pattern = "^(" + strings.Join(alts, "|") + ")$"
	if len(pattern) <= 1000 {
		t.Fatalf("pattern too short to exercise long patterns: %d", len(pattern))
	}
	assert(true, binary.BinaryOp.Binary(eval, "user-0499", pattern), t)
	assert(false, binary.BinaryOp.Binary(eval, "user-0500", pattern), t)
}
