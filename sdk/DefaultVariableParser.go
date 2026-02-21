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
		if context.EventLogger_ != nil {
			context.EventLogger_.HandleEvent(context, Error,
				"DefaultVariableParser.Parse: Failed to parse variant config for experiment '"+experimentName+
					"', variant '"+variantName+"': "+err.Error())
		}
		return nil
	}
	return data
}
