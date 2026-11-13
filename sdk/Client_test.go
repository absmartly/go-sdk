package sdk

import (
	"bytes"
	"context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"github.com/go-resty/resty/v2"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
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
	statusCode int
	status     string
	bodyString string
}

func (h HttpMock) response() *resty.Response {
	if h.statusCode == 0 {
		h.statusCode = 200
	}
	if h.status == "" {
		h.status = "OK"
	}
	return &resty.Response{Request: &resty.Request{}, RawResponse: &http.Response{StatusCode: h.statusCode, Status: h.status, Body: io.NopCloser(strings.NewReader(h.bodyString))}}
}

func (h HttpMock) Get(url string, query map[string]string, headers map[string]string) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return h.response(), nil
	})
	return fut
}

func (h HttpMock) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return h.response(), nil
	})
	return fut
}
func (h HttpMock) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	fut := future.Call(func() (future.Value, error) {
		return h.response(), nil
	})
	return fut
}

func TestGetContextData(t *testing.T) {

	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{statusCode: 200, status: "OK", bodyString: "{}"})
	var expected = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}
	var actual, err = client.GetContextData().Get(context.Background())
	assertAny(nil, err, t)
	assertAny(expected, actual, t)

}

func TestGetContextDataExceptionallyHTTP(t *testing.T) {

	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{statusCode: 500, status: "Internal Server Error", bodyString: "{}"})
	var actual, err = client.GetContextData().Get(context.Background())
	assertAny(nil, actual, t)
	assertAny(errors.New("Internal Server Error"), err, t)
}

func TestPublish(t *testing.T) {

	var event = jsonmodels.PublishEvent{}

	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{statusCode: 200, status: "OK", bodyString: "test"})
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

	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	var client = CreateClient(config, HttpMock{statusCode: 500, status: "Internal Server Error", bodyString: "test"})
	var actual, err = client.Publish(event).Get(context.Background())
	assertAny(errors.New("Internal Server Error"), err, t)
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
				Body:       io.NopCloser(strings.NewReader("{}")),
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
				Body:       io.NopCloser(strings.NewReader("{}")),
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
				Body:       io.NopCloser(strings.NewReader("{}")),
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

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get returned %v, want context.DeadlineExceeded", err)
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
				Body:       io.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
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
				Body:       io.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
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
				Body:       io.NopCloser(strings.NewReader(`{"error": "forbidden"}`)),
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
				Body:       io.NopCloser(strings.NewReader("")),
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
				Body:       io.NopCloser(strings.NewReader("")),
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
				Body:       io.NopCloser(strings.NewReader("")),
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
			Body:       io.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
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
			Body:       io.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
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
			Body:       io.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
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
		t.Errorf("Expected a rate limit error, got: %v", err)
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

func TestClientPartialResponse(t *testing.T) {
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experiments": [`))
	}))
	defer server.Close()

	var config = ClientConfig{
		Deserializer_: DefaultContextDataDeserializer{},
		Endpoint_:     server.URL,
		ApiKey_:       "test-api-key",
		Application_:  "website",
		Environment_:  "dev",
	}
	var client = CreateDefaultClient(config)

	actual, err := client.GetContextData().Get(context.Background())
	if err == nil {
		t.Errorf("Expected an error for a partial response, got result: %v", actual)
	}
	if data, ok := actual.(jsonmodels.ContextData); ok && len(data.Experiments) != 0 {
		t.Errorf("Expected no experiments from a partial response, got: %v", data)
	}
}

func TestCreateClientHTTPEndpointDoesNotWriteGlobalLog(t *testing.T) {
	var buf bytes.Buffer
	var prevOut = log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prevOut)

	var config = ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "http://localhost:8080/v1", ApiKey_: "test-api-key", Application_: "website", Environment_: "dev"}
	CreateClient(config, HttpMock{statusCode: 200, status: "OK", bodyString: "{}"})

	if buf.Len() != 0 {
		t.Fatalf("CreateClient wrote to the global logger: %q", buf.String())
	}
}

type headerCapturingHttp struct {
	HttpMock
	headers *map[string]string
}

func (h headerCapturingHttp) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {
	*h.headers = headers
	return h.HttpMock.Put(url, query, headers, body)
}

func TestPublishSendsVersionedAgentAndApplicationVersion(t *testing.T) {
	var headers map[string]string
	var config = ClientConfig{Endpoint_: "https://localhost/v1", ApiKey_: "test-api-key", Application_: "website",
		Environment_: "dev", ApplicationVersion_: "1000123"}
	var client = CreateClient(config, headerCapturingHttp{HttpMock{bodyString: "{}"}, &headers})
	var _, err = client.Publish(jsonmodels.PublishEvent{}).Get(context.Background())
	assertAny(nil, err, t)

	assertAny("absmartly-go-sdk/"+SDKVersion, headers["X-Agent"], t)
	assertAny("1000123", headers["X-Application-Version"], t)
	assertAny("absmartly-go-sdk/"+SDKVersion, client.GetAgent(), t)
	assertAny("1000123", client.GetApplicationVersion(), t)
}

// The collector binds X-Application-Version as a whole number and rejects
// the request otherwise, so anything else is sent as "0".
func TestApplicationVersionHeaderIsSentOnlyWhenNumeric(t *testing.T) {
	for version, want := range map[string]string{
		"":                    "0",
		"   ":                 "0",
		"42":                  "42",
		" 42 ":                "42",
		"2.4.1":               "0",
		"v2":                  "0",
		"-1":                  "0",
		"1234567890123456789": "1234567890123456789",
		"9223372036854775807": "9223372036854775807",
		"9223372036854775808": "0",
		"007":                 "007",
	} {
		var client = CreateClient(ClientConfig{ApplicationVersion_: version}, HttpMock{})
		if got := client.GetApplicationVersion(); got != want {
			t.Errorf("application version %q: header %q, want %q", version, got, want)
		}
	}
}

func TestSDKAgentFormat(t *testing.T) {
	assertAny("absmartly-go-sdk", SDKName, t)
	assertAny(SDKName+"/"+SDKVersion, SDKAgent, t)
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(SDKVersion) {
		t.Errorf("SDKVersion %q is not MAJOR.MINOR.PATCH", SDKVersion)
	}
}

func TestBuilderApplicationVersion(t *testing.T) {
	var headers map[string]string
	var abs, err = NewBuilder().Endpoint("https://localhost/v1").ApiKey("key").Application("website").
		Environment("dev").ApplicationVersion("7").HTTPClient(headerCapturingHttp{HttpMock{bodyString: "{}"}, &headers}).Build()
	assertAny(nil, err, t)
	_, err = abs.Client_.Publish(jsonmodels.PublishEvent{}).Get(context.Background())
	assertAny(nil, err, t)
	assertAny("7", headers["X-Application-Version"], t)
}

// Cross-SDK scenario 320: the context fetch must carry the same SDK headers as
// publish (X-API-Key, X-Agent, X-Environment, X-Application,
// X-Application-Version), and keep its application/environment query.
func TestGetContextDataSendsSDKHeaders(t *testing.T) {
	var got *http.Request
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experiments":[]}`))
	}))
	defer server.Close()

	var config = ClientConfig{Endpoint_: server.URL + "/v1", ApiKey_: "test-api-key", Application_: "website",
		Environment_: "dev", ApplicationVersion_: "1000123"}
	var client = CreateDefaultClient(config)
	var _, err = client.GetContextData().Get(context.Background())
	assertAny(nil, err, t)

	if got == nil {
		t.Fatal("no context request reached the server")
	}
	assertAny(http.MethodGet, got.Method, t)
	assertAny("/v1/context", got.URL.Path, t)
	assertAny("website", got.URL.Query().Get("application"), t)
	assertAny("dev", got.URL.Query().Get("environment"), t)
	assertAny("test-api-key", got.Header.Get("X-API-Key"), t)
	assertAny("website", got.Header.Get("X-Application"), t)
	assertAny("dev", got.Header.Get("X-Environment"), t)
	assertAny("1000123", got.Header.Get("X-Application-Version"), t)
	var agent = got.Header.Get("X-Agent")
	if !regexp.MustCompile(`^absmartly-[a-z0-9]+-sdk/[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]*)?$`).MatchString(agent) {
		t.Errorf("X-Agent %q does not match absmartly-<lang>-sdk/<semver>", agent)
	}
	assertAny(SDKAgent, agent, t)
}
