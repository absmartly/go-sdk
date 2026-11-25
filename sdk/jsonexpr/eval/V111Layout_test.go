package eval

// An unkeyed Evaluator literal, as a v1.1.1 caller would write it (see
// sdk/V111Consumer_test.go); here so plain `go vet ./...` accepts it.
var _ = Evaluator{nil, nil}
