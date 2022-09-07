package jsonmodels

type Experiment struct {
	Id             int                     `json:"id"`
	Name           string                  `json:"name"`
	UnitType       string                  `json:"unitType"`
	Iteration      int                     `json:"iteration"`
	SeedHi         int                     `json:"seedHi"`
	SeedLo         int                     `json:"seedLo"`
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
