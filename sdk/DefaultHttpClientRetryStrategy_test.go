package sdk

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-resty/resty/v2"
)

// The retried statuses are the v1.1.1 set: network errors, 502 and 503.
func TestRetryConditionStatuses(t *testing.T) {
	var retry = RetryCondition()
	for status, want := range map[int]bool{200: false, 400: false, 500: false, 502: true, 503: true, 504: false} {
		var rsp = &resty.Response{RawResponse: &http.Response{StatusCode: status}}
		assertAny(want, retry(rsp, nil), t)
	}
	assertAny(true, retry(nil, errors.New("network error")), t)
	assertAny(false, retry(nil, nil), t)
}
