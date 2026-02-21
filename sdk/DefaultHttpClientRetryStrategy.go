package sdk

import (
	"github.com/go-resty/resty/v2"
)

func RetryCondition() func(*resty.Response, error) bool {
	return func(rsp *resty.Response, err error) bool {
		return err != nil || (rsp != nil && (rsp.StatusCode() == 502 || rsp.StatusCode() == 503 || rsp.StatusCode() == 504))
	}
}
