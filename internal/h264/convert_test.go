package h264

import (
	"image"
	"image/color"
	"testing"
)

func TestPad16(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{1, 16}, {16, 16}, {17, 32}, {360, 368}, {361, 368}, {640, 640}, {641, 656}, {720, 720},
	} {
		if got := pad16(c.in); got != c.want {
			t.Errorf("pad16(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNV12RoundTrip(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		col  color.RGBA
	}{
		{"black", 4, 4, color.RGBA{0, 0, 0, 255}},
		{"white", 4, 4, color.RGBA{255, 255, 255, 255}},
		{"red odd", 5, 3, color.RGBA{255, 0, 0, 255}},
		{"green", 2, 2, color.RGBA{0, 255, 0, 255}},
		{"blue odd", 17, 9, color.RGBA{0, 0, 255, 255}},
		{"gray", 8, 6, color.RGBA{128, 128, 128, 255}},
	} {
		img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.col.R, c.col.G, c.col.B, 255
		}
		pw, ph := pad16(c.w), pad16(c.h)
		buf := make([]byte, pw*ph*3/2)
		rgbaToNV12(img, pw, ph, buf)
		out := nv12ToRGBA(buf, pw, ph, c.w, c.h)
		if b := out.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Fatalf("%s: bounds %v", c.name, b)
		}
		for i := 0; i < len(out.Pix); i += 4 {
			for k, want := range []uint8{c.col.R, c.col.G, c.col.B, 255} {
				if d := int(out.Pix[i+k]) - int(want); d < -3 || d > 3 {
					t.Fatalf("%s: pixel %d chan %d = %d, want %d", c.name, i/4, k, out.Pix[i+k], want)
				}
			}
		}
	}
}

func TestRGBAToNV12PadsByEdgeReplication(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 1))
	for i := range img.Pix {
		img.Pix[i] = 255 // white
	}
	buf := make([]byte, 16*16*3/2)
	rgbaToNV12(img, 16, 16, buf)
	for _, c := range []struct{ x, y int }{{0, 0}, {2, 0}, {15, 0}, {0, 15}, {15, 15}} {
		if y := buf[c.y*16+c.x]; y != 235 {
			t.Errorf("Y(%d,%d) = %d, want 235", c.x, c.y, y)
		}
	}
	if u, v := buf[16*16], buf[16*16+1]; u != 128 || v != 128 {
		t.Errorf("UV = %d,%d, want 128,128", u, v)
	}
}

func TestNV12ToRGBACropsWithStride(t *testing.T) {
	// 4x2 visible inside a buffer with stride 8 and 4 Y rows; mark column 0 bright.
	const stride, rows = 8, 4
	buf := make([]byte, stride*rows*3/2)
	for i := range buf {
		buf[i] = 128
	}
	for y := 0; y < rows; y++ {
		buf[y*stride] = 235
		buf[y*stride+1] = 16
	}
	out := nv12ToRGBA(buf, stride, rows, 4, 2)
	if b := out.Bounds(); b.Dx() != 4 || b.Dy() != 2 {
		t.Fatalf("bounds %v", b)
	}
	for _, c := range []struct {
		x, y int
		want uint8
	}{{0, 0, 255}, {1, 0, 0}, {0, 1, 255}, {1, 1, 0}} {
		if g := out.Pix[c.y*out.Stride+c.x*4+1]; g != c.want {
			t.Errorf("G(%d,%d) = %d, want %d", c.x, c.y, g, c.want)
		}
	}
}
