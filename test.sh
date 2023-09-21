#!/bin/bash

echo "run golang fmt"
go fmt ./...

echo "run golang tests"
go clean -testcache
go test ./...
