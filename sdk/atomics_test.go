package sdk

import (
	"sync"
	"sync/atomic"
	"time"
)

// The module's floor is Go 1.17, before sync/atomic had typed values
// (Go 1.19), so the tests use these small equivalents.

type atomicBool struct{ v int32 }

func (b *atomicBool) Load() bool { return atomic.LoadInt32(&b.v) != 0 }

func (b *atomicBool) Store(value bool) {
	var n int32
	if value {
		n = 1
	}
	atomic.StoreInt32(&b.v, n)
}

func (b *atomicBool) CompareAndSwap(old bool, value bool) bool {
	var o, n int32
	if old {
		o = 1
	}
	if value {
		n = 1
	}
	return atomic.CompareAndSwapInt32(&b.v, o, n)
}

type atomicInt32 struct{ v int32 }

func (i *atomicInt32) Load() int32 { return atomic.LoadInt32(&i.v) }

func (i *atomicInt32) Add(delta int32) int32 { return atomic.AddInt32(&i.v, delta) }

type atomicContext struct{ v atomic.Value }

func (p *atomicContext) Load() *Context {
	var c, _ = p.v.Load().(*Context)
	return c
}

func (p *atomicContext) Store(c *Context) { p.v.Store(c) }

// Go 1.17 has no TryLock or TryRLock (Go 1.18), so lock probes take the lock
// on another goroutine and report whether it was granted within probeWait.
// A granted probe is released before it returns.
const probeWait = 200 * time.Millisecond

func probeLock(lock func(), unlock func()) bool {
	var granted = make(chan struct{})
	var abandon = make(chan struct{})
	go func() {
		lock()
		select {
		case granted <- struct{}{}:
		case <-abandon:
		}
		unlock()
	}()
	select {
	case <-granted:
		return true
	case <-time.After(probeWait):
		close(abandon)
		return false
	}
}

// lockFree reports whether mu could be locked now; it is left unlocked.
func lockFree(mu sync.Locker) bool { return probeLock(mu.Lock, mu.Unlock) }

// readLockFree reports whether mu could be read-locked now (a queued writer
// blocks new readers); it is left unlocked.
func readLockFree(mu *sync.RWMutex) bool { return probeLock(mu.RLock, mu.RUnlock) }
