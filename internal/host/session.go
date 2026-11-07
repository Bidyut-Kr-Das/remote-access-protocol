// Package host admits one remote session at a time.
package host

import (
	"net"
	"sync"
	"time"

	"rdp-go/internal/protocol"
)

type Manager struct {
	password string
	w, h     uint16
	lim      *protocol.Limiter
	timeout  time.Duration // handshake read deadline

	mu     sync.Mutex
	active bool
}

func NewManager(password string, w, h uint16, lim *protocol.Limiter, timeout time.Duration) *Manager {
	return &Manager{password: password, w: w, h: h, lim: lim, timeout: timeout}
}

// Accept runs the handshake on c and claims the session slot. Denied or timed-out conns are closed.
func (m *Manager) Accept(c net.Conn) bool {
	ip, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		c.Close()
		return false
	}
	c.SetReadDeadline(time.Now().Add(m.timeout)) // silent peer must not hold us forever
	claimed := false
	err = protocol.ServerHandshake(c, ip, m.password, m.lim, func() (uint16, uint16, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active {
			return 0, 0, false
		}
		m.active, claimed = true, true
		return m.w, m.h, true
	})
	if err != nil {
		c.Close()
		if claimed { // OK write failed after the slot was taken
			m.End()
		}
		return false
	}
	c.SetReadDeadline(time.Time{})
	return true
}

// End clears the active session so a new one may start.
func (m *Manager) End() {
	m.mu.Lock()
	m.active = false
	m.mu.Unlock()
}
