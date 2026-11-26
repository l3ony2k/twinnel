package rpc

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Bus keeps the last events in a ring and lets clients long-poll for new
// ones (events.wait). It is safe for concurrent use.
type Bus struct {
	mu     sync.Mutex
	buf    []Event
	size   int
	seq    uint64
	notify chan struct{} // closed and replaced on every publish
	now    func() time.Time
}

// NewBus keeps up to size events (default 256).
func NewBus(size int) *Bus {
	if size <= 0 {
		size = 256
	}
	return &Bus{size: size, notify: make(chan struct{}), now: time.Now}
}

// Publish adds an event. data is marshalled to JSON; a marshal error is
// replaced by a null payload.
func (b *Bus) Publish(typ string, data any) Event {
	var raw json.RawMessage
	if data != nil {
		if bs, err := json.Marshal(data); err == nil {
			raw = bs
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e := Event{Seq: b.seq, Time: b.now().UTC(), Type: typ, Data: raw}
	b.buf = append(b.buf, e)
	if len(b.buf) > b.size {
		b.buf = append(b.buf[:0:0], b.buf[len(b.buf)-b.size:]...)
	}
	close(b.notify)
	b.notify = make(chan struct{})
	return e
}

// Since returns buffered events with Seq > after, and the seq to pass next.
// If events after `after` were already dropped, the result starts with a
// synthetic events.truncated event so the client knows to resync.
func (b *Bus) Since(after uint64) ([]Event, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sinceLocked(after)
}

func (b *Bus) sinceLocked(after uint64) ([]Event, uint64) {
	if after > b.seq { // helper restarted; the client's cursor is from an older run
		after = 0
	}
	var out []Event
	if len(b.buf) > 0 && after > 0 && b.buf[0].Seq > after+1 {
		out = append(out, Event{Seq: b.buf[0].Seq - 1, Time: b.now().UTC(), Type: EventEventsTruncated})
	}
	for _, e := range b.buf {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, b.seq
}

// Wait blocks until there are events after `after`, ctx ends, or timeout
// passes. It never returns an error; an expired wait returns no events.
func (b *Bus) Wait(ctx context.Context, after uint64, timeout time.Duration) ([]Event, uint64) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		b.mu.Lock()
		evs, next := b.sinceLocked(after)
		ch := b.notify
		b.mu.Unlock()
		if len(evs) > 0 {
			return evs, next
		}
		select {
		case <-ch:
		case <-t.C:
			return nil, next
		case <-ctx.Done():
			return nil, next
		}
	}
}
