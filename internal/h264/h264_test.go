package h264

import (
	"image"
	"math/rand"
	"testing"
	"time"
)

// nalTypes lists the NAL unit types in an Annex B stream.
func nalTypes(b []byte) []int {
	var ts []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			ts = append(ts, int(b[i+3]&0x1f))
			i += 3
		}
	}
	return ts
}

func hasNAL(b []byte, t int) bool {
	for _, x := range nalTypes(b) {
		if x == t {
			return true
		}
	}
	return false
}

// movingRect draws a gradient with a white rectangle offset by frame i.
func movingRect(w, h, i int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.Pix[y*img.Stride+4*x:]
			p[0], p[1], p[2], p[3] = uint8(x*255/w), uint8(y*255/h), 96, 255
			if x >= 20+8*i && x < 120+8*i && y >= 40+4*i && y < 140+4*i {
				p[0], p[1], p[2] = 250, 250, 250
			}
		}
	}
	return img
}

func meanAbsErr(a, b *image.RGBA) [3]float64 {
	var s [3]float64
	for i := 0; i < len(a.Pix); i += 4 {
		for k := 0; k < 3; k++ {
			d := int(a.Pix[i+k]) - int(b.Pix[i+k])
			if d < 0 {
				d = -d
			}
			s[k] += float64(d)
		}
	}
	n := float64(len(a.Pix) / 4)
	return [3]float64{s[0] / n, s[1] / n, s[2] / n}
}

func TestRoundTrip(t *testing.T) {
	if !Available() {
		t.Skip("Media Foundation H.264 not available")
	}
	const maxMAE = 12
	for _, c := range []struct{ w, h int }{{640, 360}, {641, 361}} {
		enc, err := NewEncoder(c.w, c.h, 30, 4000)
		if err != nil {
			t.Fatal(err)
		}
		dec, err := NewDecoder()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			src := movingRect(c.w, c.h, i)
			t0 := time.Now()
			au, err := enc.Encode(src)
			te := time.Since(t0)
			if err != nil {
				t.Fatalf("%dx%d frame %d: encode: %v", c.w, c.h, i, err)
			}
			if len(au) == 0 {
				t.Fatalf("%dx%d frame %d: empty AU", c.w, c.h, i)
			}
			if i == 0 && !(hasNAL(au, 7) && hasNAL(au, 8) && hasNAL(au, 5)) {
				t.Fatalf("first AU lacks SPS/PPS/IDR: NALs %v", nalTypes(au))
			}
			t1 := time.Now()
			got, err := dec.Decode(au, c.w, c.h)
			td := time.Since(t1)
			if err != nil {
				t.Fatalf("%dx%d frame %d: decode: %v", c.w, c.h, i, err)
			}
			if got == nil {
				t.Fatalf("%dx%d frame %d: decoder buffered (no output for this input)", c.w, c.h, i)
			}
			if b := got.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
				t.Fatalf("decoded size %v, want %dx%d", b, c.w, c.h)
			}
			mae := meanAbsErr(src, got)
			t.Logf("%dx%d frame %d: enc %.2fms dec %.2fms AU %d B NALs %v MAE %.2f/%.2f/%.2f",
				c.w, c.h, i, ms(te), ms(td), len(au), nalTypes(au), mae[0], mae[1], mae[2])
			for k, m := range mae {
				if m >= maxMAE {
					t.Errorf("frame %d channel %d MAE %.2f >= %d", i, k, m, maxMAE)
				}
			}
		}
		enc.Close()
		enc.Close()
		dec.Close()
		dec.Close()
	}
}

func TestForceKeyFrame(t *testing.T) {
	if !Available() {
		t.Skip("Media Foundation H.264 not available")
	}
	enc, err := NewEncoder(320, 240, 30, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	for i := 0; i < 3; i++ {
		if _, err := enc.Encode(movingRect(320, 240, i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.ForceKeyFrame(); err != nil {
		t.Fatal(err)
	}
	au, err := enc.Encode(movingRect(320, 240, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !hasNAL(au, 5) {
		t.Fatalf("forced frame NALs %v, want IDR (5)", nalTypes(au))
	}
}

// TestDesktop720p measures a desktop-like frame: static text-ish blocks with a
// small change (a "typed" glyph) per frame.
func TestDesktop720p(t *testing.T) {
	if !Available() {
		t.Skip("Media Foundation H.264 not available")
	}
	const w, h, frames = 1280, 720, 30
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	glyph := func(gx, gy int, r *rand.Rand) {
		for y := 0; y < 12; y++ {
			for x := 0; x < 7; x++ {
				if r.Intn(3) == 0 {
					p := img.Pix[(gy+y)*img.Stride+4*(gx+x):]
					p[0], p[1], p[2] = 20, 20, 20
				}
			}
		}
	}
	r := rand.New(rand.NewSource(1))
	for gy := 8; gy+12 < h-40; gy += 18 {
		for gx := 8; gx+7 < w-8; gx += 8 {
			if r.Intn(6) != 0 {
				glyph(gx, gy, r)
			}
		}
	}
	enc, err := NewEncoder(w, h, 30, 4000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var encTot, decTot time.Duration
	var pTot int
	for i := 0; i < frames; i++ {
		if i > 0 {
			glyph(8+8*i, h-30, r)
		}
		t0 := time.Now()
		au, err := enc.Encode(img)
		te := time.Since(t0)
		if err != nil {
			t.Fatal(err)
		}
		t1 := time.Now()
		got, err := dec.Decode(au, w, h)
		td := time.Since(t1)
		if err != nil || got == nil {
			t.Fatalf("frame %d: decode %v (nil=%v)", i, err, got == nil)
		}
		if i == 0 {
			mae := meanAbsErr(img, got)
			t.Logf("720p IDR: enc %.2fms dec %.2fms AU %.1f KB MAE %.2f/%.2f/%.2f", ms(te), ms(td), float64(len(au))/1024, mae[0], mae[1], mae[2])
			continue
		}
		encTot += te
		decTot += td
		pTot += len(au)
	}
	n := float64(frames - 1)
	t.Logf("720p P frames: avg enc %.2fms dec %.2fms AU %.2f KB", ms(encTot)/n, ms(decTot)/n, float64(pTot)/n/1024)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func TestCloseNil(t *testing.T) {
	var e *Encoder
	e.Close()
	var d *Decoder
	d.Close()
}
