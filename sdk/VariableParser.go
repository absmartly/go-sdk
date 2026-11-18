package sdk

type VariableParser interface {
	Parse(context Context, experimentName string, variantName string, variableValue string) map[string]interface{}
}

// CustomFieldValueParser is an optional interface a VariableParser can
// implement to parse "json" custom field values. Unlike variant configs, which
// are always JSON objects, a JSON custom field may hold any JSON value: an
// object, array, string, number, boolean or null.
//
// Which parser reads a "json" custom field:
//   - DefaultVariableParser (value or pointer): the SDK's own JSON parser,
//     which returns any JSON value (map, slice, string, float64 or bool); JSON
//     null and invalid JSON give a nil map[string]interface{}.
//   - A parser that implements this interface: its ParseCustomFieldValue.
//   - Any other parser, including one that embeds DefaultVariableParser:
//     its Parse, storing the map it returns, as in v1.1.1.
type CustomFieldValueParser interface {
	ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error)
}
