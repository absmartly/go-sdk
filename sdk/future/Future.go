package future

import (
	"context"
	"fmt"
	"sync"
)

// Value represents value concept. Can be anything.
type Value interface{}

// SetResultFunc is function to set result of the Future.
type SetResultFunc func(Value, error)

// Future holds the value of Future.
type Future struct {
	val   Value
	err   error
	ready chan struct{}

	mu        sync.Mutex
	callbacks []SetResultFunc
}

// New constructs new Future.
func New() (*Future, SetResultFunc) {
	f := &Future{ready: make(chan struct{})}
	return f, f.SetResult
}

// Get returns value when it's ready. Will return error when the ctx signal a cancelation.
func (f *Future) Get(ctx context.Context) (Value, error) {
	select {
	case <-f.ready:
		return f.val, f.err
	default:
	}

	select {
	case <-f.ready:
		return f.val, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Ready indicates whether result ready or not.
func (f *Future) Ready() bool {
	select {
	case <-f.ready:
		return true
	default:
		return false
	}
}

// SetResult settles the future. Only the first call has an effect, even when
// several goroutines settle the future at the same time.
func (f *Future) SetResult(v Value, err error) {
	f.mu.Lock()
	select {
	case <-f.ready:
		f.mu.Unlock()
		return
	default:
	}
	f.val, f.err = v, err
	close(f.ready)
	f.mu.Unlock()
	f.NotifyCallbacks()
}

// Listen for the result.
func (f *Future) Listen(callback SetResultFunc) {
	// Check readiness while holding the lock that NotifyCallbacks takes. Checking
	// first and locking after would lose the callback when the future completes
	// in between: NotifyCallbacks would already have run without it.
	f.mu.Lock()
	select {
	case <-f.ready:
		f.mu.Unlock()
		callback(f.val, f.err)
	default:
		f.callbacks = append(f.callbacks, callback)
		f.mu.Unlock()
	}
}

func (f *Future) NotifyCallbacks() {
	f.mu.Lock()
	var callbacks = f.callbacks
	f.callbacks = nil
	f.mu.Unlock()
	// Run the callbacks without the lock so one of them can call Listen on
	// this future. A callback that panics must not stop the callbacks after it
	// (they would never run) or unwind into whoever settled the future.
	for _, callback := range callbacks {
		f.runCallback(callback)
	}
}

func (f *Future) runCallback(callback SetResultFunc) {
	defer func() { _ = recover() }()
	callback(f.val, f.err)
}

func (f *Future) Join(ctx context.Context) {
	f.Get(ctx)
}

// Call will converts the sync function call as async call.
func Call(f func() (Value, error)) *Future {
	fut, setDone := New()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				var panicErr error
				if err, ok := r.(error); ok {
					panicErr = err
				} else {
					panicErr = &PanicError{Value: r}
				}
				setDone(nil, panicErr)
			}
		}()
		res, err := f()
		setDone(res, err)
	}()
	return fut
}

type PanicError struct {
	Value interface{}
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", e.Value)
}
