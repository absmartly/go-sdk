package jsonmodels

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
