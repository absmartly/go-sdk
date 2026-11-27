package jsonmodels

// Holdout is an entry of the context payload's top-level holdouts list. An
// experiment is covered by the holdouts its HoldoutIds name. The unit's arm
// is drawn from Split with SeedHi/SeedLo; arm 0 holds the covered experiment
// out, and in a three-arm holdout arm 1 holds out experiments that are not
// full-on. The arm count comes from Split, never from HoldoutType.
type Holdout struct {
	Id          int       `json:"id"`
	Name        string    `json:"name"`
	UnitType    string    `json:"unitType"`
	Iteration   int       `json:"iteration"`
	SeedHi      int       `json:"seedHi"`
	SeedLo      int       `json:"seedLo"`
	Split       []float64 `json:"split"`
	HoldoutType string    `json:"holdoutType,omitempty"`
}
