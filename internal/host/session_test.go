package host

import (
	"errors"
	"net"
	"testing"
	"time"

	"rdp-go/internal/protocol"
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
func join(t *testing.T, m *Manager, ln net.Listener, ch byte) (sess *Session, ok bool, cerr error, c net.Conn) {
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
		sess, ok = m.Accept(sc)
	}()
	_, _, cerr = protocol.ClientHandshake(c, ch, pw)
	<-done
	return
}

func TestPairTwoChannels(t *testing.T) {
	m, ln := setup(t, time.Second)
	if s, ok, err, _ := join(t, m, ln, protocol.ChannelScreen); err != nil || ok || s != nil {
		t.Fatalf("first: %v %v %v", s, ok, err)
	}
	s, ok, err, _ := join(t, m, ln, protocol.ChannelInput)
	if err != nil || !ok || s == nil || s.Screen == nil || s.Input == nil {
		t.Fatalf("second: %v %v %v", s, ok, err)
	}
}

func TestDuplicateChannelDenied(t *testing.T) {
	m, ln := setup(t, time.Second)
	join(t, m, ln, protocol.ChannelScreen)
	if _, ok, err, _ := join(t, m, ln, protocol.ChannelScreen); ok || !errors.Is(err, protocol.ErrDenied) {
		t.Fatalf("want ErrDenied, got ok=%v err=%v", ok, err)
	}
}

func TestThirdConnDeniedWhileActive(t *testing.T) {
	m, ln := setup(t, time.Second)
	join(t, m, ln, protocol.ChannelScreen)
	join(t, m, ln, protocol.ChannelInput)
	if _, ok, err, _ := join(t, m, ln, protocol.ChannelInput); ok || !errors.Is(err, protocol.ErrDenied) {
		t.Fatalf("want ErrDenied, got ok=%v err=%v", ok, err)
	}
	m.End()
	if _, _, err, _ := join(t, m, ln, protocol.ChannelScreen); err != nil {
		t.Fatalf("after End: %v", err)
	}
}

func TestPairTimeout(t *testing.T) {
	m, ln := setup(t, 50*time.Millisecond)
	_, _, err, c := join(t, m, ln, protocol.ChannelScreen)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("want read error after timeout")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("conn not closed by pair timeout")
	}
	m2, ln2 := setup(t, time.Second)
	join(t, m2, ln2, protocol.ChannelScreen)
	if s, ok, err, _ := join(t, m2, ln2, protocol.ChannelInput); err != nil || !ok || s == nil {
		t.Fatalf("new pair: %v %v %v", s, ok, err)
	}
}
