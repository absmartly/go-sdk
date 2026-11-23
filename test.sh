#!/bin/bash
set -e


echo "run golang fmt"
go fmt ./...

echo "run golang vet"
go vet ./...

echo "run golang tests"
go clean -testcache
go test -race ./...
