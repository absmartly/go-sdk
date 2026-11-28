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
	// RandomizationType is "switchback" for a switchback experiment; empty
	// or "unit" means a unit experiment, and the Switchback fields are
	// then ignored.
	RandomizationType string `json:"randomizationType,omitempty"`
	// SwitchbackClusters maps each linked unit type to the attribute path
	// holding its cluster value, in the served order. The order decides
	// which unit serves, so it is kept as decoded.
	SwitchbackClusters SwitchbackClusters `json:"switchbackClusters,omitempty"`
	// SwitchbackClusterValues is the allow-list of cluster values; nil or
	// empty allows every value.
	SwitchbackClusterValues []string `json:"switchbackClusterValues,omitempty"`
	// SwitchbackStartAt is the grid anchor and SwitchbackPeriodMs the period
	// length, in epoch milliseconds; nil when not set.
	SwitchbackStartAt  *int64 `json:"switchbackStartAt,omitempty"`
	SwitchbackPeriodMs *int64 `json:"switchbackPeriodMs,omitempty"`
	// The carryover and burn windows and the estimand are analysis-only;
	// the SDK carries them but never reads them.
	SwitchbackCarryoverDurationMs *int64 `json:"switchbackCarryoverDurationMs,omitempty"`
	SwitchbackBurnInDurationMs    *int64 `json:"switchbackBurnInDurationMs,omitempty"`
	SwitchbackBurnOutDurationMs   *int64 `json:"switchbackBurnOutDurationMs,omitempty"`
	SwitchbackDesignType          string `json:"switchbackDesignType,omitempty"`
	SwitchbackEstimand            string `json:"switchbackEstimand,omitempty"`
}
