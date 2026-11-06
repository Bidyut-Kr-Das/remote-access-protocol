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
