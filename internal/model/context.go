package model

type Context struct {
	Experiments []Experiment `json:"experiments"`
}

type Experiment struct {
	Id             int                     `json:"id"`
	Name           string                  `json:"name"`
	UnitType       string                  `json:"unitType"`
	Iteration      int                     `json:"iteration"`
	SeedHi         int32                   `json:"seedHi"`
	SeedLo         int32                   `json:"seedLo"`
	Split          []float64               `json:"split"`
	TrafficSeedHi  int                     `json:"trafficSeedHi"`
	TrafficSeedLo  int                     `json:"trafficSeedLo"`
	TrafficSplit   []float64               `json:"trafficSplit"`
	FullOnVariant  int                     `json:"fullOnVariant"`
	Applications   []ExperimentApplication `json:"applications"`
	Variants       []ExperimentVariant     `json:"variants"`
	AudienceStrict bool                    `json:"audienceStrict"`
	Audience       string                  `json:"audience"`
}

type ExperimentApplication struct {
	Name string `json:"name"`
}

type ExperimentVariant struct {
	Name   string `json:"name"`
	Config string `json:"config"`
}
