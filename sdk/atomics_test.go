package sdk

import "sync/atomic"

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
