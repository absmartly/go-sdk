package sdk

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

func TestDefaultVariableParserParseCustomFieldValueAcceptsAnyJSONValue(t *testing.T) {
	var parser = DefaultVariableParser{}
	var cases = []struct {
		name  string
		input string
		want  interface{}
	}{
		{"object", `{"123":1,"456":0}`, map[string]interface{}{"123": 1.0, "456": 0.0}},
		{"array", `["hello", "world"]`, []interface{}{"hello", "world"}},
		{"number", `123`, 123.0},
		{"string", `"hello"`, "hello"},
		{"boolean", `true`, true},
		{"null", `null`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parser.ParseCustomFieldValue(Context{}, "exp", "field", tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDefaultVariableParserParseCustomFieldValueRejectsInvalidJSON(t *testing.T) {
	got, err := DefaultVariableParser{}.ParseCustomFieldValue(Context{}, "exp", "field", "invalid")
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	if got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestDefaultVariableParserParseStillRequiresObjectForVariantConfig(t *testing.T) {
	var logger = &MockEventLogger{}
	var ctx = Context{EventLogger_: logger}

	assertAny(map[string]interface{}{"button.color": "red"},
		DefaultVariableParser{}.Parse(ctx, "exp", "B", `{"button.color":"red"}`), t)

	if got := (DefaultVariableParser{}).Parse(ctx, "exp", "B", `["red"]`); got != nil {
		t.Errorf("non-object variant config: got %#v, want nil", got)
	}
	if len(logger.GetEvents()) != 1 || logger.GetEvents()[0].EventType != Error {
		t.Errorf("expected one Error event for a non-object variant config, got %#v", logger.GetEvents())
	}
}

func jsonCustomFieldContextData() jsonmodels.ContextData {
	return jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{
		Id:           2,
		Name:         "exp_test_abc",
		UnitType:     "session_id",
		Iteration:    1,
		SeedHi:       55006150,
		SeedLo:       47189152,
		Split:        []float64{0.34, 0.33, 0.33},
		TrafficSplit: []float64{0, 1},
		Variants: []jsonmodels.ExperimentVariant{
			{Name: "A"}, {Name: "B", Config: `{"button.color":"blue"}`}, {Name: "C", Config: `{"button.color":"red"}`},
		},
		CustomFieldValues: []jsonmodels.CustomFieldValue{
			{Name: "country", Type: "string", Value: "US,PT,ES,DE,FR"},
			{Name: "json_object", Type: "json", Value: `{"123":1,"456":0}`},
			{Name: "json_array", Type: "json", Value: `["hello", "world"]`},
			{Name: "json_number", Type: "json", Value: `123`},
			{Name: "json_string", Type: "json", Value: `"hello"`},
			{Name: "json_boolean", Type: "json", Value: `true`},
			{Name: "json_null", Type: "json", Value: `null`},
			{Name: "json_invalid", Type: "json", Value: `invalid`},
		},
	}}}
}

func createJSONCustomFieldContext(t *testing.T, parser VariableParser, logger *MockEventLogger) *Context {
	t.Helper()
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{"session_id": "test123"}
	config.EventLogger_ = logger
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0

	var dataFut, done = future.New()
	done(jsonCustomFieldContextData(), nil)

	var ctx = createTrackedContext(clock, config, dataFut, dataProvider, eventHandler, logger, parser, audienceMatcher)
	if !ctx.IsReady() || ctx.IsFailed() {
		t.Fatalf("context not ready: ready=%v failed=%v", ctx.IsReady(), ctx.IsFailed())
	}
	return ctx
}

func errorEvents(logger *MockEventLogger) []LoggedEvent {
	var result []LoggedEvent
	for _, event := range logger.GetEvents() {
		if event.EventType == Error {
			result = append(result, event)
		}
	}
	return result
}

// Cross-SDK scenarios 16 and 53-58: a "json" custom field may hold any JSON
// value, and the default parser must not reject non-object values.
func TestGetCustomFieldValueParsesNonObjectJSONValues(t *testing.T) {
	var logger = &MockEventLogger{}
	var ctx = createJSONCustomFieldContext(t, DefaultVariableParser{}, logger)

	if errs := errorEvents(logger); len(errs) != 0 {
		t.Fatalf("context creation logged %d error events, want 0: %#v", len(errs), errs)
	}

	var cases = []struct {
		field     string
		fieldType string
		want      interface{}
	}{
		{"country", "string", "US,PT,ES,DE,FR"},
		{"json_object", "json", map[string]interface{}{"123": 1.0, "456": 0.0}},
		{"json_array", "json", []interface{}{"hello", "world"}},
		{"json_number", "json", 123.0},
		{"json_string", "json", "hello"},
		{"json_boolean", "json", true},
		{"json_null", "json", nil},
	}
	for _, tc := range cases {
		got := ctx.GetCustomFieldValue("exp_test_abc", tc.field)
		if !reflect.DeepEqual(tc.want, got) {
			t.Errorf("GetCustomFieldValue(%q) = %#v, want %#v", tc.field, got, tc.want)
		}
		assertAny(tc.fieldType, ctx.GetCustomFieldValueType("exp_test_abc", tc.field), t)
	}

	if errs := errorEvents(logger); len(errs) != 0 {
		t.Errorf("reading valid fields logged %d error events, want 0: %#v", len(errs), errs)
	}

	keys, err := ctx.GetVariableKeys()
	assertAny(nil, err, t)
	assertAny(map[string][]string{"button.color": {"exp_test_abc"}}, keys, t)
}

func TestGetCustomFieldValueLogsInvalidJSONOnRead(t *testing.T) {
	var logger = &MockEventLogger{}
	var ctx = createJSONCustomFieldContext(t, DefaultVariableParser{}, logger)

	if errs := errorEvents(logger); len(errs) != 0 {
		t.Fatalf("context creation logged %d error events, want 0: %#v", len(errs), errs)
	}

	if got := ctx.GetCustomFieldValue("exp_test_abc", "json_invalid"); got != nil {
		t.Errorf("GetCustomFieldValue(json_invalid) = %#v, want nil", got)
	}

	var errs = errorEvents(logger)
	if len(errs) != 1 {
		t.Fatalf("got %d error events, want 1: %#v", len(errs), errs)
	}
	err, ok := errs[0].Data.(error)
	if !ok {
		t.Fatalf("error event data is %T, want error", errs[0].Data)
	}
	if !strings.Contains(err.Error(), "json_invalid") || !strings.Contains(err.Error(), "exp_test_abc") {
		t.Errorf("error message %q does not name the field and experiment", err.Error())
	}
}

func TestGetCustomFieldValueClearsParseErrorWhenDataChanges(t *testing.T) {
	var logger = &MockEventLogger{}
	var ctx = createJSONCustomFieldContext(t, DefaultVariableParser{}, logger)

	var fixed = jsonCustomFieldContextData()
	for i := range fixed.Experiments[0].CustomFieldValues {
		if fixed.Experiments[0].CustomFieldValues[i].Name == "json_invalid" {
			fixed.Experiments[0].CustomFieldValues[i].Value = `{"fixed":true}`
		}
	}
	ctx.SetData(fixed)

	assertAny(map[string]interface{}{"fixed": true}, ctx.GetCustomFieldValue("exp_test_abc", "json_invalid"), t)
	if errs := errorEvents(logger); len(errs) != 0 {
		t.Errorf("got %d error events after the field was fixed, want 0: %#v", len(errs), errs)
	}
}

// legacyVariableParser implements only VariableParser, as custom parsers
// written before CustomFieldValueParser existed do.
type legacyVariableParser struct {
	calls []string
}

func (p *legacyVariableParser) Parse(context Context, experimentName string, variantName string, config string) map[string]interface{} {
	p.calls = append(p.calls, variantName)
	return map[string]interface{}{"parsed": config}
}

func TestGetCustomFieldValueUsesLegacyParserParse(t *testing.T) {
	var parser = &legacyVariableParser{}
	var ctx = createJSONCustomFieldContext(t, parser, &MockEventLogger{})

	assertAny(map[string]interface{}{"parsed": `["hello", "world"]`},
		ctx.GetCustomFieldValue("exp_test_abc", "json_array"), t)

	var called = false
	for _, name := range parser.calls {
		if name == "json_array" {
			called = true
		}
	}
	if !called {
		t.Errorf("legacy parser Parse was not called for json custom field; calls: %v", parser.calls)
	}
}

type customFieldParser struct {
	DefaultVariableParser
}

func (customFieldParser) ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error) {
	if fieldName == "json_number" {
		return nil, errors.New("custom failure")
	}
	return "custom:" + value, nil
}

func TestGetCustomFieldValueUsesCustomFieldValueParser(t *testing.T) {
	var logger = &MockEventLogger{}
	var ctx = createJSONCustomFieldContext(t, customFieldParser{}, logger)

	assertAny("custom:true", ctx.GetCustomFieldValue("exp_test_abc", "json_boolean"), t)
	assertAny(nil, ctx.GetCustomFieldValue("exp_test_abc", "json_number"), t)

	var errs = errorEvents(logger)
	if len(errs) != 1 {
		t.Fatalf("got %d error events, want 1: %#v", len(errs), errs)
	}
	if err, ok := errs[0].Data.(error); !ok || !strings.Contains(err.Error(), "custom failure") {
		t.Errorf("error event data = %#v, want error containing %q", errs[0].Data, "custom failure")
	}
}
