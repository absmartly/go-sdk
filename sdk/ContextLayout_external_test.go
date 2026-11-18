package sdk_test

import (
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// This is intentionally in an external package: adding even one private field,
// reordering fields, or changing a field type breaks v1.1.1 positional literals.
// Keep all 36 values, including the nil Clock_ (whose type is internal).
var positionalContextV111 = sdk.Context{
	0, 0, nil, nil, nil, nil, sdk.AudienceMatcher{},
	map[string]string{}, nil, nil, nil, jsonmodels.ContextData{},
	map[string]sdk.ExperimentVariables{},
	map[string]map[string]sdk.ContextCustomFieldValue{},
	map[interface{}]interface{}{}, nil,
	map[interface{}]interface{}{}, map[interface{}]interface{}{},
	map[string]sdk.Assignment{}, nil,
	[]jsonmodels.Exposure{}, []jsonmodels.GoalAchievement{}, []interface{}{},
	map[interface{}]interface{}{}, map[interface{}]interface{}{},
	nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
}

func TestContextV111Layout(t *testing.T) {
	var typ = reflect.TypeOf(positionalContextV111)
	var names = []string{
		"PublishDelay_", "RefreshInterval_", "EventHandler_", "EventLogger_",
		"DataProvider_", "VariableParser_", "AudienceMatcher_", "Units_",
		"Failed_", "Ready_", "DataLock", "Data_", "Index_", "ContextCustomFields_",
		"IndexVariables_", "ContextLock_", "HashedUnits_", "Assigners_",
		"AssignmentCache", "EventLock_", "Exposures_", "Achievements_", "Attributes_",
		"Overrides_", "Cassignments_", "PendingCount_", "Closing_", "Closed_",
		"Refreshing_", "ReadyFuture_", "ClosingFuture_", "RefreshFuture_",
		"TimeoutLock_", "Timeout_", "RefreshTimer_", "Clock_",
	}
	if typ.NumField() != len(names) {
		t.Fatalf("Context has %d fields, want %d", typ.NumField(), len(names))
	}
	for i, name := range names {
		if field := typ.Field(i); field.Name != name || field.PkgPath != "" {
			t.Errorf("field %d = %+v, want exported %s", i, field, name)
		}
	}
}
