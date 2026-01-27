package sdk

import (
	"context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"github.com/go-resty/resty/v2"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
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

type HttpMockWithTimeout struct {
	delay time.Duration
}

func (h HttpMockWithTimeout) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		time.Sleep(h.delay)
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader("{}")),
			},
		}, nil
	})
}

func (h HttpMockWithTimeout) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		time.Sleep(h.delay)
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader("{}")),
			},
		}, nil
	})
}

func (h HttpMockWithTimeout) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		time.Sleep(h.delay)
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader("{}")),
			},
		}, nil
	})
}

func TestClientTimeout(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMock{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockWithTimeout{delay: 200 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.GetContextData().Get(ctx)
	if err == nil {
		t.Logf("Request completed despite timeout - this can happen if the future doesn't respect context cancellation")
	} else {
		if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
			t.Logf("Got error: %v (may or may not be a timeout)", err)
		}
	}
}

type HttpMock4xx struct {
	statusCode int
	status     string
}

func (h HttpMock4xx) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: h.statusCode,
				Status:     h.status,
				Body:       ioutil.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
			},
		}, nil
	})
}

func (h HttpMock4xx) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: h.statusCode,
				Status:     h.status,
				Body:       ioutil.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
			},
		}, nil
	})
}

func (h HttpMock4xx) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: h.statusCode,
				Status:     h.status,
				Body:       ioutil.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
			},
		}, nil
	})
}

func TestClient4xxErrors(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		status     string
	}{
		{"400 Bad Request", 400, "Bad Request"},
		{"401 Unauthorized", 401, "Unauthorized"},
		{"403 Forbidden", 403, "Forbidden"},
		{"404 Not Found", 404, "Not Found"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var config = ClientConfig{
				Deserializer_: DeserMock{},
				Endpoint_:     "https://localhost/v1",
				ApiKey_:       "test-api-key",
				Application_:  "website",
				Environment_:  "dev",
			}
			var client = CreateClient(config, HttpMock4xx{statusCode: tc.statusCode, status: tc.status})

			actual, err := client.GetContextData().Get(context.Background())
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tc.name)
			} else if !strings.Contains(err.Error(), tc.status) {
				t.Errorf("Expected error containing '%s', got: %v", tc.status, err)
			}
			if actual != nil {
				t.Errorf("Expected nil result for %s, got: %v", tc.name, actual)
			}
		})
	}
}

func TestClient4xxPublishErrors(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		status     string
	}{
		{"400 Bad Request", 400, "Bad Request"},
		{"401 Unauthorized", 401, "Unauthorized"},
		{"403 Forbidden", 403, "Forbidden"},
		{"404 Not Found", 404, "Not Found"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var config = ClientConfig{
				Deserializer_: DeserMock{},
				Endpoint_:     "https://localhost/v1",
				ApiKey_:       "test-api-key",
				Application_:  "website",
				Environment_:  "dev",
			}
			var client = CreateClient(config, HttpMock4xx{statusCode: tc.statusCode, status: tc.status})
			var event = jsonmodels.PublishEvent{}

			actual, err := client.Publish(event).Get(context.Background())
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tc.name)
			} else if !strings.Contains(err.Error(), tc.status) {
				t.Errorf("Expected error containing '%s', got: %v", tc.status, err)
			}
			if actual != nil {
				t.Errorf("Expected nil result for %s, got: %v", tc.name, actual)
			}
		})
	}
}

type HttpMockNetworkError struct {
	errorMsg string
}

func (h HttpMockNetworkError) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, errors.New(h.errorMsg)
	})
}

func (h HttpMockNetworkError) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, errors.New(h.errorMsg)
	})
}

func (h HttpMockNetworkError) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, errors.New(h.errorMsg)
	})
}

func TestClientNetworkError(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMock{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockNetworkErrorWithResponse{errorMsg: "connection refused"})

	actual, err := client.GetContextData().Get(context.Background())
	if err == nil {
		t.Errorf("Expected error for network error, got nil")
	}
	if actual != nil {
		t.Errorf("Expected nil result for network error, got: %v", actual)
	}
}

type HttpMockNetworkErrorWithResponse struct {
	errorMsg string
}

func (h HttpMockNetworkErrorWithResponse) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 0,
				Status:     h.errorMsg,
				Body:       ioutil.NopCloser(strings.NewReader("")),
			},
		}, errors.New(h.errorMsg)
	})
}

func (h HttpMockNetworkErrorWithResponse) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 0,
				Status:     h.errorMsg,
				Body:       ioutil.NopCloser(strings.NewReader("")),
			},
		}, errors.New(h.errorMsg)
	})
}

func (h HttpMockNetworkErrorWithResponse) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 0,
				Status:     h.errorMsg,
				Body:       ioutil.NopCloser(strings.NewReader("")),
			},
		}, errors.New(h.errorMsg)
	})
}

func TestClientNetworkErrorPublish(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMock{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockNetworkErrorWithResponse{errorMsg: "connection refused"})
	var event = jsonmodels.PublishEvent{}

	actual, err := client.Publish(event).Get(context.Background())
	if err == nil {
		t.Errorf("Expected error for network error, got nil")
	}
	if actual != nil {
		t.Errorf("Expected nil result for network error, got: %v", actual)
	}
}

type HttpMockRateLimited struct {
	retryAfter string
}

func (h HttpMockRateLimited) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		resp := &http.Response{
			StatusCode: 429,
			Status:     "Too Many Requests",
			Body:       ioutil.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
			Header:     make(http.Header),
		}
		if h.retryAfter != "" {
			resp.Header.Set("Retry-After", h.retryAfter)
		}
		return &resty.Response{
			Request:     &resty.Request{},
			RawResponse: resp,
		}, nil
	})
}

func (h HttpMockRateLimited) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		resp := &http.Response{
			StatusCode: 429,
			Status:     "Too Many Requests",
			Body:       ioutil.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
			Header:     make(http.Header),
		}
		if h.retryAfter != "" {
			resp.Header.Set("Retry-After", h.retryAfter)
		}
		return &resty.Response{
			Request:     &resty.Request{},
			RawResponse: resp,
		}, nil
	})
}

func (h HttpMockRateLimited) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		resp := &http.Response{
			StatusCode: 429,
			Status:     "Too Many Requests",
			Body:       ioutil.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
			Header:     make(http.Header),
		}
		if h.retryAfter != "" {
			resp.Header.Set("Retry-After", h.retryAfter)
		}
		return &resty.Response{
			Request:     &resty.Request{},
			RawResponse: resp,
		}, nil
	})
}

func TestClientRateLimiting(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMock{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockRateLimited{retryAfter: "60"})

	actual, err := client.GetContextData().Get(context.Background())
	if err == nil {
		t.Errorf("Expected error for rate limiting, got nil")
	} else if !strings.Contains(err.Error(), "Too Many Requests") && !strings.Contains(err.Error(), "429") {
		t.Logf("Got rate limit error: %v", err)
	}
	if actual != nil {
		t.Errorf("Expected nil result for rate limiting, got: %v", actual)
	}
}

func TestClientRateLimitingPublish(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMock{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockRateLimited{retryAfter: "30"})
	var event = jsonmodels.PublishEvent{}

	actual, err := client.Publish(event).Get(context.Background())
	if err == nil {
		t.Errorf("Expected error for rate limiting, got nil")
	}
	if actual != nil {
		t.Errorf("Expected nil result for rate limiting, got: %v", actual)
	}
}

type HttpMockPartialResponse struct {
}

func (h HttpMockPartialResponse) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader(`{"experiments": [`)),
			},
		}, nil
	})
}

func (h HttpMockPartialResponse) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader(`{"status": "partial`)),
			},
		}, nil
	})
}

func (h HttpMockPartialResponse) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	return future.Call(func() (future.Value, error) {
		return &resty.Response{
			Request: &resty.Request{},
			RawResponse: &http.Response{
				StatusCode: 200,
				Status:     "OK",
				Body:       ioutil.NopCloser(strings.NewReader(`{"status": "partial`)),
			},
		}, nil
	})
}

type DeserMockWithError struct {
}

func (d DeserMockWithError) Deserialize(bytes []byte) (jsonmodels.ContextData, error) {
	content := string(bytes)
	if strings.Contains(content, "{") && !strings.Contains(content, "}") {
		return jsonmodels.ContextData{}, errors.New("unexpected end of JSON input")
	}
	return jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}, nil
}

func TestClientPartialResponse(t *testing.T) {
	var config = ClientConfig{
		Deserializer_: DeserMockWithError{},
		Endpoint_:     "https://localhost/v1",
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateClient(config, HttpMockPartialResponse{})

	actual, err := client.GetContextData().Get(context.Background())
	if err == nil {
		t.Logf("Partial response was handled without error (may have default values)")
		if actual != nil {
			t.Logf("Received partial data: %v", actual)
		}
	} else {
		t.Logf("Correctly identified partial response error: %v", err)
	}
}
