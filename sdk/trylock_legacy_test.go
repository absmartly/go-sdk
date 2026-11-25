//go:build !go1.18

package sdk

import (
	"sync"
	"testing"
)

// Before Go 1.18 sync has no TryLock, so the lock-probing tests skip.
func tryLock(t testing.TB, _ interface{}) bool {
	t.Skip("lock probing needs Go 1.18 (sync.Mutex.TryLock)")
	return false
}

func tryRLock(t testing.TB, _ *sync.RWMutex) bool {
	t.Skip("lock probing needs Go 1.18 (sync.RWMutex.TryRLock)")
	return false
}
