// Package host pairs the screen and input connections of one remote session.
package host

import (
	"net"
	"sync"
	"time"

	"rdp-go/internal/protocol"
)

type Session struct{ Screen, Input net.Conn }

type Manager struct {
	password    string
	w, h        uint16
	lim         *protocol.Limiter
	pairTimeout time.Duration

	mu      sync.Mutex
	pending *Session
	timer   *time.Timer
	active  bool
}

func NewManager(password string, w, h uint16, lim *protocol.Limiter, pairTimeout time.Duration) *Manager {
	return &Manager{password: password, w: w, h: h, lim: lim, pairTimeout: pairTimeout}
}

func (m *Manager) slot(s *Session, ch byte) *net.Conn {
	if ch == protocol.ChannelScreen {
		return &s.Screen
	}
	return &s.Input
}

// clearPending drops pending and stops its timer. Caller holds mu.
func (m *Manager) clearPending() {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.pending, m.timer = nil, nil
}

// ok reports whether channel may join now. Caller holds mu.
func (m *Manager) ok(channel byte) bool {
	if m.active || (channel != protocol.ChannelScreen && channel != protocol.ChannelInput) {
		return false
	}
	return m.pending == nil || *m.slot(m.pending, channel) == nil
}

// Accept runs the handshake on c. It returns (sess, true) only from the call that completes a pair.
// Denied or timed-out conns are closed. A conn joins pending only after its OK was written.
func (m *Manager) Accept(c net.Conn) (*Session, bool) {
	ip, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		c.Close()
		return nil, false
	}
	c.SetReadDeadline(time.Now().Add(m.pairTimeout)) // silent peer must not hold us forever
	ch, err := protocol.ServerHandshake(c, ip, m.password, m.lim, func(channel byte) (uint16, uint16, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.w, m.h, m.ok(channel)
	})
	if err != nil {
		c.Close()
		return nil, false
	}
	c.SetReadDeadline(time.Time{})

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ok(ch) { // lost a race after the OK was sent
		c.Close()
		return nil, false
	}
	if m.pending == nil {
		s := &Session{}
		*m.slot(s, ch) = c
		m.pending = s
		m.timer = time.AfterFunc(m.pairTimeout, func() { m.expire(s) })
		return nil, false
	}
	*m.slot(m.pending, ch) = c
	done := m.pending
	m.clearPending()
	m.active = true
	return done, true
}

func (m *Manager) expire(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != s {
		return
	}
	m.pending, m.timer = nil, nil
	if s.Screen != nil {
		s.Screen.Close()
	}
	if s.Input != nil {
		s.Input.Close()
	}
}

// End clears the active session so a new one may start.
func (m *Manager) End() {
	m.mu.Lock()
	m.active = false
	m.mu.Unlock()
}
