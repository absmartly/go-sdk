package main

import (
	"encoding/json"
)

type DefaultAudienceDeserializer struct {
	AudienceDeserializer
}

func (dad DefaultAudienceDeserializer) Deserialize(bytes []byte, offset int, length int) (map[string]interface{}, error) { //TODO: remove offset and length everywhere
	var data map[string]interface{}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return nil, err
	} else {
		return data, nil
	}
}
