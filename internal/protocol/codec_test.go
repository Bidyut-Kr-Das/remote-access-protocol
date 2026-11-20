package protocol

import (
	"bytes"
	"net"
	"reflect"
	"testing"
)

func TestHelloOKCodecByte(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    Msg
		n    int // payload length on the wire
	}{
		{"hello old", Hello{Version: 2, Hash: [32]byte{7}}, 33},
		{"hello jpeg", Hello{Version: 2, Hash: [32]byte{7}, Codecs: CodecsJPEG}, 34},
		{"hello both", Hello{Version: 2, Hash: [32]byte{7}, Codecs: CodecsJPEG | CodecsH264}, 34},
		{"ok old", OK{W: 1920, H: 1080}, 4},
		{"ok jpeg", OK{W: 1920, H: 1080, Codec: CodecJPEG}, 5},
		{"ok h264", OK{W: 1920, H: 1080, Codec: CodecH264}, 5},
	} {
		if got := len(tc.m.Marshal()); got != tc.n {
			t.Errorf("%s: payload len %d want %d", tc.name, got, tc.n)
		}
		var b bytes.Buffer
		if err := Write(&b, tc.m); err != nil {
			t.Fatal(err)
		}
		got, err := ReadMax(&b, 64)
		if err != nil || !reflect.DeepEqual(got, tc.m) {
			t.Errorf("%s: got %#v, %v", tc.name, got, err)
		}
	}
}

func TestChooseCodec(t *testing.T) {
	for _, tc := range []struct {
		name         string
		viewer, host byte
		want         byte
	}{
		{"both h264", CodecsJPEG | CodecsH264, CodecsJPEG | CodecsH264, CodecH264},
		{"viewer jpeg only", CodecsJPEG, CodecsJPEG | CodecsH264, CodecJPEG},
		{"host jpeg only", CodecsJPEG | CodecsH264, CodecsJPEG, CodecJPEG},
		{"old viewer", 0, CodecsJPEG | CodecsH264, CodecJPEG},
		{"garbage", 0xfc, 0xfc, CodecJPEG},
	} {
		if got := chooseCodec(tc.viewer, tc.host); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

// New viewer and new host negotiate; JPEG-only on either side yields JPEG.
func TestHandshakeNegotiates(t *testing.T) {
	both := CodecsJPEG | CodecsH264
	for _, tc := range []struct {
		name         string
		viewer, host byte
		want         byte
	}{
		{"both h264", both, both, CodecH264},
		{"jpeg only", CodecsJPEG, CodecsJPEG, CodecJPEG},
		{"host jpeg", both, CodecsJPEG, CodecJPEG},
	} {
		s, c := net.Pipe()
		res := make(chan byte, 1)
		go func() {
			codec, err := ServerHandshake(s, "1.1.1.1", pw, newLim(), tc.host, okAccept)
			if err != nil {
				t.Errorf("%s server: %v", tc.name, err)
			}
			res <- codec
		}()
		w, h, codec, err := ClientHandshake(c, pw, tc.viewer)
		if err != nil || w != 1920 || h != 1080 || codec != tc.want {
			t.Errorf("%s client: %d %d %d %v", tc.name, w, h, codec, err)
		}
		if got := <-res; got != tc.want {
			t.Errorf("%s server codec %d want %d", tc.name, got, tc.want)
		}
		s.Close()
		c.Close()
	}
}

// Old viewer: 33-byte Hello passes the Version check; host replies with the exact 4-byte OK and picks JPEG.
func TestHandshakeOldViewer(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	res := make(chan byte, 1)
	go func() {
		codec, err := ServerHandshake(s, "1.1.1.1", pw, newLim(), CodecsJPEG|CodecsH264, okAccept)
		if err != nil {
			t.Errorf("server: %v", err)
		}
		res <- codec
	}()
	m, err := Read(c)
	if err != nil {
		t.Fatal(err)
	}
	h := HashPassword(pw, m.(Challenge).Nonce)
	if _, err := c.Write(append([]byte{0, 0, 0, 34, TypeHello, Version}, h[:]...)); err != nil {
		t.Fatal(err)
	}
	var raw [9]byte
	if _, err := c.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0, 0, 0, 5, TypeOK, 0x07, 0x80, 0x04, 0x38}; !bytes.Equal(raw[:], want) {
		t.Fatalf("OK bytes % x want % x", raw, want)
	}
	if got := <-res; got != CodecJPEG {
		t.Fatalf("server codec %d", got)
	}
}

// Old host: replies with a 4-byte OK; new viewer reads it as JPEG.
func TestHandshakeOldHost(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	go func() {
		Write(s, Challenge{})
		hello, err := ReadMax(s, 64)
		if err != nil || hello.(Hello).Version != Version {
			t.Errorf("hello %#v %v", hello, err)
		}
		s.Write([]byte{0, 0, 0, 5, TypeOK, 0, 100, 0, 50})
	}()
	w, h, codec, err := ClientHandshake(c, pw, CodecsJPEG|CodecsH264)
	if err != nil || w != 100 || h != 50 || codec != CodecJPEG {
		t.Fatalf("got %d %d %d %v", w, h, codec, err)
	}
}
