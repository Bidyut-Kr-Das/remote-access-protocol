package main

import (
	"testing"
	"time"

	"rap/internal/protocol"
)

func acquireAsync(w *window, done <-chan struct{}) <-chan bool {
	r := make(chan bool, 1)
	go func() { r <- w.acquire(done) }()
	return r
}

func TestWindowBlocksWhenFull(t *testing.T) {
	w := newWindow(3)
	for i := 0; i < 3; i++ {
		w.add(1)
	}
	done := make(chan struct{})
	defer close(done)
	r := acquireAsync(w, done)
	select {
	case <-r:
		t.Fatal("acquire returned while window full")
	case <-time.After(50 * time.Millisecond):
	}
	w.ack()
	select {
	case ok := <-r:
		if !ok {
			t.Fatal("acquire returned false after ack")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire still blocked after ack")
	}
}

func TestWindowAckFloorsAtZero(t *testing.T) {
	w := newWindow(3)
	for i := 0; i < 5; i++ {
		w.ack()
	}
	for i := 0; i < 3; i++ {
		w.add(1)
	}
	done := make(chan struct{})
	defer close(done)
	select {
	case <-acquireAsync(w, done):
		t.Fatal("acquire returned; stale acks leaked into the window")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWindowAcquireAbortsOnDone(t *testing.T) {
	w := newWindow(1)
	w.add(1)
	done := make(chan struct{})
	r := acquireAsync(w, done)
	close(done)
	select {
	case ok := <-r:
		if ok {
			t.Fatal("acquire returned true after done")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not abort on done")
	}
}

func TestWindowCountsBatches(t *testing.T) {
	w := newWindow(1)
	w.add(3) // one batch of 3 Frame messages
	done := make(chan struct{})
	defer close(done)
	r := acquireAsync(w, done)
	for i := 0; i < 2; i++ {
		w.ack()
	}
	select {
	case <-r:
		t.Fatal("acquire returned before the whole batch was acked")
	case <-time.After(50 * time.Millisecond):
	}
	w.ack()
	select {
	case ok := <-r:
		if !ok {
			t.Fatal("acquire returned false after batch acked")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire still blocked after batch acked")
	}
}

func TestWindowBatchIsOneSlot(t *testing.T) {
	w := newWindow(2)
	w.add(10) // a 10-rect batch uses one of 2 slots
	done := make(chan struct{})
	defer close(done)
	select {
	case ok := <-acquireAsync(w, done):
		if !ok {
			t.Fatal("acquire returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("acquire blocked with only one batch in flight")
	}
}

func TestWindowEmptyAckIgnored(t *testing.T) {
	w := newWindow(1)
	w.ack() // stray: empty queue
	w.add(1)
	done := make(chan struct{})
	defer close(done)
	select {
	case <-acquireAsync(w, done):
		t.Fatal("acquire returned; stray ack counted against a later batch")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifyNeverBlocks(t *testing.T) {
	ch := make(chan struct{}, 1)
	for i := 0; i < 10; i++ {
		notify(ch)
	}
	if len(ch) != 1 {
		t.Fatalf("len = %d, want 1", len(ch))
	}
}

func TestIsInput(t *testing.T) {
	for _, c := range []struct {
		m    protocol.Msg
		want bool
	}{
		{protocol.MouseMove{}, true},
		{protocol.MouseButton{}, true},
		{protocol.MouseWheel{}, true},
		{protocol.Key{}, true},
		{protocol.Ping{}, false},
		{protocol.Ack{}, false},
		{protocol.Clipboard{}, false},
		{protocol.Release{}, false},
	} {
		if got := isInput(c.m); got != c.want {
			t.Errorf("isInput(%T) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestFrameInterval(t *testing.T) {
	now := time.Now()
	const active, idle = 33 * time.Millisecond, 100 * time.Millisecond
	tests := []struct {
		name              string
		lastInput, change time.Time
		want              time.Duration
	}{
		{"recent input", now.Add(-500 * time.Millisecond), now.Add(-5 * time.Second), active},
		{"recent change", now.Add(-5 * time.Second), now.Add(-500 * time.Millisecond), active},
		{"both stale", now.Add(-5 * time.Second), now.Add(-5 * time.Second), idle},
		{"zero values", time.Time{}, time.Time{}, idle},
	}
	for _, tc := range tests {
		if got := frameInterval(now, tc.lastInput, tc.change, active, idle); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	// active slower than idle: idle must not poll faster than active
	if got := frameInterval(now, time.Time{}, time.Time{}, 200*time.Millisecond, idle); got != 200*time.Millisecond {
		t.Errorf("slow active: got %v, want 200ms", got)
	}
}
