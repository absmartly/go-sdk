package sdk

import (
	"context"
	"errors"
	"strings"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"github.com/go-resty/resty/v2"
)

type ClientI interface {
	GetContextData() *future.Future
	Publish(event jsonmodels.PublishEvent) *future.Future
}

type Client struct {
	url_          string
	query_        map[string]string
	headers_      map[string]string
	httpClient_   HTTPClient
	deserializer_ ContextDataDeserializer
	serializer_   ContextEventSerializer
	ClientI
}

func CreateDefaultClient(config ClientConfig) Client {
	var client = CreateDefaultHttpClient()
	client.DefaultHttpClientConfig(CreateDefaultHttpClientConfig())
	return CreateClient(config, client)
}

func CreateClient(config ClientConfig, httpClient HTTPClient) Client {
	var cl = Client{url_: config.Endpoint_ + "/context", serializer_: config.Serializer_, deserializer_: config.Deserializer_, httpClient_: httpClient}
	if cl.deserializer_ == nil {
		cl.deserializer_ = DefaultContextDataDeserializer{}
	}

	if cl.serializer_ == nil {
		cl.serializer_ = DefaultContextEventSerializer{}
	}

	var headers = map[string]string{
		"X-API-Key":             config.ApiKey_,
		"X-Application":         config.Application_,
		"X-Environment":         config.Environment_,
		"X-Application-Version": "0",
		"X-Agent":               SDKAgent,
		"Content-Type":          "application/json",
	}
	cl.headers_ = headers

	var query = map[string]string{
		"application": config.Application_,
		"environment": config.Environment_,
	}
	cl.query_ = query
	return cl
}

// GetApplication returns the application name from the client config, which the
// context injects into the attribute map audiences are evaluated against. It is
// the same value sent as the "application" request query parameter.
func (c Client) GetApplication() string {
	return c.query_["application"]
}

// GetEnvironment returns the environment name from the client config, which the
// context injects into the attribute map audiences are evaluated against. It is
// the same value sent as the "environment" request query parameter.
func (c Client) GetEnvironment() string {
	return c.query_["environment"]
}

// WithApplicationVersion returns a copy of the client that sends version as
// the X-Application-Version header on every request when it is 1 to 18 ASCII
// digits (surrounding spaces are ignored), and "0" otherwise: the collector
// reads the header as a 64-bit integer and rejects the request on anything
// else. The receiver is left unchanged. It is a method rather than a
// ClientConfig field so existing ClientConfig literals keep compiling.
func (c Client) WithApplicationVersion(version string) Client {
	var headers = make(map[string]string, len(c.headers_))
	for key, value := range c.headers_ {
		headers[key] = value
	}
	headers["X-Application-Version"] = applicationVersionHeader(version)
	c.headers_ = headers
	return c
}

// GetApplicationVersion returns the value sent as the X-Application-Version
// header: the version set with WithApplicationVersion when it is 1 to 18
// digits, otherwise "0".
func (c Client) GetApplicationVersion() string {
	return c.headers_["X-Application-Version"]
}

// GetAgent returns the value sent as the X-Agent header, for example
// "absmartly-go-sdk/1.2.0".
func (c Client) GetAgent() string {
	return c.headers_["X-Agent"]
}

// applicationVersionHeader returns the trimmed version when it is 1 to 18
// ASCII digits, which always fits the 64-bit integer the collector binds the
// header to, and "0" for anything else, including an empty version. This is
// the Java SDK's rule.
func applicationVersionHeader(version string) string {
	version = strings.TrimSpace(version)
	if len(version) == 0 || len(version) > 18 {
		return "0"
	}
	for i := 0; i < len(version); i++ {
		if version[i] < '0' || version[i] > '9' {
			return "0"
		}
	}
	return version
}

func (c Client) GetContextData() *future.Future {
	var dataFuture = future.Call(func() (future.Value, error) {
		var fut = c.httpClient_.Get(c.url_, c.query_, c.headers_)
		var value, err = fut.Get(context.Background())
		if err != nil {
			return nil, err
		}

		resp, ok := value.(*resty.Response)
		if !ok {
			return nil, errors.New("unexpected response type")
		}

		if resp.StatusCode()/100 != 2 {
			return nil, errors.New(resp.Status())
		}

		value, err = c.deserializer_.Deserialize(resp.Body())
		return value, err
	})
	return dataFuture
}

func (c Client) Publish(event jsonmodels.PublishEvent) *future.Future {
	var dataFuture = future.Call(func() (future.Value, error) {
		var body, er = c.serializer_.Serialize(event)
		if er != nil {
			return nil, er
		}

		var fut = c.httpClient_.Put(c.url_, nil, c.headers_, body)
		var value, err = fut.Get(context.Background())
		if err != nil {
			return nil, err
		}

		resp, ok := value.(*resty.Response)
		if !ok {
			return nil, errors.New("unexpected response type")
		}

		if resp.StatusCode()/100 != 2 {
			return nil, errors.New(resp.Status())
		}
		return value, nil
	})
	return dataFuture
}
