package jsonmodels

import (
	"bytes"
	"encoding/json"
)

type Experiment struct {
	Id                int                     `json:"id"`
	Name              string                  `json:"name"`
	UnitType          string                  `json:"unitType"`
	Iteration         int                     `json:"iteration"`
	SeedHi            int                     `json:"seedHi"`
	SeedLo            int                     `json:"seedLo"`
	Split             []float64               `json:"split"`
	TrafficSeedHi     int                     `json:"trafficSeedHi"`
	TrafficSeedLo     int                     `json:"trafficSeedLo"`
	TrafficSplit      []float64               `json:"trafficSplit"`
	FullOnVariant     int                     `json:"fullOnVariant"`
	Applications      []ExperimentApplication `json:"applications"`
	Variants          []ExperimentVariant     `json:"variants"`
	CustomFieldValues []CustomFieldValue      `json:"customFieldValues"`
	AudienceStrict    bool                    `json:"audienceStrict"`
	Audience          string                  `json:"audience"`
	// AssignmentRules is the experiment's custom assignment rules, a JSON
	// string such as {"rules": [...]}, or empty when it has none. The first
	// matching rule sets the variant, ahead of the audience, traffic and
	// custom assignments; only an override beats it.
	AssignmentRules string `json:"assignmentRules,omitempty"`

	// assignmentRulesIgnored is set when the payload sent an object or array
	// in place of the assignmentRules string.
	assignmentRulesIgnored bool
}

// AssignmentRulesIgnored reports whether the payload sent an object or array
// where the assignmentRules string belongs. Only that experiment's rules are
// dropped; the rest of the context data still loads.
func (e Experiment) AssignmentRulesIgnored() bool {
	return e.assignmentRulesIgnored
}

func (e *Experiment) UnmarshalJSON(data []byte) error {
	type plain Experiment
	var decoded struct {
		plain
		AssignmentRules json.RawMessage `json:"assignmentRules"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = Experiment(decoded.plain)

	var raw = bytes.TrimSpace(decoded.AssignmentRules)
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
	case raw[0] == '"':
		if err := json.Unmarshal(raw, &e.AssignmentRules); err != nil {
			return err
		}
	case raw[0] == '{' || raw[0] == '[':
		e.assignmentRulesIgnored = true
	default:
		// Other scalars keep their JSON text, as the Java SDK coerces them.
		e.AssignmentRules = string(raw)
	}
	return nil
}
