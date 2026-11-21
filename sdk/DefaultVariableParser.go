package sdk

import (
	"encoding/json"
)

type DefaultVariableParser struct {
	VariableParser
}

func (vr DefaultVariableParser) Parse(context Context, experimentName string, variantName string, config string) map[string]interface{} {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(config), &data); err != nil {
		return nil
	} else {
		return data
	}
}

// defaultCustomFieldValueParser parses "json" custom fields for
// DefaultVariableParser. It is not a method of DefaultVariableParser, so a
// customer parser that embeds DefaultVariableParser does not inherit it and
// keeps receiving "json" custom fields through its own Parse, as in v1.1.1.
type defaultCustomFieldValueParser struct{}

// ParseCustomFieldValue returns any JSON value: an object, array, string,
// number or boolean. JSON null and invalid JSON give a nil
// map[string]interface{}, the value v1.1.1 returned for them.
func (defaultCustomFieldValueParser) ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return map[string]interface{}(nil), err
	}
	if data == nil {
		return map[string]interface{}(nil), nil
	}
	return data, nil
}

// customFieldValueParser returns the parser for "json" custom fields: the
// default one for DefaultVariableParser, or the configured parser when it
// implements CustomFieldValueParser itself.
func customFieldValueParser(parser VariableParser) (CustomFieldValueParser, bool) {
	switch parser.(type) {
	case DefaultVariableParser, *DefaultVariableParser:
		return defaultCustomFieldValueParser{}, true
	}
	custom, ok := parser.(CustomFieldValueParser)
	return custom, ok
}
