package benchmark

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/absmartly/go-sdk/sdk"
)

var (
	clients   = make(map[string]sdk.ClientI)
	clientsMu sync.Mutex
)

func getContext() *sdk.Context {
	return getContextWithUID("user", 747)
}

func getContextWithUID(prefix string, uid int) *sdk.Context {
	endpoint := os.Getenv("ABSMARTLY_ENDPOINT")
	return getContextWithParams(endpoint, fmt.Sprintf("%s-%d", prefix, uid))
}

func getContextWithParams(endpoint string, uid string) *sdk.Context {
	var clientConfig = sdk.ClientConfig{
		Endpoint_:    endpoint,
		ApiKey_:      os.Getenv("ABSMARTLY_APIKEY"),
		Application_: os.Getenv(`ABSMARTLY_APP`),
		Environment_: os.Getenv(`ABSMARTLY_ENV`),
	}

	clientsMu.Lock()
	if cli, ok := clients[endpoint]; !ok {
		cli = sdk.CreateDefaultClient(clientConfig)
		clients[endpoint] = cli
	}
	cli := clients[endpoint]
	clientsMu.Unlock()
	var sdkConfig = sdk.ABSmartlyConfig{
		Client_: cli,
	}

	var sd = sdk.Create(sdkConfig)

	var contextConfig = sdk.ContextConfig{
		Units_: map[string]string{ // add Unit
			"user_id": uid,
		}, PublishDelay_: 10000, RefreshInterval_: 5000}

	var ctx = sd.CreateContext(contextConfig)
	return ctx
}

func BenchmarkGetContext(b *testing.B) {
	for i := 0; i < b.N; i++ {
		c := getContext()
		c.WaitUntilReady()
		c.Close()
	}
}

func BenchmarkGetContextBadEndpoint(b *testing.B) {
	for i := 0; i < b.N; i++ {
		c := getContextWithParams("http://127.0.0.1:79", "any-747")
		c.WaitUntilReady()
		c.Close()
	}
}

func BenchmarkContextRefresh(b *testing.B) {
	c := getContext()
	c.WaitUntilReady()
	for i := 0; i < b.N; i++ {
		c.Refresh()
	}
}

func BenchmarkGetTreatment(b *testing.B) {
	c := getContext()
	c.WaitUntilReady()
	b.Run("fake", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment("non-exist-name")
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
		err := c.Publish()
		assert.NoError(b, err)
	})
	b.Run("exist", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment("filter-test")
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
		err := c.Publish()
		assert.NoError(b, err)
	})
	b.Run("fake-p", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment("non-exist-name")
			assert.NoError(b, err)
			err = c.Publish()
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
	})
	b.Run("exist-p", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment("filter-test")
			assert.NoError(b, err)
			err = c.Publish()
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
	})
	b.Run("names", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment(fmt.Sprintf("fake-%d", i))
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
		err := c.Publish()
		assert.NoError(b, err)
	})
	b.Run("names-p", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := c.GetTreatment(fmt.Sprintf("fake-p-%d", i))
			assert.NoError(b, err)
			err = c.Publish()
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
	})
}

func BenchmarkContextTreatment(b *testing.B) {
	b.Run("close", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := getContextWithUID("close", i)
			c.WaitUntilReady()
			_, err := c.GetTreatment("filter-test")
			assert.NoError(b, err)
			err = c.Publish()
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
			c.Close()
		}
	})
	b.Run("noclose", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := getContextWithUID("noclose", i)
			c.WaitUntilReady()
			_, err := c.GetTreatment("filter-test")
			assert.NoError(b, err)
			err = c.Publish()
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
	})
}
