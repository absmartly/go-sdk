package main

import (
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"testing"
)

func TestSerialize(t *testing.T) {

	var event = jsonmodels.PublishEvent{
		Hashed:      true,
		PublishedAt: 123456789,
		Units: []jsonmodels.Unit{{
			Type: "session_id",
			Uid:  "pAE3a1i5Drs5mKRNq56adA"}, {
			Type: "user_id",
			Uid:  "JfnnlDI7RTiF9RgfG2JNCw"},
		},
		Exposures: []jsonmodels.Exposure{{
			Id:               1,
			Name:             "exp_test_ab",
			Unit:             "session_id",
			Variant:          1,
			ExposedAt:        123470000,
			Assigned:         true,
			Eligible:         true,
			Overridden:       false,
			FullOn:           false,
			Custom:           false,
			AudienceMismatch: true,
		}},
		Goals: []jsonmodels.GoalAchievement{{
			Name:       "goal1",
			AchievedAt: 123456000,
			Properties: map[string]interface{}{
				"amount":     6,
				"value":      5.1,
				"tries":      1,
				"nested":     map[string]interface{}{"value": 5},
				"nested_arr": map[string]interface{}{"nested": []interface{}{1, 2, "test"}}},
		},
			{
				Name:       "goal2",
				AchievedAt: 123456789,
				Properties: nil,
			}},
		Attributes: []jsonmodels.Attribute{{
			Name:  "attr1",
			Value: "value1",
			SetAt: 123456000,
		}, {
			Name:  "attr2",
			Value: "value2",
			SetAt: 123456789,
		}, {
			Name:  "attr2",
			Value: nil,
			SetAt: 123450000,
		}, {
			Name:  "attr3",
			Value: map[string]interface{}{"nested": map[string]interface{}{"value": 5}},
			SetAt: 123470000,
		}, {
			Name:  "attr4",
			Value: map[string]interface{}{"nested": []interface{}{1, 2, "test"}},
			SetAt: 123480000,
		},
		},
	}

	var ser = DefaultContextEventSerializer{}
	var result, err = ser.Serialize(event)
	var expected = "{\"hashed\":true,\"units\":[{\"type\":\"session_id\",\"uid\":\"pAE3a1i5Drs5mKRNq56adA\"},{\"type\":\"user_id\",\"uid\":\"JfnnlDI7RTiF9RgfG2JNCw\"}],\"publishedAt\":123456789,\"exposures\":[{\"id\":1,\"name\":\"exp_test_ab\",\"unit\":\"session_id\",\"variant\":1,\"exposedAt\":123470000,\"assigned\":true,\"eligible\":true,\"overridden\":false,\"fullOn\":false,\"custom\":false,\"audienceMismatch\":true}],\"goals\":[{\"name\":\"goal1\",\"achievedAt\":123456000,\"properties\":{\"amount\":6,\"nested\":{\"value\":5},\"nested_arr\":{\"nested\":[1,2,\"test\"]},\"tries\":1,\"value\":5.1}},{\"name\":\"goal2\",\"achievedAt\":123456789}],\"attributes\":[{\"name\":\"attr1\",\"value\":\"value1\",\"setAt\":123456000},{\"name\":\"attr2\",\"value\":\"value2\",\"setAt\":123456789},{\"name\":\"attr2\",\"setAt\":123450000},{\"name\":\"attr3\",\"value\":{\"nested\":{\"value\":5}},\"setAt\":123470000},{\"name\":\"attr4\",\"value\":{\"nested\":[1,2,\"test\"]},\"setAt\":123480000}]}"
	assertAny(expected, string(result), t)
	assertAny(nil, err, t)
}

func TestSerializeBrokenJson(t *testing.T) {
	var broken = "{\"hashed\":true,\"units\":[{\"type\":\"session_id\",\"uid\":\"pAE3a1i5Drs5mKRNq56adA\"},{\"type\":\"user_id\",\"uid\":\"JfnnlDI7RTiF9RgfG2JNCw\"}],\"publishedAt\":123456789,\"exposures\":[{\"id\":1,\"name\":\"exp_test_ab\",\"unit\":\"session_id\",\"variant\":1,\"exposedAt\":123470000,\"assigned\":true,\"eligible\":true,\"overridden\":false,\"fullOn\":false,]}"
	var deser = DefaultContextDataDeserializer{}
	var result, err = deser.Deserialize([]byte(broken))
	assertAny("invalid character ']' looking for beginning of object key string", err.Error(), t)
	assertAny(jsonmodels.ContextData{}, result, t)
}
