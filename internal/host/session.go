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

// Accept runs the handshake on c. It returns (sess, true) only from the call that completes a pair.
// Denied or timed-out conns are closed.
func (m *Manager) Accept(c net.Conn) (*Session, bool) {
	ip, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		c.Close()
		return nil, false
	}
	var done *Session
	c.SetReadDeadline(time.Now().Add(m.pairTimeout)) // silent peer must not hold us forever
	_, err = protocol.ServerHandshake(c, ip, m.password, m.lim, func(channel byte) (uint16, uint16, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active || (channel != protocol.ChannelScreen && channel != protocol.ChannelInput) {
			return 0, 0, false
		}
		if m.pending == nil {
			s := &Session{}
			*m.slot(s, channel) = c
			m.pending = s
			m.timer = time.AfterFunc(m.pairTimeout, func() { m.expire(s) })
		} else {
			slot := m.slot(m.pending, channel)
			if *slot != nil {
				return 0, 0, false
			}
			*slot = c
			done = m.pending
			m.clearPending()
			m.active = true
		}
		return m.w, m.h, true
	})
	if err == nil {
		c.SetReadDeadline(time.Time{})
		if done != nil {
			return done, true
		}
		return nil, false
	}
	m.rollback(c, done)
	c.Close()
	return nil, false
}

// rollback undoes a registration whose handshake failed after accept said yes.
func (m *Manager) rollback(c net.Conn, done *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if done != nil {
		m.active = false
	} else if m.pending != nil && (m.pending.Screen == c || m.pending.Input == c) {
		m.clearPending()
	}
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
