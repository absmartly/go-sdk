#!/bin/bash
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.49.0
elif [[ "$OSTYPE" == "darwin"* ]]; then
   brew install golangci-lint
fi
golangci-lint version

cd main
echo "run golang lint"
golangci-lint run
echo "run golang tests"
go test ./...
echo "run golang fmt"
go fmt ./...