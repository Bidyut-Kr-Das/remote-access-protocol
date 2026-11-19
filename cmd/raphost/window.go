package main

import (
	"sync"
	"time"

	"rap/internal/protocol"
)

// window bounds frame batches in flight (sent, not fully acked). Acks arrive in order on one
// conn, so each ack belongs to the oldest batch.
type window struct {
	mu      sync.Mutex
	batches []int // unacked Frame messages per batch, oldest first
	max     int
	wake    chan struct{}
}

func newWindow(max int) *window { return &window{max: max, wake: make(chan struct{}, 1)} }

// acquire blocks while the window is full; false if done closes first.
func (w *window) acquire(done <-chan struct{}) bool {
	for {
		w.mu.Lock()
		ok := len(w.batches) < w.max
		w.mu.Unlock()
		if ok {
			return true
		}
		select {
		case <-done:
			return false
		case <-w.wake:
		}
	}
}

// add records a batch of n Frame messages about to be sent.
func (w *window) add(n int) {
	w.mu.Lock()
	w.batches = append(w.batches, n)
	w.mu.Unlock()
}

// ack records one viewer Ack against the oldest batch; ignored when none is queued so a
// stray ack cannot widen the window.
func (w *window) ack() {
	w.mu.Lock()
	if len(w.batches) > 0 {
		if w.batches[0]--; w.batches[0] <= 0 {
			w.batches = w.batches[1:]
		}
	}
	w.mu.Unlock()
	notify(w.wake)
}

// notify is a non-blocking send on a cap-1 wake channel.
func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// isInput reports whether m is injected user input (should trigger an immediate capture).
func isInput(m protocol.Msg) bool {
	switch m.(type) {
	case protocol.MouseMove, protocol.MouseButton, protocol.MouseWheel, protocol.Key:
		return true
	}
	return false
}

// frameInterval is idle once neither input nor a screen change was seen for over 1 s, else active.
func frameInterval(now, lastInput, lastChange time.Time, active, idle time.Duration) time.Duration {
	if now.Sub(lastInput) > time.Second && now.Sub(lastChange) > time.Second {
		return max(idle, active) // idle never polls faster than active
	}
	return active
}
