package jsonmodels

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
	// HoldoutIds are the ids of the ContextData.Holdouts covering this
	// experiment. Ids with no matching holdout are ignored.
	HoldoutIds []int `json:"holdoutIds,omitempty"`
}
