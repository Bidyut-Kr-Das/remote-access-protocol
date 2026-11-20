package capture

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
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

func TestGrabFallsBackToGDI(t *testing.T) {
	type step struct {
		at    time.Duration // fake clock offset
		ctor  error         // result of newDXGIFn if Grab calls it
		frame error         // result of the DXGI frame copy if Grab calls it
		want  string        // "dxgi" or "gdi"
		inits int           // total newDXGIFn calls after this step
		rels  int           // total releases after this step
	}
	fail := errors.New("boom")
	for _, c := range []struct {
		name  string
		steps []step
	}{
		{"init fails, retry throttled to 2s", []step{
			{0, fail, nil, "gdi", 1, 0},
			{time.Second, nil, nil, "gdi", 1, 0},
			{2 * time.Second, nil, nil, "dxgi", 2, 0},
		}},
		{"bounds mismatch stops retrying", []step{
			{0, errBoundsMismatch, nil, "gdi", 1, 0},
			{time.Minute, nil, nil, "gdi", 1, 0},
		}},
		{"still screen serves cached frame", []step{
			{0, nil, nil, "dxgi", 1, 0},
			{time.Second, nil, errNoNewFrame, "dxgi", 1, 0},
		}},
		{"no frame yet uses GDI and keeps DXGI", []step{
			{0, nil, errNoNewFrame, "gdi", 1, 0},
			{time.Millisecond, nil, nil, "dxgi", 1, 0},
		}},
		{"frame error releases, GDI until 2s", []step{
			{0, nil, nil, "dxgi", 1, 0},
			{time.Second, nil, fail, "gdi", 1, 1},
			{2 * time.Second, nil, nil, "gdi", 1, 1},
			{3 * time.Second, nil, errNoNewFrame, "gdi", 2, 1}, // re-init must not serve the stale pre-loss frame
			{3*time.Second + time.Millisecond, nil, nil, "dxgi", 2, 1},
		}},
	} {
		t0 := time.Unix(1000, 0)
		var clock time.Time
		var cur step
		inits, rels := 0, 0
		rect := image.Rect(0, 0, 8, 4)
		gdiImg := image.NewRGBA(rect)
		oldNew, oldGDI, oldNow := newDXGIFn, gdiGrab, now
		newDXGIFn = func() (*dxgiGrabber, error) {
			inits++
			if cur.ctor != nil {
				return nil, cur.ctor
			}
			return &dxgiGrabber{rect: rect,
				frame: func(dst *image.RGBA) error {
					if cur.frame == nil {
						dst.Pix[0] = 7
					}
					return cur.frame
				},
				close: func() { rels++ }}, nil
		}
		gdiGrab = func() (*image.RGBA, error) { return gdiImg, nil }
		now = func() time.Time { return clock }
		dxgi, dxgiOK, dxgiRetry, dxgiBuf, dxgiNever, dxgiFailLogged = nil, false, time.Time{}, nil, false, false
		for i, s := range c.steps {
			cur, clock = s, t0.Add(s.at)
			img, err := Grab()
			got := "dxgi"
			if img == gdiImg {
				got = "gdi"
			} else if err != nil || img.Rect != rect || img.Pix[0] != 7 {
				t.Fatalf("%s step %d: bad DXGI image %v err %v", c.name, i, img, err)
			}
			if got != s.want || inits != s.inits || rels != s.rels {
				t.Errorf("%s step %d: got %s inits %d rels %d, want %s %d %d", c.name, i, got, inits, rels, s.want, s.inits, s.rels)
			}
		}
		newDXGIFn, gdiGrab, now = oldNew, oldGDI, oldNow
		dxgi, dxgiOK, dxgiRetry, dxgiBuf, dxgiNever, dxgiFailLogged = nil, false, time.Time{}, nil, false, false
	}
}
