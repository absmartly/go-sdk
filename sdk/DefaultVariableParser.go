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
