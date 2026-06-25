package sdk

import (
	"testing"
)

func TestAudienceMatcher_EvaluateNullOnEmpty(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}
	var res, err = matcher.Evaluate("", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)

	res, err = matcher.Evaluate("{}", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)

	res, err = matcher.Evaluate("null", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)
}

func TestAudienceMatcher_EvaluateNullIfFilterNotMapOrList(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}
	var res, err = matcher.Evaluate("{\"filter\":null}", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)

	res, err = matcher.Evaluate("{\"filter\":false}", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)

	res, err = matcher.Evaluate("{\"filter\":5}", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)

	res, err = matcher.Evaluate("{\"filter\":\"a\"}", nil)
	assertAny(Result{}, res, t)
	assertAny("can't evaluate data", err.Error(), t)
}

func TestAudienceMatcher_EvaluateNReturnsBoolean(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}
	var res, err = matcher.Evaluate("{\"filter\":[{\"value\":5}]}", nil)
	assertAny(true, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"value\":true}]}", nil)
	assertAny(true, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"value\":1}]}", nil)
	assertAny(true, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"value\":null}]}", nil)
	assertAny(false, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"value\":0}]}", nil)
	assertAny(false, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"not\":{\"var\":\"returning\"}}]}", map[string]interface{}{"returning": true})
	assertAny(false, res.Get(), t)
	assertAny(nil, err, t)

	res, err = matcher.Evaluate("{\"filter\":[{\"not\":{\"var\":\"returning\"}}]}", map[string]interface{}{"returning": false})
	assertAny(true, res.Get(), t)
	assertAny(nil, err, t)
}

func TestComplexNestedAudience(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}

	complexAudience := `{
		"filter": [{
			"or": [
				[
					{"gte": [{"var": {"path": "age"}}, {"value": 18}]},
					{"lte": [{"var": {"path": "age"}}, {"value": 65}]},
					{"eq": [{"var": {"path": "country"}}, {"value": "US"}]}
				],
				[
					{"gte": [{"var": {"path": "age"}}, {"value": 21}]},
					{"eq": [{"var": {"path": "country"}}, {"value": "CA"}]}
				],
				[
					{"gte": [{"var": {"path": "age"}}, {"value": 16}]},
					{"or": [
						{"eq": [{"var": {"path": "country"}}, {"value": "UK"}]},
						{"eq": [{"var": {"path": "country"}}, {"value": "DE"}]},
						{"eq": [{"var": {"path": "country"}}, {"value": "FR"}]}
					]}
				]
			]
		}]
	}`

	testCases := []struct {
		name     string
		attrs    map[string]interface{}
		expected bool
	}{
		{
			name:     "US adult matches",
			attrs:    map[string]interface{}{"age": 25, "country": "US"},
			expected: true,
		},
		{
			name:     "US minor does not match",
			attrs:    map[string]interface{}{"age": 16, "country": "US"},
			expected: false,
		},
		{
			name:     "CA 21+ matches",
			attrs:    map[string]interface{}{"age": 21, "country": "CA"},
			expected: true,
		},
		{
			name:     "CA under 21 does not match",
			attrs:    map[string]interface{}{"age": 20, "country": "CA"},
			expected: false,
		},
		{
			name:     "UK 16+ matches",
			attrs:    map[string]interface{}{"age": 17, "country": "UK"},
			expected: true,
		},
		{
			name:     "DE 16+ matches",
			attrs:    map[string]interface{}{"age": 16, "country": "DE"},
			expected: true,
		},
		{
			name:     "FR under 16 does not match",
			attrs:    map[string]interface{}{"age": 15, "country": "FR"},
			expected: false,
		},
		{
			name:     "Unknown country does not match",
			attrs:    map[string]interface{}{"age": 30, "country": "JP"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := matcher.Evaluate(complexAudience, tc.attrs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if res.Get() != tc.expected {
				t.Errorf("Expected %v for %v, got %v", tc.expected, tc.attrs, res.Get())
			}
		})
	}
}

func TestAudienceWithAllOperators(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}

	testCases := []struct {
		name     string
		filter   string
		attrs    map[string]interface{}
		expected bool
	}{
		{
			name:     "eq operator - equal",
			filter:   `{"filter":[{"eq":[{"var":{"path":"status"}},{"value":"active"}]}]}`,
			attrs:    map[string]interface{}{"status": "active"},
			expected: true,
		},
		{
			name:     "eq operator - not equal",
			filter:   `{"filter":[{"eq":[{"var":{"path":"status"}},{"value":"active"}]}]}`,
			attrs:    map[string]interface{}{"status": "inactive"},
			expected: false,
		},
		{
			name:     "gt operator - greater",
			filter:   `{"filter":[{"gt":[{"var":{"path":"score"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"score": 150},
			expected: true,
		},
		{
			name:     "gt operator - equal (should be false)",
			filter:   `{"filter":[{"gt":[{"var":{"path":"score"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"score": 100},
			expected: false,
		},
		{
			name:     "gte operator - equal",
			filter:   `{"filter":[{"gte":[{"var":{"path":"score"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"score": 100},
			expected: true,
		},
		{
			name:     "lt operator - less",
			filter:   `{"filter":[{"lt":[{"var":{"path":"score"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"score": 50},
			expected: true,
		},
		{
			name:     "lte operator - equal",
			filter:   `{"filter":[{"lte":[{"var":{"path":"score"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"score": 100},
			expected: true,
		},
		{
			name:     "in operator - substring in string",
			filter:   `{"filter":[{"in":[{"var":{"path":"language"}},{"value":"en"}]}]}`,
			attrs:    map[string]interface{}{"language": "en-US"},
			expected: true,
		},
		{
			name:     "in operator - substring not in string",
			filter:   `{"filter":[{"in":[{"var":{"path":"language"}},{"value":"fr"}]}]}`,
			attrs:    map[string]interface{}{"language": "en-US"},
			expected: false,
		},
		{
			name:     "not operator - negates true",
			filter:   `{"filter":[{"not":[{"var":{"path":"disabled"}}]}]}`,
			attrs:    map[string]interface{}{"disabled": true},
			expected: false,
		},
		{
			name:     "not operator - negates false",
			filter:   `{"filter":[{"not":[{"var":{"path":"disabled"}}]}]}`,
			attrs:    map[string]interface{}{"disabled": false},
			expected: true,
		},
		{
			name:     "match operator - regex match",
			filter:   `{"filter":[{"match":[{"var":{"path":"email"}},{"value":".*@example\\.com$"}]}]}`,
			attrs:    map[string]interface{}{"email": "user@example.com"},
			expected: true,
		},
		{
			name:     "null operator - checks null",
			filter:   `{"filter":[{"null":{"var":{"path":"optional"}}}]}`,
			attrs:    map[string]interface{}{},
			expected: true,
		},
		{
			name:     "null operator - not null",
			filter:   `{"filter":[{"null":{"var":{"path":"optional"}}}]}`,
			attrs:    map[string]interface{}{"optional": "value"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := matcher.Evaluate(tc.filter, tc.attrs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if res.Get() != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, res.Get())
			}
		})
	}
}

func TestAudienceBoundaryValues(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}

	testCases := []struct {
		name     string
		filter   string
		attrs    map[string]interface{}
		expected bool
	}{
		{
			name:     "zero boundary - gte 0",
			filter:   `{"filter":[{"gte":[{"var":{"path":"value"}},{"value":0}]}]}`,
			attrs:    map[string]interface{}{"value": 0},
			expected: true,
		},
		{
			name:     "zero boundary - gt 0",
			filter:   `{"filter":[{"gt":[{"var":{"path":"value"}},{"value":0}]}]}`,
			attrs:    map[string]interface{}{"value": 0},
			expected: false,
		},
		{
			name:     "negative boundary",
			filter:   `{"filter":[{"lt":[{"var":{"path":"value"}},{"value":0}]}]}`,
			attrs:    map[string]interface{}{"value": -1},
			expected: true,
		},
		{
			name:     "large integer",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":9007199254740991}]}]}`,
			attrs:    map[string]interface{}{"value": 9007199254740991},
			expected: true,
		},
		{
			name:     "float precision - very small difference",
			filter:   `{"filter":[{"gte":[{"var":{"path":"value"}},{"value":0.1}]}]}`,
			attrs:    map[string]interface{}{"value": 0.1},
			expected: true,
		},
		{
			name:     "float comparison",
			filter:   `{"filter":[{"gt":[{"var":{"path":"value"}},{"value":0.333}]}]}`,
			attrs:    map[string]interface{}{"value": 0.334},
			expected: true,
		},
		{
			name:     "empty string comparison",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":""}]}]}`,
			attrs:    map[string]interface{}{"value": ""},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := matcher.Evaluate(tc.filter, tc.attrs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if res.Get() != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, res.Get())
			}
		})
	}
}

func TestAudienceNullAttributes(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}

	testCases := []struct {
		name     string
		filter   string
		attrs    map[string]interface{}
		expected bool
	}{
		{
			name:     "missing attribute with eq",
			filter:   `{"filter":[{"eq":[{"var":{"path":"missing"}},{"value":"test"}]}]}`,
			attrs:    map[string]interface{}{},
			expected: false,
		},
		{
			name:     "missing attribute with null check",
			filter:   `{"filter":[{"null":{"var":{"path":"missing"}}}]}`,
			attrs:    map[string]interface{}{},
			expected: true,
		},
		{
			name:     "explicit null attribute",
			filter:   `{"filter":[{"null":{"var":{"path":"explicit_null"}}}]}`,
			attrs:    map[string]interface{}{"explicit_null": nil},
			expected: true,
		},
		{
			name:     "empty attrs with not null check",
			filter:   `{"filter":[{"not":[{"null":{"var":{"path":"missing"}}}]}]}`,
			attrs:    map[string]interface{}{},
			expected: false,
		},
		{
			name:     "empty attrs with value comparison",
			filter:   `{"filter":[{"gt":[{"var":{"path":"count"}},{"value":5}]}]}`,
			attrs:    map[string]interface{}{},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := matcher.Evaluate(tc.filter, tc.attrs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if res.Get() != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, res.Get())
			}
		})
	}
}

func TestAudienceTypeCoercion(t *testing.T) {
	var matcher = AudienceMatcher{
		Deserializer_: DefaultAudienceDeserializer{},
	}

	testCases := []struct {
		name     string
		filter   string
		attrs    map[string]interface{}
		expected bool
	}{
		{
			name:     "string to number comparison - equal",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":"100"}]}]}`,
			attrs:    map[string]interface{}{"value": 100},
			expected: true,
		},
		{
			name:     "number to string comparison - equal",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":100}]}]}`,
			attrs:    map[string]interface{}{"value": "100"},
			expected: true,
		},
		{
			name:     "boolean true to number 1",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":1}]}]}`,
			attrs:    map[string]interface{}{"value": true},
			expected: true,
		},
		{
			name:     "boolean false to number 0",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":0}]}]}`,
			attrs:    map[string]interface{}{"value": false},
			expected: true,
		},
		{
			name:     "string true to boolean",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":true}]}]}`,
			attrs:    map[string]interface{}{"value": "true"},
			expected: true,
		},
		{
			name:     "float to int comparison",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":10}]}]}`,
			attrs:    map[string]interface{}{"value": 10.0},
			expected: true,
		},
		{
			name:     "int to float comparison",
			filter:   `{"filter":[{"eq":[{"var":{"path":"value"}},{"value":10.0}]}]}`,
			attrs:    map[string]interface{}{"value": 10},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := matcher.Evaluate(tc.filter, tc.attrs)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if res.Get() != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, res.Get())
			}
		})
	}
}
