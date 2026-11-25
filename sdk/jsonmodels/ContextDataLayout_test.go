package jsonmodels

import (
	"reflect"
	"testing"
)

// v1.3.0 adds one field to ContextData, Holdouts, after v1.2.0's only field
// (CHANGELOG, v1.3.0 BREAKING). Any other change to the layout would break
// more callers than the CHANGELOG lists.
func TestContextDataLayoutAddsOnlyHoldouts(t *testing.T) {
	var fields []string
	var typ = reflect.TypeOf(ContextData{})
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name+" "+typ.Field(i).Type.String())
	}
	var want = []string{"Experiments []jsonmodels.Experiment", "Holdouts []jsonmodels.Holdout"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("ContextData fields %v, want %v", fields, want)
	}
	var _ = ContextData{Experiments: nil}
}
