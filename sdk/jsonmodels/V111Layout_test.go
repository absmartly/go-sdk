package jsonmodels

// Unkeyed literals of the models whose v1.1.1 layout v1.2.0 keeps, written
// as a v1.1.1 caller would (see sdk/V111Consumer_test.go). They live in this
// package so plain `go vet ./...` accepts them; a field added, removed or
// reordered still fails to compile here exactly as it would for a caller.
// Experiment, Exposure and ContextData gained wire fields (assignment rules
// and holdouts), so they are not listed; see the CHANGELOG.
var (
	_ = PublishEvent{true, nil, 0, nil, nil, nil}
	_ = Unit{"session_id", "uid"}
	_ = GoalAchievement{"goal", 0, nil}
	_ = Attribute{"name", "value", 0}
	_ = CustomFieldValue{"name", "string", "value"}
	_ = ExperimentVariant{"A", "{}"}
	_ = ExperimentApplication{"website"}
)
