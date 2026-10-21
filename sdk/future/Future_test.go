package future

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Listen racing with completion must call the callback exactly once.
func TestListenRacingSetResultCallsExactlyOnce(t *testing.T) {
	for i := 0; i < 2000; i++ {
		var f, done = New()
		var calls int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); f.Listen(func(Value, error) { atomic.AddInt32(&calls, 1) }) }()
		go func() { defer wg.Done(); done(1, nil) }()
		wg.Wait()
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("iteration %d: callback called %d times, want 1", i, got)
		}
	}
}

// A callback may register another listener on the same future.
func TestListenFromInsideCallback(t *testing.T) {
	var f, done = New()
	var inner int32
	f.Listen(func(Value, error) {
		f.Listen(func(Value, error) { atomic.AddInt32(&inner, 1) })
	})
	done(1, nil)
	if atomic.LoadInt32(&inner) != 1 {
		t.Fatal("inner listener was not called")
	}
}

// Concurrent SetResult calls settle the future once: one value, callbacks run once.
func TestConcurrentSetResultSettlesOnce(t *testing.T) {
	for i := 0; i < 2000; i++ {
		var f, done = New()
		var calls int32
		f.Listen(func(Value, error) { atomic.AddInt32(&calls, 1) })
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func(j int) { defer wg.Done(); done(j, nil) }(j)
		}
		wg.Wait()
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("iteration %d: callback ran %d times, want 1", i, got)
		}
	}
}

// A callback that panics must not stop the callbacks after it or unwind into
// the goroutine that settled the future.
func TestPanickingCallbackDoesNotStopOthers(t *testing.T) {
	var f, done = New()
	var after int32
	f.Listen(func(Value, error) { panic("callback panic") })
	f.Listen(func(Value, error) { atomic.AddInt32(&after, 1) })
	done(1, nil)
	if atomic.LoadInt32(&after) != 1 {
		t.Fatal("a callback after a panicking one did not run")
	}
}
