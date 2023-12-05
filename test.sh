#!/bin/bash

echo "run golang get"
go get github.com/go-resty/resty/v2@v2.7.0

echo "run golang fmt"
go fmt ./...

echo "run golang tests"
go clean -testcache
go test ./...
