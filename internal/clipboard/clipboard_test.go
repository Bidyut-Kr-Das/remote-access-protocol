package clipboard

import "testing"

// fake clipboard: Set bumps the sequence like Windows does.
type fake struct {
	text string
	seq  uint32
}

func (f *fake) syncer() *Syncer {
	return &Syncer{
		Get: func() (string, error) { return f.text, nil },
		Set: func(t string) error { f.text = t; f.seq++; return nil },
		Seq: func() uint32 { return f.seq },
	}
}

func TestPollReportsLocalChangeOnce(t *testing.T) {
	f := &fake{}
	s := f.syncer()
	if _, ok := s.poll(); ok {
		t.Fatal("reported change with no change")
	}
	f.text, f.seq = "a", 1
	if got, ok := s.poll(); !ok || got != "a" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := s.poll(); ok {
		t.Fatal("reported same text twice")
	}
	f.seq++ // seq bump with same text (e.g. app re-set it): still no resend
	if _, ok := s.poll(); ok {
		t.Fatal("resent unchanged text")
	}
}

func TestApplyDoesNotEcho(t *testing.T) {
	f := &fake{}
	s := f.syncer()
	s.Apply("from peer")
	if f.text != "from peer" {
		t.Fatalf("clipboard not set: %q", f.text)
	}
	if got, ok := s.poll(); ok {
		t.Fatalf("echoed %q back", got)
	}
	f.text, f.seq = "local", f.seq+1
	if got, ok := s.poll(); !ok || got != "local" {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestPollSkipsEmptyAndOversize(t *testing.T) {
	f := &fake{text: "", seq: 1}
	s := f.syncer()
	if _, ok := s.poll(); ok {
		t.Fatal("sent empty")
	}
	f.text, f.seq = string(make([]byte, 256<<10+1)), 2
	if _, ok := s.poll(); ok {
		t.Fatal("sent oversize")
	}
}
