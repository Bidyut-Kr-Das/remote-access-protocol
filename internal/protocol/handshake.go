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

// chooseCodec picks H.264 when both masks have it, else JPEG (also for 0 = old peer).
func chooseCodec(viewer, host byte) byte {
	if viewer&host&CodecsH264 != 0 {
		return CodecH264
	}
	return CodecJPEG
}

// ServerHandshake authenticates a client. accept runs once the password is right and
// decides whether a session may start now; it returns the screen size sent in OK.
// codecs is the host's encoder mask (Codecs*); it returns the codec ID chosen for the session.
func ServerHandshake(c io.ReadWriter, ip, password string, lim *Limiter, codecs byte,
	accept func() (w, h uint16, ok bool)) (byte, error) {
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
	m, err := ReadMax(c, 64) // pre-auth: never allocate more than a Hello needs
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
	if lim.Blocked(ip) { // parallel attempts may have locked this IP since we started
		return deny()
	}
	want := HashPassword(password, ch.Nonce)
	if subtle.ConstantTimeCompare(want[:], hello.Hash[:]) != 1 {
		lim.Fail(ip)
		return deny()
	}
	w, h, ok := accept()
	if !ok {
		return deny()
	}
	lim.Reset(ip)
	codec := chooseCodec(hello.Codecs, codecs)
	o := OK{W: w, H: h}
	if hello.Codecs != 0 { // old viewers get today's exact 4-byte OK
		o.Codec = codec
	}
	return codec, Write(c, o)
}

// ClientHandshake answers the server's challenge, advertising codecs (the viewer's
// decoder mask, Codecs*), and returns the screen size and the codec ID the host chose.
func ClientHandshake(c io.ReadWriter, password string, codecs byte) (w, h uint16, codec byte, err error) {
	m, err := ReadMax(c, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	ch, ok := m.(Challenge)
	if !ok {
		return 0, 0, 0, ErrDenied
	}
	if err = Write(c, Hello{Version: Version, Hash: HashPassword(password, ch.Nonce), Codecs: codecs}); err != nil {
		return 0, 0, 0, err
	}
	if m, err = ReadMax(c, 64); err != nil {
		return 0, 0, 0, err
	}
	if o, ok := m.(OK); ok {
		if o.Codec == 0 { // old host: no codec byte means JPEG
			o.Codec = CodecJPEG
		}
		return o.W, o.H, o.Codec, nil
	}
	return 0, 0, 0, ErrDenied
}
