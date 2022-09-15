package main

import "github.com/absmartly/go-sdk/main/future"

type ContextDataProvider interface {
	GetContextData() *future.Future
}
