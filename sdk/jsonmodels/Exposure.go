package jsonmodels

import "encoding/json"

type Exposure struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Unit             string `json:"unit"`
	Variant          int    `json:"variant"`
	ExposedAt        int64  `json:"exposedAt"`
	Assigned         bool   `json:"assigned"`
	Eligible         bool   `json:"eligible"`
	Overridden       bool   `json:"overridden"`
	FullOn           bool   `json:"fullOn"`
	Custom           bool   `json:"custom"`
	AudienceMismatch bool   `json:"audienceMismatch"`
}

// MarshalJSON writes "unit": null when the experiment is unknown and so has
// no unit type, like the other ABsmartly SDKs.
func (e Exposure) MarshalJSON() ([]byte, error) {
	var unit *string
	if e.Unit != "" {
		unit = &e.Unit
	}
	return json.Marshal(struct {
		Id               int     `json:"id"`
		Name             string  `json:"name"`
		Unit             *string `json:"unit"`
		Variant          int     `json:"variant"`
		ExposedAt        int64   `json:"exposedAt"`
		Assigned         bool    `json:"assigned"`
		Eligible         bool    `json:"eligible"`
		Overridden       bool    `json:"overridden"`
		FullOn           bool    `json:"fullOn"`
		Custom           bool    `json:"custom"`
		AudienceMismatch bool    `json:"audienceMismatch"`
	}{e.Id, e.Name, unit, e.Variant, e.ExposedAt, e.Assigned, e.Eligible, e.Overridden, e.FullOn, e.Custom, e.AudienceMismatch})
}
