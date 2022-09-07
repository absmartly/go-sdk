package main

import (
	"context"
	"errors"
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"github.com/go-resty/resty/v2"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type DeserMock struct {
}

func (d DeserMock) Deserialize(bytes []byte) (jsonmodels.ContextData, error) {
	return jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}, nil
}

type HttpMock struct {
}

var statusCode = 200
var status = "OK"
var bodyString = "{}"

func (h HttpMock) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: statusCode,
				Status:     status,
				Body:       ioutil.NopCloser(strings.NewReader(bodyString)),
			},
		}, nil
	})
	return fut
}

func (h HttpMock) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: statusCode,
				Status:     status,
				Body:       ioutil.NopCloser(strings.NewReader(bodyString)),
			},
		}, nil
	})
	return fut
}
func (h HttpMock) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: statusCode,
				Status:     status,
				Body:       ioutil.NopCloser(strings.NewReader(bodyString)),
			},
		}, nil
	})
	return fut
}

func TestGetContextData(t *testing.T) {

	statusCode = 200
	status = "OK"
	bodyString = "{}"
	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{})
	var expected = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}
	var actual, err = client.GetContextData().Get(context.Background())
	assertAny(nil, err, t)
	assertAny(expected, actual, t)

}

func TestGetContextDataExceptionallyHTTP(t *testing.T) {

	statusCode = 500
	status = "Internal Server Error"
	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{})
	var actual, err = client.GetContextData().Get(context.Background())
	assertAny(nil, actual, t)
	assertAny(errors.New(status), err, t)
}

func TestPublish(t *testing.T) {

	var event = jsonmodels.PublishEvent{}
	statusCode = 200
	status = "OK"
	bodyString = "test"
	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{})
	var actual, err = client.Publish(event).Get(context.Background())
	assertAny(nil, err, t)
	buf := new(strings.Builder)
	res, er := io.Copy(buf, actual.(*resty.Response).RawResponse.Body)
	assertAny(nil, er, t)
	assertAny(int64(4), res, t)
	assertAny("test", buf.String(), t)

}

func TestPublishExceptionally(t *testing.T) {

	var event = jsonmodels.PublishEvent{}
	statusCode = 500
	status = "Internal Server Error"
	bodyString = "test"
	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{})
	var actual, err = client.Publish(event).Get(context.Background())
	assertAny(errors.New(status), err, t)
	assertAny(nil, actual, t)

}

func assertAny(want interface{}, got interface{}, t *testing.T) {
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %q, wanted %q", got, want)
	}
}
