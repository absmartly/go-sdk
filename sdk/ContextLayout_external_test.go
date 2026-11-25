package sdk_test

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
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

// v111ContextFields is v1.1.1's Context declaration: each field's name and
// type, in order. Checking types, not just names, catches a change such as
// PublishDelay_ int64 -> int that the untyped zeros above would not.
var v111ContextFields = []struct {
	name string
	typ  reflect.Type
}{
	{"PublishDelay_", reflect.TypeOf(int64(0))},
	{"RefreshInterval_", reflect.TypeOf(int64(0))},
	{"EventHandler_", reflect.TypeOf((*sdk.ContextEventHandler)(nil)).Elem()},
	{"EventLogger_", reflect.TypeOf((*sdk.ContextEventLogger)(nil)).Elem()},
	{"DataProvider_", reflect.TypeOf((*sdk.ContextDataProvider)(nil)).Elem()},
	{"VariableParser_", reflect.TypeOf((*sdk.VariableParser)(nil)).Elem()},
	{"AudienceMatcher_", reflect.TypeOf(sdk.AudienceMatcher{})},
	{"Units_", reflect.TypeOf(map[string]string{})},
	{"Failed_", reflect.TypeOf(&atomic.Value{})},
	{"Ready_", reflect.TypeOf(&atomic.Value{})},
	{"DataLock", reflect.TypeOf(&sync.RWMutex{})},
	{"Data_", reflect.TypeOf(jsonmodels.ContextData{})},
	{"Index_", reflect.TypeOf(map[string]sdk.ExperimentVariables{})},
	{"ContextCustomFields_", reflect.TypeOf(map[string]map[string]sdk.ContextCustomFieldValue{})},
	{"IndexVariables_", reflect.TypeOf(map[interface{}]interface{}{})},
	{"ContextLock_", reflect.TypeOf(&sync.RWMutex{})},
	{"HashedUnits_", reflect.TypeOf(map[interface{}]interface{}{})},
	{"Assigners_", reflect.TypeOf(map[interface{}]interface{}{})},
	{"AssignmentCache", reflect.TypeOf(map[string]sdk.Assignment{})},
	{"EventLock_", reflect.TypeOf(&sync.Mutex{})},
	{"Exposures_", reflect.TypeOf([]jsonmodels.Exposure{})},
	{"Achievements_", reflect.TypeOf([]jsonmodels.GoalAchievement{})},
	{"Attributes_", reflect.TypeOf([]interface{}{})},
	{"Overrides_", reflect.TypeOf(map[interface{}]interface{}{})},
	{"Cassignments_", reflect.TypeOf(map[interface{}]interface{}{})},
	{"PendingCount_", reflect.TypeOf(&atomic.Value{})},
	{"Closing_", reflect.TypeOf(&atomic.Value{})},
	{"Closed_", reflect.TypeOf(&atomic.Value{})},
	{"Refreshing_", reflect.TypeOf(&atomic.Value{})},
	{"ReadyFuture_", reflect.TypeOf(&future.Future{})},
	{"ClosingFuture_", reflect.TypeOf(&future.Future{})},
	{"RefreshFuture_", reflect.TypeOf(&future.Future{})},
	{"TimeoutLock_", reflect.TypeOf(&sync.Mutex{})},
	{"Timeout_", reflect.TypeOf(&time.Timer{})},
	{"RefreshTimer_", reflect.TypeOf(&time.Timer{})},
	// Clock_ is an internal interface type, so it is checked by name.
	{"Clock_", nil},
}

func TestContextV111Layout(t *testing.T) {
	var typ = reflect.TypeOf(positionalContextV111)
	if typ.NumField() != len(v111ContextFields) {
		t.Fatalf("Context has %d fields, want %d", typ.NumField(), len(v111ContextFields))
	}
	for i, want := range v111ContextFields {
		var field = typ.Field(i)
		if field.Name != want.name || field.PkgPath != "" {
			t.Errorf("field %d = %s, want exported %s", i, field.Name, want.name)
			continue
		}
		if want.typ == nil {
			if field.Type.Kind() != reflect.Interface || field.Type.Name() != "Clock" {
				t.Errorf("field %s has type %v, want the internal Clock interface", want.name, field.Type)
			}
		} else if field.Type != want.typ {
			t.Errorf("field %s has type %v, want %v", want.name, field.Type, want.typ)
		}
	}
}
