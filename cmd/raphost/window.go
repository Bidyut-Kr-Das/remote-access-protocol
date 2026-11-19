package main

import (
	"sync"

	"rap/internal/protocol"
)

// window bounds Frame messages in flight (sent, not yet acked).
type window struct {
	mu       sync.Mutex
	inflight int
	max      int
	wake     chan struct{}
}

func newWindow(max int) *window { return &window{max: max, wake: make(chan struct{}, 1)} }

// acquire blocks while the window is full; false if done closes first.
func (w *window) acquire(done <-chan struct{}) bool {
	for {
		w.mu.Lock()
		ok := w.inflight < w.max
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

// add records n Frame messages about to be sent.
func (w *window) add(n int) {
	w.mu.Lock()
	w.inflight += n
	w.mu.Unlock()
}

// ack records one viewer Ack; floors at 0 so a stray ack cannot widen the window.
func (w *window) ack() {
	w.mu.Lock()
	if w.inflight > 0 {
		w.inflight--
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
