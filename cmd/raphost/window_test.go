package main

import (
	"testing"
	"time"
)

func acquireAsync(w *window, done <-chan struct{}) <-chan bool {
	r := make(chan bool, 1)
	go func() { r <- w.acquire(done) }()
	return r
}

func TestWindowBlocksWhenFull(t *testing.T) {
	w := newWindow(3)
	w.add(3)
	r := acquireAsync(w, make(chan struct{}))
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
	w.add(3)
	select {
	case <-acquireAsync(w, make(chan struct{})):
		t.Fatal("acquire returned; stale acks leaked into the window")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWindowAcquireAbortsOnDone(t *testing.T) {
	w := newWindow(3)
	w.add(3)
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

func TestNotifyNeverBlocks(t *testing.T) {
	ch := make(chan struct{}, 1)
	for i := 0; i < 10; i++ {
		notify(ch)
	}
	if len(ch) != 1 {
		t.Fatalf("len = %d, want 1", len(ch))
	}
}
