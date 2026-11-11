package capture

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestEncodeProducesDecodableJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	f, err := Encode(img, 70)
	if err != nil {
		t.Fatal(err)
	}
	if f.X != 0 || f.Y != 0 || f.W != 64 || f.H != 48 || f.Codec != 1 {
		t.Fatalf("bad frame header: %+v", f)
	}
	d, err := jpeg.Decode(bytes.NewReader(f.Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := d.Bounds(); b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("bad bounds %v", b)
	}
}

func TestEncodeRejectsOversize(t *testing.T) {
	if _, err := Encode(image.NewRGBA(image.Rect(0, 0, 65536, 1)), 70); err == nil {
		t.Fatal("expected error for width > 65535")
	}
}

func TestDiffRect(t *testing.T) {
	const w, h = 50, 40
	mk := func(pts ...image.Point) *image.RGBA {
		m := image.NewRGBA(image.Rect(0, 0, w, h))
		for _, p := range pts {
			m.Pix[p.Y*m.Stride+4*p.X] = 9
		}
		return m
	}
	for _, c := range []struct {
		name string
		b    *image.RGBA
		want image.Rectangle
	}{
		{"identical", mk(), image.Rectangle{}},
		{"single", mk(image.Pt(20, 18)), image.Rect(16, 16, 32, 32)},
		{"edge", mk(image.Pt(w-1, h-1)), image.Rect(48&^15, 32, w, h)},
		{"full", mk(image.Pt(0, 0), image.Pt(w-1, h-1)), image.Rect(0, 0, w, h)},
	} {
		if got := DiffRect(mk(), c.b); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestEncodeSubImageHeader(t *testing.T) {
	f, err := Encode(image.NewRGBA(image.Rect(0, 0, 64, 48)).SubImage(image.Rect(16, 32, 48, 48)), 70)
	if err != nil || f.X != 16 || f.Y != 32 || f.W != 32 || f.H != 16 {
		t.Fatalf("%+v %v", f, err)
	}
}
