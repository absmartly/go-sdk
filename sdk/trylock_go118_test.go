//go:build go1.18

package sdk

import (
	"sync"
	"testing"
)

// tryLock and tryRLock probe a lock without blocking. sync's TryLock needs
// Go 1.18; on older toolchains (trylock_legacy_test.go) the probing tests skip.
func tryLock(_ testing.TB, m interface{ TryLock() bool }) bool { return m.TryLock() }

func tryRLock(_ testing.TB, m *sync.RWMutex) bool { return m.TryRLock() }
