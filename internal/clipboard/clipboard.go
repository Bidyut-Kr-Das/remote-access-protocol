// Package clipboard syncs the local text clipboard with a remote peer.
package clipboard

import (
	"sync"
	"time"

	"rdp-go/internal/protocol"
)

// Syncer polls the local clipboard and reports changes; Apply sets it from the peer
// without echoing that text back.
type Syncer struct {
	Get func() (string, error)
	Set func(string) error
	Seq func() uint32 // cheap change counter, so Get only runs on real change

	mu   sync.Mutex
	last string // last text seen locally or applied from peer
	seq  uint32
}

func New() *Syncer { return &Syncer{Get: get, Set: set, Seq: seq} }

// Run polls until done is closed, calling send for each new local text.
func (s *Syncer) Run(done <-chan struct{}, send func(string)) {
	s.seq = s.Seq()
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		if text, ok := s.poll(); ok {
			send(text)
		}
	}
}

func (s *Syncer) poll() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.Seq(); n == s.seq {
		return "", false
	} else {
		s.seq = n
	}
	text, err := s.Get()
	if err != nil || text == "" || text == s.last || len(text) > protocol.MaxClipboard {
		return "", false
	}
	s.last = text
	return text, true
}

// Apply sets the local clipboard from the peer.
func (s *Syncer) Apply(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text == s.last || s.Set(text) != nil {
		return
	}
	s.last = text
	s.seq = s.Seq() // our own Set bumped it; don't treat that as a local change
}
