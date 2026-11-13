package sdk

import (
	"context"
	"errors"
	"strconv"
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
		"X-Application-Version": applicationVersionHeader(config.ApplicationVersion_),
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

// GetApplicationVersion returns the value sent as the X-Application-Version
// header: the configured application version when it is a whole number,
// otherwise "0".
func (c Client) GetApplicationVersion() string {
	return c.headers_["X-Application-Version"]
}

// GetAgent returns the value sent as the X-Agent header, for example
// "absmartly-go-sdk/1.2.0".
func (c Client) GetAgent() string {
	return c.headers_["X-Agent"]
}

// applicationVersionHeader returns version when it is a whole number the
// collector can parse (it binds the header as a 64-bit integer and rejects the
// request otherwise), and "0" for anything else, including an empty version.
func applicationVersionHeader(version string) string {
	version = strings.TrimSpace(version)
	if len(version) == 0 {
		return "0"
	}
	for _, c := range version {
		if c < '0' || c > '9' {
			return "0"
		}
	}
	if _, err := strconv.ParseInt(version, 10, 64); err != nil {
		return "0"
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
