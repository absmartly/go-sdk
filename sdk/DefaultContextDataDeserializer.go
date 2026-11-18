package sdk

import (
	"bytes"
	"encoding/json"

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
	// The payload is invalid somewhere other than assignmentRules. Decode it
	// as v1.1.1 did, which had no assignmentRules field, so the partial data
	// and the error text are the ones v1.1.1 returned.
	var data jsonmodels.ContextData
	var err = json.Unmarshal(withoutAssignmentRules(bytes), &data)
	return data, err
}

// withoutAssignmentRules returns the payload with every experiment's
// assignmentRules removed and every other key left in its order, so a decode
// reports the same first invalid field as v1.1.1. A payload that is not an
// object with an experiments array is returned as it is; its decode error is
// the same either way.
func withoutAssignmentRules(payload []byte) []byte {
	return rewriteObject(payload, func(key string, value json.RawMessage) (json.RawMessage, bool) {
		if key != "experiments" {
			return value, true
		}
		var experiments []json.RawMessage
		if json.Unmarshal(value, &experiments) != nil {
			return value, true
		}
		var out = []byte{'['}
		for i, experiment := range experiments {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, rewriteObject(experiment, func(key string, value json.RawMessage) (json.RawMessage, bool) {
				return value, key != "assignmentRules"
			})...)
		}
		return append(out, ']'), true
	})
}

// rewriteObject re-encodes a JSON object, keeping its keys in order and
// passing each value through keep, which returns the value to write and
// whether to write the key at all. Anything that is not a valid object is
// returned unchanged.
func rewriteObject(raw []byte, keep func(key string, value json.RawMessage) (json.RawMessage, bool)) []byte {
	var decoder = json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return raw
	}
	var out = []byte{'{'}
	var first = true
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return raw
		}
		var key, ok = token.(string)
		if !ok {
			return raw
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return raw
		}
		value, write := keep(key, value)
		if !write {
			continue
		}
		if !first {
			out = append(out, ',')
		}
		first = false
		encodedKey, _ := json.Marshal(key)
		out = append(out, encodedKey...)
		out = append(out, ':')
		out = append(out, value...)
	}
	return append(out, '}')
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
