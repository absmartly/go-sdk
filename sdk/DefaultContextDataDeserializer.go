package sdk

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"

	v111 "github.com/absmartly/go-sdk/sdk/internal/v111"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type DefaultContextDataDeserializer struct {
	ContextDataDeserializer
}

func (d DefaultContextDataDeserializer) Deserialize(bytes []byte) (jsonmodels.ContextData, error) {
	var wire wireContextData
	if json.Unmarshal(bytes, &wire) == nil {
		return wire.contextData(), nil
	}
	// The payload is invalid somewhere other than assignmentRules. Decode the
	// caller's bytes into v1.1.1's model, which has no assignmentRules field,
	// so the partial data and the error (text and offset) are v1.1.1's.
	var legacy v111.ContextData
	var err = json.Unmarshal(bytes, &legacy)
	return legacy.Public(), err
}

// wireContextData is the context payload as the default deserializer decodes
// it. It differs from jsonmodels.ContextData only in accepting any JSON value
// for an experiment's assignmentRules, holdoutIds and the holdouts list, so a
// malformed value drops that experiment's rules, that holdout id or that
// holdout instead of failing the whole payload. Keeping this here,
// rather than in an UnmarshalJSON method on the public model, leaves the
// encoding/json behaviour of jsonmodels.Experiment, and of customer structs
// that embed it, unchanged.
type wireContextData struct {
	Experiments []wireExperiment `json:"experiments"`
	Holdouts    json.RawMessage  `json:"holdouts"`
}

type wireExperiment struct {
	jsonmodels.Experiment
	AssignmentRules json.RawMessage `json:"assignmentRules"`
	HoldoutIds      json.RawMessage `json:"holdoutIds"`
}

func (w wireContextData) contextData() jsonmodels.ContextData {
	var data jsonmodels.ContextData
	if w.Experiments != nil {
		data.Experiments = make([]jsonmodels.Experiment, len(w.Experiments))
		for i, experiment := range w.Experiments {
			data.Experiments[i] = experiment.Experiment
			data.Experiments[i].AssignmentRules = assignmentRulesText(experiment.AssignmentRules)
			data.Experiments[i].HoldoutIds = holdoutIds(experiment.HoldoutIds)
		}
	}
	data.Holdouts = holdouts(w.Holdouts)
	return data
}

// holdoutIds decodes an experiment's holdoutIds, skipping entries that are
// not integral numbers (2.0 and 1e1 are integers, as in JavaScript). Anything
// but an array gives none.
func holdoutIds(raw json.RawMessage) []int {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil
	}
	var ids = make([]int, 0, len(entries))
	for _, entry := range entries {
		if id, ok := holdoutId(entry); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// holdoutNumber rejects quoted numbers, which json.Number otherwise accepts.
func holdoutNumber(raw json.RawMessage) (float64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' {
		return 0, strconv.ErrSyntax
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, err
	}
	return number.Float64()
}

// holdoutId decodes an integral JSON number that fits an int. It goes
// through float64, as JavaScript's JSON.parse does, so 10, 10.0 and 1e1 are
// the same id, and any two spellings of one number decode alike (above 2^53
// they round together, as in JavaScript).
func holdoutId(raw json.RawMessage) (int, bool) {
	var value, err = holdoutNumber(raw)
	if err != nil || value != math.Trunc(value) || value < -math.Exp2(float64(strconv.IntSize-1)) || value >= math.Exp2(float64(strconv.IntSize-1)) {
		return 0, false
	}
	return int(value), true
}

// holdoutSeed decodes numeric JSON seeds as the JS SDK's DataView.setUint32:
// truncated and taken modulo 2^32. Non-numeric values default to 0 rather than
// applying JavaScript's broader coercions; non-finite numbers also give 0.
func holdoutSeed(raw json.RawMessage) int {
	var value, err = holdoutNumber(raw)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return 0
	}
	return int(uint32(int64(math.Mod(math.Trunc(value), 1<<32))))
}

// holdoutSplit decodes a holdout's split. Absent or null is no split: the
// holdout is kept, as in the JS SDK, and covers nothing. A null element is
// kept as 0, which is what JavaScript's arithmetic makes of it. Any other
// element that is not a number (a string, boolean, array or object) makes the
// entry malformed, and it is skipped; the JS SDK would keep it and coerce the
// element, a divergence the CHANGELOG documents.
func holdoutSplit(raw json.RawMessage) ([]float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, true
	}
	// encoding/json decodes a null element as 0 and rejects any other
	// non-number element or an out-of-range number.
	var split []float64
	if json.Unmarshal(raw, &split) != nil {
		return nil, false
	}
	return split, true
}

// wireHoldout decodes a holdout with its integer fields written as any
// integral JSON number, so a producer re-encoding 1 as 1.0 keeps the same
// epoch and seeds. holdoutType and unitType are informational (the arity
// comes from the split, the arm from the covered experiment's unit), so a
// value of the wrong type drops only that field rather than the holdout, as
// an unknown field would.
type wireHoldout struct {
	jsonmodels.Holdout
	Split       json.RawMessage `json:"split"`
	Id          json.RawMessage `json:"id"`
	Iteration   json.RawMessage `json:"iteration"`
	SeedHi      json.RawMessage `json:"seedHi"`
	SeedLo      json.RawMessage `json:"seedLo"`
	HoldoutType json.RawMessage `json:"holdoutType"`
	UnitType    json.RawMessage `json:"unitType"`
}

// holdouts decodes the payload's holdouts, skipping entries that are not
// holdout objects. Anything but an array gives none.
func holdouts(raw json.RawMessage) []jsonmodels.Holdout {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil
	}
	var result = make([]jsonmodels.Holdout, 0, len(entries))
	for _, entry := range entries {
		var holdout wireHoldout
		if trimmed := bytes.TrimSpace(entry); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(entry, &holdout) != nil {
			continue
		}
		var id, validID = holdoutId(holdout.Id)
		if !validID {
			continue
		}
		var split, ok = holdoutSplit(holdout.Split)
		if !ok {
			continue
		}
		holdout.Holdout.Split = split
		holdout.Holdout.Id = id
		holdout.Holdout.Iteration, _ = holdoutId(holdout.Iteration)
		holdout.Holdout.SeedHi = holdoutSeed(holdout.SeedHi)
		holdout.Holdout.SeedLo = holdoutSeed(holdout.SeedLo)
		_ = json.Unmarshal(holdout.HoldoutType, &holdout.Holdout.HoldoutType)
		_ = json.Unmarshal(holdout.UnitType, &holdout.Holdout.UnitType)
		result = append(result, holdout.Holdout)
	}
	return result
}

// assignmentRulesText returns the assignmentRules string, or "" (no rules)
// when the field is omitted, null, an object or an array. Other scalars keep
// their JSON text, as the Java SDK coerces them; they are not a rules document,
// so they give no rules either.
func assignmentRulesText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] == '{' || raw[0] == '[':
		return ""
	case raw[0] == '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return ""
		}
		return text
	default:
		return string(raw)
	}
}
