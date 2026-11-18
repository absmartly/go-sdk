package sdk

import (
	"bytes"
	"encoding/json"

	"github.com/absmartly/go-sdk/sdk/internal/v111"
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
// for an experiment's assignmentRules, so a malformed value drops that
// experiment's rules instead of failing the whole payload. Keeping this here,
// rather than in an UnmarshalJSON method on the public model, leaves the
// encoding/json behaviour of jsonmodels.Experiment, and of customer structs
// that embed it, unchanged.
type wireContextData struct {
	Experiments []wireExperiment `json:"experiments"`
}

type wireExperiment struct {
	jsonmodels.Experiment
	AssignmentRules json.RawMessage `json:"assignmentRules"`
}

func (w wireContextData) contextData() jsonmodels.ContextData {
	var data jsonmodels.ContextData
	if w.Experiments != nil {
		data.Experiments = make([]jsonmodels.Experiment, len(w.Experiments))
		for i, experiment := range w.Experiments {
			data.Experiments[i] = experiment.Experiment
			data.Experiments[i].AssignmentRules = assignmentRulesText(experiment.AssignmentRules)
		}
	}
	return data
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
