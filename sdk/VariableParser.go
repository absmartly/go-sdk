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
// for "json" custom fields and stores the value it returns. Other parsers use
// Parse and store its map result, as in v1.1.1.
type CustomFieldValueParser interface {
	ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error)
}
