package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"testing"
	"time"

	"rap/internal/h264"
	"rap/internal/protocol"
)

// testGame returns a headless game whose acks are counted from the other end of a pipe.
func testGame(t *testing.T) (*game, func() int) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	acks := make(chan struct{}, 16)
	go func() {
		for {
			m, err := protocol.Read(b)
			if err != nil {
				return
			}
			if _, ok := m.(protocol.Ack); ok {
				acks <- struct{}{}
			}
		}
	}()
	g := &game{conn: a, backing: image.NewRGBA(image.Rect(0, 0, 1, 1))}
	t.Cleanup(func() { g.dec.Close() })
	return g, func() int { // acks so far; waits briefly for the reader goroutine to pass one on
		n := 0
		for {
			select {
			case <-acks:
				n++
			case <-time.After(100 * time.Millisecond):
				return n
			}
		}
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

func near(a, b uint8) bool { d := int(a) - int(b); return d > -16 && d < 16 }

func TestFrameCodecs(t *testing.T) {
	const w, h = 64, 48
	want := color.RGBA{200, 40, 90, 255}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, solid(w, h, want), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name    string
		codec   byte
		data    func(t *testing.T) []byte
		painted bool
	}{
		{"jpeg", protocol.CodecJPEG, func(*testing.T) []byte { return jb.Bytes() }, true},
		{"h264", protocol.CodecH264, func(t *testing.T) []byte {
			if !h264.Available() {
				t.Skip("no Media Foundation H.264")
			}
			e, err := h264.NewEncoder(w, h, 30, 2000)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			au, err := e.Encode(solid(w, h, want))
			if err != nil || len(au) == 0 {
				t.Fatalf("encode: %d bytes, %v", len(au), err)
			}
			return au
		}, true},
		{"unknown codec", 9, func(*testing.T) []byte { return []byte{1, 2, 3} }, false},
		{"bad jpeg", protocol.CodecJPEG, func(*testing.T) []byte { return []byte{1, 2, 3} }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, acks := testGame(t)
			g.frame(protocol.Frame{W: w, H: h, Codec: c.codec, Data: c.data(t)})
			if n := acks(); n != 1 {
				t.Fatalf("acks = %d, want 1", n)
			}
			if g.gotFrame != c.painted {
				t.Fatalf("gotFrame = %v, want %v", g.gotFrame, c.painted)
			}
			if !c.painted {
				return
			}
			if g.backing.Rect != image.Rect(0, 0, w, h) || !g.full || g.dirty != g.backing.Rect {
				t.Fatalf("backing %v full %v dirty %v", g.backing.Rect, g.full, g.dirty)
			}
			if p := g.backing.RGBAAt(w/2, h/2); !near(p.R, want.R) || !near(p.G, want.G) || !near(p.B, want.B) {
				t.Fatalf("pixel %v, want ~%v", p, want)
			}
		})
	}
}
