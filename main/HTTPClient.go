package main

import "github.com/absmartly/go-sdk/main/future"

type HTTPClient interface {
	Get(url string, query map[string]string, headers map[string]string) *future.Future
	Put(url string, query map[string]string, headers map[string]string, body []byte) *future.Future
	Post(url string, query map[string]string, headers map[string]string, body []byte) *future.Future
}
