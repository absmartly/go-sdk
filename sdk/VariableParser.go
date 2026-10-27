package sdk

type VariableParser interface {
	Parse(context Context, experimentName string, variantName string, variableValue string) map[string]interface{}
}

// CustomFieldValueParser is an optional interface a VariableParser can
// implement to parse "json" custom field values. Unlike variant configs, which
// are always JSON objects, a JSON custom field may hold any JSON value: an
// object, array, string, number, boolean or null.
//
// When the configured VariableParser implements this interface, Context uses it
// for "json" custom fields and reports a returned error through the event
// logger only when the field is read with GetCustomFieldValue. Other parsers use
// Parse and store its map result.
type CustomFieldValueParser interface {
	ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error)
}
