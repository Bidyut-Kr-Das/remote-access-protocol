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

// join dials ln, runs Accept (server) and ClientHandshake (client) concurrently, both offering JPEG only.
func join(t *testing.T, m *Manager, ln net.Listener) (ok bool, cerr error) {
	t.Helper()
	_, _, ok, cerr = joinCodecs(t, m, ln, protocol.CodecsJPEG, protocol.CodecsJPEG)
	return
}

// joinCodecs is join with explicit host and viewer codec masks; it returns the codec each side got.
func joinCodecs(t *testing.T, m *Manager, ln net.Listener, hostMask, viewMask byte) (hostCodec, viewCodec byte, ok bool, cerr error) {
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
		hostCodec, ok = m.Accept(sc, hostMask)
	}()
	_, _, viewCodec, cerr = protocol.ClientHandshake(c, pw, viewMask)
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
	if _, ok := m.Accept(sc, protocol.CodecsJPEG); ok {
		t.Fatal("silent peer accepted")
	}
	if ok, err := join(t, m, ln); !ok || err != nil { // slot was not claimed
		t.Fatalf("after timeout: %v %v", ok, err)
	}
}

func TestAcceptNegotiatesCodec(t *testing.T) {
	both := protocol.CodecsJPEG | protocol.CodecsH264
	for _, c := range []struct {
		name           string
		hostMask, view byte
		want           byte
	}{
		{"both H264", both, both, protocol.CodecH264},
		{"host JPEG only (-codec jpeg or no MF)", protocol.CodecsJPEG, both, protocol.CodecJPEG},
		{"viewer JPEG only", both, protocol.CodecsJPEG, protocol.CodecJPEG},
		{"v2.0.0 viewer (no codec byte)", both, 0, protocol.CodecJPEG},
	} {
		m, ln := setup(t, time.Second)
		hc, vc, ok, err := joinCodecs(t, m, ln, c.hostMask, c.view)
		if !ok || err != nil || hc != c.want || vc != c.want {
			t.Errorf("%s: host %d viewer %d ok=%v err=%v, want %d", c.name, hc, vc, ok, err, c.want)
		}
	}
}
