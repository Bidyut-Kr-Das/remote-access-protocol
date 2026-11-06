package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
	"testing/iotest"
)

func TestRoundTripAll(t *testing.T) {
	msgs := []Msg{
		Challenge{Nonce: [32]byte{1, 2, 3, 31: 9}},
		Hello{Version: 1, Channel: ChannelInput, Hash: [32]byte{7, 31: 8}},
		OK{W: 1920, H: 1080},
		Deny{},
		Frame{X: 1, Y: 2, W: 3, H: 4, Codec: 1, Data: []byte{9, 8, 7}},
		MouseMove{X: 100, Y: 200},
		MouseButton{Button: 1, Down: 1},
		MouseWheel{Delta: -120},
		Key{VK: 0x41, Down: 1},
		Release{},
		Ping{},
		Pong{},
	}
	for _, m := range msgs {
		var b bytes.Buffer
		if err := Write(&b, m); err != nil {
			t.Fatalf("%T write: %v", m, err)
		}
		got, err := Read(&b)
		if err != nil {
			t.Fatalf("%T read: %v", m, err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Errorf("%T: got %#v want %#v", m, got, m)
		}
	}
}

func TestReadOneByteAtATime(t *testing.T) {
	m := Frame{X: 1, Y: 2, W: 3, H: 4, Codec: 1, Data: []byte{9, 8, 7}}
	var b bytes.Buffer
	if err := Write(&b, m); err != nil {
		t.Fatal(err)
	}
	got, err := Read(iotest.OneByteReader(&b))
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestRejectLenZero(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte{0, 0, 0, 0})); !errors.Is(err, ErrOversize) {
		t.Fatalf("got %v", err)
	}
}

type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestRejectOversize(t *testing.T) {
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], MaxLen+1)
	cr := &countReader{r: bytes.NewReader(append(h[:], make([]byte, 64)...))}
	if _, err := Read(cr); !errors.Is(err, ErrOversize) {
		t.Fatalf("got %v", err)
	}
	if cr.n != 4 {
		t.Fatalf("consumed %d bytes, want 4", cr.n)
	}
}

func TestRejectUnknownType(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte{0, 0, 0, 1, 0x7F})); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("got %v", err)
	}
}

func TestHeaderLayout(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, MouseMove{X: 1, Y: 2}); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 5, 0x20, 0, 1, 0, 2}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("got % x want % x", b.Bytes(), want)
	}
}
