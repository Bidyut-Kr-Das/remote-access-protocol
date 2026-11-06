package protocol

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"sync"
	"time"
)

var ErrDenied = errors.New("protocol: denied")

// HashPassword returns sha256(password || nonce).
func HashPassword(password string, nonce [32]byte) [32]byte {
	return sha256.Sum256(append([]byte(password), nonce[:]...))
}

type limEntry struct {
	fails int
	until time.Time
}

// Limiter counts failures per IP and blocks an IP for window after max failures.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	m      map[string]*limEntry
}

func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{max: max, window: window, now: now, m: map[string]*limEntry{}}
}

func (l *Limiter) Blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	if e == nil || e.until.IsZero() {
		return false
	}
	if l.now().Before(e.until) {
		return true
	}
	delete(l.m, ip) // block expired: start fresh
	return false
}

func (l *Limiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	if e == nil {
		e = &limEntry{}
		l.m[ip] = e
	}
	e.fails++
	if e.fails >= l.max {
		e.until = l.now().Add(l.window)
	}
}

func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

// ServerHandshake authenticates a client. accept decides whether channel is acceptable now.
func ServerHandshake(c io.ReadWriter, ip, password string, lim *Limiter,
	accept func(channel byte) (w, h uint16, ok bool)) (byte, error) {
	if lim.Blocked(ip) {
		return 0, ErrDenied
	}
	var ch Challenge
	if _, err := rand.Read(ch.Nonce[:]); err != nil {
		return 0, err
	}
	if err := Write(c, ch); err != nil {
		return 0, err
	}
	m, err := Read(c)
	if err != nil {
		return 0, err
	}
	hello, ok := m.(Hello)
	if !ok {
		return 0, ErrDenied
	}
	deny := func() (byte, error) {
		if err := Write(c, Deny{}); err != nil {
			return 0, err
		}
		return 0, ErrDenied
	}
	if hello.Version != Version {
		return deny()
	}
	want := HashPassword(password, ch.Nonce)
	if subtle.ConstantTimeCompare(want[:], hello.Hash[:]) != 1 {
		lim.Fail(ip)
		return deny()
	}
	w, h, ok := accept(hello.Channel)
	if !ok {
		return deny()
	}
	lim.Reset(ip)
	if err := Write(c, OK{W: w, H: h}); err != nil {
		return 0, err
	}
	return hello.Channel, nil
}

// ClientHandshake answers the server's challenge and returns the screen size.
func ClientHandshake(c io.ReadWriter, channel byte, password string) (w, h uint16, err error) {
	m, err := Read(c)
	if err != nil {
		return 0, 0, err
	}
	ch, ok := m.(Challenge)
	if !ok {
		return 0, 0, ErrDenied
	}
	if err = Write(c, Hello{Version: Version, Channel: channel, Hash: HashPassword(password, ch.Nonce)}); err != nil {
		return 0, 0, err
	}
	if m, err = Read(c); err != nil {
		return 0, 0, err
	}
	if o, ok := m.(OK); ok {
		return o.W, o.H, nil
	}
	return 0, 0, ErrDenied
}
