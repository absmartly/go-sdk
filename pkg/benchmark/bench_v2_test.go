package benchmark

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/absmartly/go-sdk/pkg/absmartly"
)

func getV2Config() absmartly.Config {
	cfg := absmartly.Config{
		Endpoint:    os.Getenv("ABSMARTLY_ENDPOINT"),
		ApiKey:      "unused",
		Application: "app-unused",
		Environment: "env-unused",
	}

	return cfg
}

func BenchmarkV2InitSDK(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		sdk, err := absmartly.New(ctx, getV2Config())
		assert.NoError(b, err)
		if b.Failed() {
			break
		}
		sdk.Close()
	}
}

func BenchmarkV2GetContext(b *testing.B) {
	ctx := context.Background()
	sdk, err := absmartly.New(ctx, getV2Config())
	assert.NoError(b, err)
	if b.Failed() {
		return
	}
	for i := 0; i < b.N; i++ {
		sdk.UnitContext(absmartly.Units{"user_id": fmt.Sprintf("v2getcontext-%d", i)})
	}
	sdk.Close()
}

func BenchmarkV2SDKRefresh(b *testing.B) {
	ctx := context.Background()
	sdk, err := absmartly.New(ctx, getV2Config())
	assert.NoError(b, err)
	if b.Failed() {
		return
	}
	for i := 0; i < b.N; i++ {
		err = sdk.Refresh(ctx)
		assert.NoError(b, err)
		if b.Failed() {
			break
		}
	}
	sdk.Close()
}

func BenchmarkV2GetTreatment(b *testing.B) {
	ctx := context.Background()
	sdk, err := absmartly.New(ctx, getV2Config())
	assert.NoError(b, err)
	if b.Failed() {
		return
	}
	uc := sdk.UnitContext(absmartly.Units{"user_id": "v2gettreatment"})

	b.Run("fake", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = uc.GetTreatment("non-exist-name")
		}
	})
	b.Run("exist", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := uc.GetTreatment("filter-test")
			assert.NoError(b, err)
			if b.Failed() {
				return
			}
		}
		err = uc.Flush(ctx)
		assert.NoError(b, err)
	})
	sdk.Close()
}

func BenchmarkV2UsersTreatment(b *testing.B) {
	ctx := context.Background()
	sdk, err := absmartly.New(ctx, getV2Config())
	assert.NoError(b, err)
	if b.Failed() {
		return
	}
	for i := 0; i < b.N; i++ {
		uc := sdk.UnitContext(absmartly.Units{"user_id": fmt.Sprintf("v2userstreatment-%d", i)})
		_, err := uc.GetTreatment("filter-test")
		assert.NoError(b, err)
		if b.Failed() {
			break
		}
	}
	sdk.Close()
}

func BenchmarkV2Batch(b *testing.B) {
	b.Run("0", benchmarkBatch(0, 0))
	b.Run("10-1s", benchmarkBatch(10, 1*time.Second))
	b.Run("100-10ms", benchmarkBatch(100, 10*time.Millisecond))
	b.Run("100-100ms", benchmarkBatch(100, 100*time.Millisecond))
	b.Run("100-1s", benchmarkBatch(100, 1*time.Second))
	b.Run("1k-10ms", benchmarkBatch(1000, 10*time.Millisecond))
	b.Run("1k-100ms", benchmarkBatch(1000, 100*time.Millisecond))
	b.Run("1k-1s", benchmarkBatch(1000, 1*time.Second))
	b.Run("1k-10s", benchmarkBatch(1000, 10*time.Second))
	b.Run("10k-1s", benchmarkBatch(10_000, 1*time.Second))
}

func benchmarkBatch(size uint, interval time.Duration) func(b *testing.B) {
	return func(b *testing.B) {
		ctx := context.Background()
		cfg := getV2Config()
		cfg.BatchSize = size
		cfg.BatchInterval = interval
		sdk, err := absmartly.New(ctx, cfg)
		assert.NoError(b, err)
		if b.Failed() {
			return
		}
		for i := 0; i < b.N; i++ {
			uc := sdk.UnitContext(absmartly.Units{
				"user_id": fmt.Sprintf("v2batch-%d-%v-%d", size, interval, i),
			})
			_, err := uc.GetTreatment("filter-test")
			assert.NoError(b, err)
			if b.Failed() {
				b.FailNow()
			}
		}
		err = sdk.Flush(ctx)
		assert.NoError(b, err)
		sdk.Close()
	}
}
