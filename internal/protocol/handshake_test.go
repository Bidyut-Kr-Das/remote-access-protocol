package protocol

import (
	"net"
	"testing"
	"time"
)

const pw = "pässwörd✓"

func okAccept() (uint16, uint16, bool) { return 1920, 1080, true }

func runServer(t *testing.T, c net.Conn, ip, password string, lim *Limiter, acc func() (uint16, uint16, bool)) chan error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- ServerHandshake(c, ip, password, lim, acc) }()
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
	w, h, err := ClientHandshake(c, pw)
	if err != nil || w != 1920 || h != 1080 {
		t.Fatalf("client: %d %d %v", w, h, err)
	}
	if err := <-res; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestHandshakeWrongPassword(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, newLim(), okAccept)
	_, _, err := ClientHandshake(c, "x")
	if err != ErrDenied {
		t.Fatalf("client err %v", err)
	}
	if err := <-res; err != ErrDenied {
		t.Fatalf("server err %v", err)
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
	if err := Write(c, Hello{Version: 2, Hash: HashPassword(pw, ch.Nonce)}); err != nil {
		t.Fatal(err)
	}
	m, err = Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(Deny); !ok {
		t.Fatalf("got %T", m)
	}
	if err := <-res; err != ErrDenied {
		t.Fatalf("server err %v", err)
	}
}

func TestHandshakeAcceptRejects(t *testing.T) {
	lim := newLim()
	for i := 0; i < 3; i++ {
		s, c := net.Pipe()
		res := runServer(t, s, "1.1.1.1", pw, lim, func() (uint16, uint16, bool) { return 0, 0, false })
		if _, _, err := ClientHandshake(c, pw); err != ErrDenied {
			t.Fatalf("client err %v", err)
		}
		if err := <-res; err != ErrDenied {
			t.Fatalf("server err %v", err)
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
	err := ServerHandshake(s, "1.1.1.1", pw, lim, okAccept)
	if err != ErrDenied {
		t.Fatalf("err %v", err)
	}
	s.Close()
	if _, err := Read(c); err == nil {
		t.Fatal("something was written")
	}
}

// attempt runs one full handshake against lim; badVer sends Version 2 via a raw client.
func attempt(t *testing.T, lim *Limiter, password string, badVer bool) error {
	t.Helper()
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "9.9.9.9", pw, lim, okAccept)
	if badVer {
		m, err := Read(c)
		if err != nil {
			t.Fatal(err)
		}
		n := m.(Challenge).Nonce
		if err := Write(c, Hello{Version: 2, Hash: HashPassword(pw, n)}); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(c); err != nil {
			t.Fatal(err)
		}
	} else {
		ClientHandshake(c, password)
	}
	return <-res
}

func TestLockoutWrongPasswordCounts(t *testing.T) {
	lim := newLim()
	for i := 0; i < 3; i++ {
		attempt(t, lim, "x", false)
	}
	if !lim.Blocked("9.9.9.9") {
		t.Fatal("3 wrong passwords should block")
	}
}

func TestLockoutBadVersionNoCount(t *testing.T) {
	lim := newLim()
	for i := 0; i < 3; i++ {
		attempt(t, lim, pw, true)
	}
	if lim.Blocked("9.9.9.9") {
		t.Fatal("bad version counted")
	}
}

func TestLockoutResetOnSuccess(t *testing.T) {
	lim := newLim()
	attempt(t, lim, "x", false)
	attempt(t, lim, "x", false)
	if err := attempt(t, lim, pw, false); err != nil {
		t.Fatal(err)
	}
	attempt(t, lim, "x", false)
	attempt(t, lim, "x", false)
	if lim.Blocked("9.9.9.9") {
		t.Fatal("success did not reset counter")
	}
}

func TestHandshakeRejectsOversizeHeader(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, newLim(), okAccept)
	if _, err := Read(c); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte{0, 0x10, 0, 0}); err != nil { // len = 1 MiB, no body follows
		t.Fatal(err)
	}
	if err := <-res; err != ErrOversize {
		t.Fatalf("server err %v", err)
	}
}

func TestLockoutAppliesMidHandshake(t *testing.T) {
	lim := newLim()
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := runServer(t, s, "1.1.1.1", pw, lim, okAccept)
	m, err := Read(c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ { // parallel attempts lock the IP after our Challenge
		lim.Fail("1.1.1.1")
	}
	if err := Write(c, Hello{Version: Version, Hash: HashPassword(pw, m.(Challenge).Nonce)}); err != nil {
		t.Fatal(err)
	}
	if m, err = Read(c); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(Deny); !ok {
		t.Fatalf("got %T", m)
	}
	if err := <-res; err != ErrDenied {
		t.Fatalf("server err %v", err)
	}
}
