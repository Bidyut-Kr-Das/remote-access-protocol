package host

import (
	"errors"
	"net"
	"testing"
	"time"

	"rap/internal/protocol"
)

const pw = "secret"

func setup(t *testing.T, timeout time.Duration) (*Manager, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return NewManager(pw, 100, 50, protocol.NewLimiter(100, time.Minute, time.Now), timeout), ln
}

// join dials ln, runs Accept (server) and ClientHandshake (client) concurrently.
func join(t *testing.T, m *Manager, ln net.Listener) (ok bool, cerr error) {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	sc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ok = m.Accept(sc)
	}()
	_, _, cerr = protocol.ClientHandshake(c, pw)
	<-done
	return
}

func TestSecondConnDeniedWhileActive(t *testing.T) {
	m, ln := setup(t, time.Second)
	if ok, err := join(t, m, ln); !ok || err != nil {
		t.Fatalf("first: %v %v", ok, err)
	}
	if ok, err := join(t, m, ln); ok || !errors.Is(err, protocol.ErrDenied) {
		t.Fatalf("want ErrDenied, got ok=%v err=%v", ok, err)
	}
	m.End()
	if ok, err := join(t, m, ln); !ok || err != nil {
		t.Fatalf("after End: %v %v", ok, err)
	}
}

func TestSilentPeerClosed(t *testing.T) {
	m, ln := setup(t, 50*time.Millisecond)
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if m.Accept(sc) {
		t.Fatal("silent peer accepted")
	}
	if ok, err := join(t, m, ln); !ok || err != nil { // slot was not claimed
		t.Fatalf("after timeout: %v %v", ok, err)
	}
}
