package sdk

import (
	"context"
	"crypto/tls"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/go-resty/resty/v2"
	"net"
	"net/http"
)

type DefaultHttpClient struct {
	HTTPClient
	httpClient_ *resty.Client
}

type Logger struct {
	resty.Logger
}

func (l Logger) Errorf(format string, v ...interface{}) {}
func (l Logger) Warnf(format string, v ...interface{})  {}
func (l Logger) Debugf(format string, v ...interface{}) {}

func CreateDefaultHttpClient() DefaultHttpClient {
	return DefaultHttpClient{httpClient_: resty.New()}
}

func defaultTransportDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return dialer.DialContext
}

func (e DefaultHttpClient) DefaultHttpClientConfig(config DefaultHttpClientConfig) {
	e.httpClient_.SetRetryCount(config.MaxRetries_)
	e.httpClient_.SetRetryWaitTime(config.RetryInterval_)
	e.httpClient_.SetTimeout(config.ConnectionRequestTimeout_)
	e.httpClient_.AddRetryCondition(RetryCondition())
	e.httpClient_.SetLogger(config.Logger)
	var tr = &http.Transport{
		MaxIdleConns: 0,
		DialContext: defaultTransportDialContext(&net.Dialer{
			Timeout:   config.ConnectTimeout_,
			KeepAlive: config.ConnectionKeepAlive_,
		}),
		DisableCompression: true,
		MaxConnsPerHost:    200,
	}
	e.httpClient_.SetTransport(tr)
	e.httpClient_.SetTLSClientConfig(&tls.Config{})
}

func (e DefaultHttpClient) Get(url string, query map[string]string, headers map[string]string) *future.Future {

	fut := future.Call(func() (future.Value, error) {
		return e.httpClient_.R().SetQueryParams(query).SetHeaders(headers).Get(url)
	})
	return fut
}

func (e DefaultHttpClient) Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {

	fut := future.Call(func() (future.Value, error) {
		return e.httpClient_.R().SetQueryParams(query).SetHeaders(headers).SetBody(body).Put(url)
	})
	return fut
}

func (e DefaultHttpClient) Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future {

	fut := future.Call(func() (future.Value, error) {
		return e.httpClient_.R().SetQueryParams(query).SetHeaders(headers).SetBody(body).Post(url)
	})
	return fut
}
