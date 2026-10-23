package sdk

import (
	"encoding/json"
	"errors"
)

type DefaultVariableParser struct {
	VariableParser
}

func (vr DefaultVariableParser) Parse(context Context, experimentName string, variantName string, config string) map[string]interface{} {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(config), &data); err != nil {
		if context.EventLogger_ != nil {
			notifyLogger(context.EventLogger_, context, Error,
				errors.New("DefaultVariableParser.Parse: Failed to parse variant config for experiment '"+experimentName+
					"', variant '"+variantName+"': "+err.Error()))
		}
		return nil
	}
	return data
}

// ParseCustomFieldValue parses a "json" custom field value. It accepts any
// JSON value. Objects become map[string]interface{}, arrays []interface{},
// numbers float64, and strings, booleans and null their Go equivalents.
// Invalid JSON returns a nil value and the decoding error.
func (vr DefaultVariableParser) ParseCustomFieldValue(context Context, experimentName string, fieldName string, value string) (interface{}, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return nil, err
	}
	return data, nil
}
