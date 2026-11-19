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

func TestDiffRects(t *testing.T) {
	const w, h = 200, 150
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
		want []image.Rectangle
	}{
		{"identical", mk(), nil},
		{"single", mk(image.Pt(10, 10)), []image.Rectangle{image.Rect(0, 0, 64, 64)}},
		{"bottom edge clamped", mk(image.Pt(10, 10), image.Pt(190, 140)),
			[]image.Rectangle{image.Rect(0, 0, 64, 64), image.Rect(128, 128, 192, 150)}},
		{"right edge clamped", mk(image.Pt(195, 10)), []image.Rectangle{image.Rect(192, 0, 200, 64)}},
		{"adjacent merged", mk(image.Pt(60, 10), image.Pt(70, 10)), []image.Rectangle{image.Rect(0, 0, 128, 64)}},
	} {
		got := DiffRects(mk(), c.b, 64, 32)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestDiffRectsFallsBackOnRectCount(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 640, 512)), image.NewRGBA(image.Rect(0, 0, 640, 512))
	for ty := 0; ty < 8; ty++ { // checkerboard: 40 single-tile runs, exactly 50% area
		for tx := ty % 2; tx < 10; tx += 2 {
			b.Pix[(ty*64+5)*b.Stride+4*(tx*64+5)] = 9
		}
	}
	if got := DiffRects(a, b, 64, 32); len(got) != 1 || got[0] != DiffRect(a, b) {
		t.Fatalf("got %v want [%v]", got, DiffRect(a, b))
	}
}

func TestDiffRectsFallsBackWhenMostlyDirty(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 200, 150)), image.NewRGBA(image.Rect(0, 0, 200, 150))
	for _, y := range []int{10, 70} { // two full tile rows: 25600 of 30000 px
		for _, x := range []int{10, 70, 130, 195} {
			b.Pix[y*b.Stride+4*x] = 9
		}
	}
	if got := DiffRects(a, b, 64, 32); len(got) != 1 || got[0] != DiffRect(a, b) {
		t.Fatalf("got %v want [%v]", got, DiffRect(a, b))
	}
}

func TestEncodeToReusesBuffer(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	var caps []int
	for range 2 {
		f, err := EncodeTo(&buf, img, 70)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(f.Data)); err != nil {
			t.Fatalf("decode: %v", err)
		}
		caps = append(caps, buf.Cap())
	}
	if caps[0] != caps[1] {
		t.Errorf("buf.Cap changed: %d -> %d", caps[0], caps[1])
	}
}
