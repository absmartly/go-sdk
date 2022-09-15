package main

import (
	"testing"
)

func TestDefaultAudienceDeserializer_Deserialize(t *testing.T) {
	var deser = DefaultAudienceDeserializer{}
	var audience = "{\"filter\":[{\"gte\":[{\"var\":\"age\"},{\"value\":20.0}]}]}"
	var bytes = []byte(audience)
	var expected = map[string]interface{}{"filter": []interface{}{
		map[string]interface{}{
			"gte": []interface{}{
				map[string]interface{}{
					"var": "age",
				},
				map[string]interface{}{
					"value": 20.0,
				},
			},
		}}}
	var actual, err = deser.Deserialize(bytes, 0, len(bytes))
	assertAny(nil, err, t)
	assertAny(expected, actual, t)
}

func TestDefaultAudienceDeserializer_Incorrect(t *testing.T) {
	var deser = DefaultAudienceDeserializer{}
	var audience = "{\"filter\":[{\"gte\":[{\"var\":\"age\"},{\"value\":20}]]}"
	var bytes = []byte(audience)
	var actual, err = deser.Deserialize(bytes, 0, len(bytes))
	assertAny("invalid character ']' after object key:value pair", err.Error(), t)
	assertAny(0, len(actual), t)
}
