#!/bin/bash
set -e


echo "run golang fmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  echo "files above are not gofmt-formatted"
  exit 1
fi

echo "run golang vet"
go vet ./...

echo "run golang tests"
go clean -testcache
go test -race ./...
