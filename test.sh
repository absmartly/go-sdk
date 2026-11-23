#!/bin/bash
set -e


echo "run golang fmt"
test -z "$(gofmt -l .)" || { gofmt -l .; echo "files above are not gofmt-formatted"; exit 1; }

echo "run golang vet"
go vet ./...

echo "run golang tests"
go clean -testcache
go test -race ./...
