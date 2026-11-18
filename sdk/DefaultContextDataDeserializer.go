package sdk

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type DefaultContextDataDeserializer struct {
	ContextDataDeserializer
}

func (d DefaultContextDataDeserializer) Deserialize(bytes []byte) (jsonmodels.ContextData, error) {
	var wire wireContextData
	if err := json.Unmarshal(bytes, &wire); err != nil {
		return wire.contextData(), err
	}
	return wire.contextData(), nil
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

// holdoutId decodes an integral JSON number that fits a holdout id.
func holdoutId(raw json.RawMessage) (int, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' {
		// json.Number also accepts a quoted number; an id must be a number.
		return 0, false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil || len(number) == 0 {
		return 0, false
	}
	if id, err := number.Int64(); err == nil {
		return int(id), id == int64(int(id))
	}
	var value, err = number.Float64()
	if err != nil || value != math.Trunc(value) || math.Abs(value) > 1<<53 {
		return 0, false
	}
	return int(value), true
}

// wireHoldout decodes a holdout with an id written as any integral number.
type wireHoldout struct {
	jsonmodels.Holdout
	Id json.RawMessage `json:"id"`
	// holdoutType and unitType are informational (the arity comes from the
	// split, the arm from the covered experiment's unit), and iteration may
	// be absent, so a value of the wrong type drops only that field rather
	// than the holdout, as an unknown field would.
	HoldoutType json.RawMessage `json:"holdoutType"`
	UnitType    json.RawMessage `json:"unitType"`
	Iteration   json.RawMessage `json:"iteration"`
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
		if id, ok := holdoutId(holdout.Id); ok {
			holdout.Holdout.Id = id
			_ = json.Unmarshal(holdout.HoldoutType, &holdout.Holdout.HoldoutType)
			_ = json.Unmarshal(holdout.UnitType, &holdout.Holdout.UnitType)
			_ = json.Unmarshal(holdout.Iteration, &holdout.Holdout.Iteration)
			result = append(result, holdout.Holdout)
		}
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
