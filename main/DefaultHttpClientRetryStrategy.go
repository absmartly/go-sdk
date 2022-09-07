package main

import (
	"github.com/go-resty/resty/v2"
)

func RetryCondition() func(*resty.Response, error) bool {
	return func(rsp *resty.Response, err error) bool {
		return rsp.StatusCode() == 502 || rsp.StatusCode() == 503
	}
}
