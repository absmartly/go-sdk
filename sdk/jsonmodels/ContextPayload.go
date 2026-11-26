package jsonmodels

// ExperimentData is an experiment of the context payload with the holdout
// and switchback fields, which Experiment does not carry: Experiment keeps
// its v1.2.0 layout, so positional literals of it keep compiling. It encodes
// to and decodes from the same JSON object as the collector sends.
type ExperimentData struct {
	Experiment
	// HoldoutIds are the ids of the ContextPayload.Holdouts covering this
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

// ContextPayload is the full context payload: ContextData plus the
// top-level holdouts and each experiment's holdout and switchback fields.
// ContextData keeps its v1.2.0 layout; pass a ContextPayload wherever an SDK
// entry point accepts one to serve holdouts and switchbacks.
type ContextPayload struct {
	Experiments []ExperimentData `json:"experiments"`
	// Holdouts are the holdouts the experiments' HoldoutIds refer to.
	Holdouts []Holdout `json:"holdouts,omitempty"`
}

// ContextData is the payload without the holdout and switchback fields.
func (p ContextPayload) ContextData() ContextData {
	var data ContextData
	if p.Experiments != nil {
		data.Experiments = make([]Experiment, len(p.Experiments))
		for i := range p.Experiments {
			data.Experiments[i] = p.Experiments[i].Experiment
		}
	}
	return data
}

// PayloadOf is data as a ContextPayload with no holdouts or switchbacks.
func PayloadOf(data ContextData) ContextPayload {
	var payload ContextPayload
	if data.Experiments != nil {
		payload.Experiments = make([]ExperimentData, len(data.Experiments))
		for i := range data.Experiments {
			payload.Experiments[i].Experiment = data.Experiments[i]
		}
	}
	return payload
}
