package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type pusher interface {
	PutEvents(ctx context.Context, events []json.RawMessage) error
	Flush(ctx context.Context) error
}

type Batcher struct {
	buf []json.RawMessage
	mu  sync.Mutex

	size     int
	interval time.Duration
	p        pusher
}

func NewBatcher(size uint, interval time.Duration, p pusher) *Batcher {
	b := &Batcher{
		buf:      make([]json.RawMessage, 0, size),
		interval: interval,
		size:     int(size),
		p:        p,
	}

	return b
}

func (b *Batcher) PutEvents(ctx context.Context, events []json.RawMessage) error {
	if len(events) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	wasEmpty := len(b.buf) == 0
	for len(events) > 0 {
		left := cap(b.buf) - len(b.buf)
		if len(events) < left {
			left = len(events)
		}
		b.buf = append(b.buf, events[:left]...)
		events = events[left:]
		if len(b.buf) >= b.size {
			go func(buf []json.RawMessage) {
				err := b.p.PutEvents(ctx, b.buf)
				if err != nil {
					// todo log
				}
			}(b.buf)
			b.buf = make([]json.RawMessage, 0, b.size)
		}
	}
	if wasEmpty && len(b.buf) != 0 {
		time.AfterFunc(b.interval, b.timer)
	}

	return nil
}

func (b *Batcher) timer() {
	err := b.Flush(context.TODO())
	if err != nil {
		// todo log
	}
}

func (b *Batcher) Flush(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) > 0 {
		err := b.p.PutEvents(ctx, b.buf)
		b.buf = b.buf[:0]
		if err != nil {
			return err
		}
	}

	return b.p.Flush(ctx)
}
