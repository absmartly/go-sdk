package jsonmodels

type ContextData struct {
	Experiments []Experiment `json:"experiments"`
	// Holdouts are the holdouts the experiments' HoldoutIds refer to.
	Holdouts []Holdout `json:"holdouts,omitempty"`
}
