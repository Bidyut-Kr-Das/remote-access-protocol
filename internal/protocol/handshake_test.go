package protocol

import (
	"net"
	"testing"
	"time"
)

const pw = "pässwörd✓"

func okAccept(byte) (uint16, uint16, bool) { return 1920, 1080, true }

func runServer(t *testing.T, c net.Conn, ip, password string, lim *Limiter, acc func(byte) (uint16, uint16, bool)) chan [2]any {
	t.Helper()
	ch := make(chan [2]any, 1)
	go func() {
		chn, err := ServerHandshake(c, ip, password, lim, acc)
		ch <- [2]any{chn, err}
	}()
	return ch
}

func newLim() *Limiter {
	return NewLimiter(3, 60*time.Second, time.Now)
}

func TestHandshakeOK(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, newLim(), okAccept)
	w, h, err := ClientHandshake(c, ChannelInput, pw)
	if err != nil || w != 1920 || h != 1080 {
		t.Fatalf("client: %d %d %v", w, h, err)
	}
	r := <-res
	if r[0].(byte) != ChannelInput || r[1] != nil {
		t.Fatalf("server: %v", r)
	}
}

func TestHandshakeWrongPassword(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, newLim(), okAccept)
	_, _, err := ClientHandshake(c, ChannelScreen, "x")
	if err != ErrDenied {
		t.Fatalf("client err %v", err)
	}
	if r := <-res; r[1] != ErrDenied {
		t.Fatalf("server err %v", r[1])
	}
}

func TestHandshakeBadVersion(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, newLim(), okAccept)
	m, err := Read(c)
	if err != nil {
		t.Fatal(err)
	}
	ch := m.(Challenge)
	if err := Write(c, Hello{Version: 2, Channel: ChannelScreen, Hash: HashPassword(pw, ch.Nonce)}); err != nil {
		t.Fatal(err)
	}
	m, err = Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(Deny); !ok {
		t.Fatalf("got %T", m)
	}
	if r := <-res; r[1] != ErrDenied {
		t.Fatalf("server err %v", r[1])
	}
}

func TestHandshakeAcceptRejects(t *testing.T) {
	lim := newLim()
	for i := 0; i < 3; i++ {
		s, c := net.Pipe()
		res := runServer(t, s, "1.1.1.1", pw, lim, func(byte) (uint16, uint16, bool) { return 0, 0, false })
		if _, _, err := ClientHandshake(c, ChannelScreen, pw); err != ErrDenied {
			t.Fatalf("client err %v", err)
		}
		if r := <-res; r[1] != ErrDenied {
			t.Fatalf("server err %v", r[1])
		}
		s.Close()
		c.Close()
	}
	if lim.Blocked("1.1.1.1") {
		t.Fatal("accept rejection counted")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(3, 60*time.Second, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		l.Fail("a")
	}
	if !l.Blocked("a") {
		t.Fatal("want blocked")
	}
	now = now.Add(60 * time.Second)
	if l.Blocked("a") {
		t.Fatal("want unblocked")
	}
	l.Fail("a")
	l.Fail("a")
	l.Reset("a")
	l.Fail("a")
	l.Fail("a")
	if l.Blocked("a") {
		t.Fatal("reset should clear count")
	}
}

func TestHandshakeBlockedIP(t *testing.T) {
	lim := newLim()
	for i := 0; i < 3; i++ {
		lim.Fail("1.1.1.1")
	}
	s, c := net.Pipe()
	defer c.Close()
	_, err := ServerHandshake(s, "1.1.1.1", pw, lim, okAccept)
	if err != ErrDenied {
		t.Fatalf("err %v", err)
	}
	s.Close()
	if _, err := Read(c); err == nil {
		t.Fatal("something was written")
	}
}
