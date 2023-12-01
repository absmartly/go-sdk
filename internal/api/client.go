package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/absmartly/go-sdk/internal/model"
)

type Client struct {
	hc        *http.Client
	endpoint  string
	header    http.Header
	headerPut http.Header
	query     string
	pool      *sync.Pool
}

func NewClient(endpoint, key, app, env string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	header := http.Header{}
	header.Set("X-API-Key", key)
	header.Set("X-Application", app)
	header.Set("X-Environment", env)
	header.Set("X-Application-Version", "0")
	header.Set("X-Agent", "absmartly-go-sdk/v2")
	headerPut := header.Clone()
	headerPut.Set("Content-Type", "application/json")
	query := url.Values{
		"application": []string{app},
		"environment": []string{env},
	}.Encode()
	c := &Client{
		hc:        hc,
		endpoint:  strings.TrimSuffix(endpoint, "/"),
		header:    header,
		headerPut: headerPut,
		query:     query,
		pool:      &sync.Pool{New: func() interface{} { return &bytes.Buffer{} }},
	}

	return c
}

func (c *Client) GetContext(ctx context.Context) (*model.Context, error) {
	uri := c.endpoint + "/context?" + c.query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.header
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("get context status '%s'", resp.Status)
	}
	mc := model.Context{}
	err = json.NewDecoder(resp.Body).Decode(&mc)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	return &mc, nil
}

func (c *Client) PutEvents(ctx context.Context, events []json.RawMessage) error {
	uri := c.endpoint + "/context?" + c.query
	buf := c.pool.Get().(*bytes.Buffer)
	defer func() {
		if buf != nil {
			buf.Reset()
			// time.Sleep(30 * time.Nanosecond)
			c.pool.Put(buf)
		}
	}()
	var err error
	err = json.NewEncoder(buf).Encode(events)
	if err != nil {
		return err
	}
	//log.Println(buf.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uri, buf)
	if err != nil {
		return err
	}
	req.Header = c.headerPut
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("put events status '%s'", resp.Status)
	}

	return nil
}

func (c *Client) Flush(_ context.Context) error {
	return nil
}
