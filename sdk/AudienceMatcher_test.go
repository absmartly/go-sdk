package sdk

import (
	"reflect"
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

func assertAny(want interface{}, got interface{}, t *testing.T) {
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %q, wanted %q", got, want)
	}
}
