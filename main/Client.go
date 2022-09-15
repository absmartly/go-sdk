package main

import (
	"context"
	"errors"
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
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
		"X-Agent":               "absmartly-java-sdk",
	}
	cl.headers_ = headers

	var query = map[string]string{
		"application": config.Application_,
		"environment": config.Environment_,
	}
	cl.query_ = query
	return cl
}

func (c Client) GetContextData() *future.Future {
	var dataFuture = future.Call(func() (future.Value, error) {
		var fut = c.httpClient_.Get(c.url_, c.query_, nil)
		var value, err = fut.Get(context.Background())
		if err != nil || value.(*resty.Response).StatusCode()/100 != 2 {
			err = errors.New(value.(*resty.Response).Status())
			value = nil
		}
		if err == nil {
			value, err = c.deserializer_.Deserialize(value.(*resty.Response).Body())
		}
		return value, err
	})
	return dataFuture
}

func (c Client) Publish(event jsonmodels.PublishEvent) *future.Future {
	var dataFuture = future.Call(func() (future.Value, error) {
		var body, er = c.serializer_.Serialize(event)
		var fut = c.httpClient_.Put(c.url_, nil, c.headers_, body)
		var value, err = fut.Get(context.Background())
		if err != nil || value.(*resty.Response).StatusCode()/100 != 2 {
			err = errors.New(value.(*resty.Response).Status())
			value = nil
		}
		if er != nil {
			err = er
			value = nil
		}
		return value, err
	})
	return dataFuture
}
