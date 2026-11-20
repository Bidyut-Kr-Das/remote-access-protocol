// Package capture grabs the primary display and encodes it as a protocol FRAME.
package capture

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"log"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
	xdraw "golang.org/x/image/draw"

	"rap/internal/protocol"
)

// dims converts bounds to wire dimensions, rejecting empty or >65535 sizes.
func dims(b image.Rectangle) (w, h uint16, err error) {
	if b.Empty() {
		return 0, 0, errors.New("capture: empty bounds (no display?)")
	}
	if b.Dx() > 0xFFFF || b.Dy() > 0xFFFF {
		return 0, 0, errors.New("capture: dimensions exceed 65535")
	}
	return uint16(b.Dx()), uint16(b.Dy()), nil
}

// ScreenSize returns the primary display size.
func ScreenSize() (w, h uint16, err error) {
	return dims(screenshot.GetDisplayBounds(0))
}

// dxgiGrabber is a DXGI desktop duplicator for display 0 (dxgi_windows.go fills it in).
type dxgiGrabber struct {
	rect  image.Rectangle
	frame func(dst *image.RGBA) error // errNoNewFrame when nothing changed since the last call
	close func()
}

func (g *dxgiGrabber) grab(dst *image.RGBA) error { return g.frame(dst) }

var errNoNewFrame = errors.New("capture: no new DXGI frame")

const dxgiRetryEvery = 2 * time.Second

// Test seams.
var (
	newDXGIFn = newDXGI
	gdiGrab   = func() (*image.RGBA, error) { return screenshot.CaptureDisplay(0) }
	now       = time.Now
)

var (
	dxgiMu    sync.Mutex
	dxgi      *dxgiGrabber
	dxgiBuf   *image.RGBA // holds the last DXGI frame
	dxgiOK    bool        // dxgiBuf holds a frame from the current dxgi
	dxgiRetry time.Time   // no DXGI init before this
)

// Grab captures the primary display, through DXGI Desktop Duplication when it works and GDI
// otherwise (UAC/secure desktop, lock screen, unsupported GPU), retrying DXGI at most every 2 s.
// A DXGI result is the same reused buffer every call: valid only until the next Grab.
func Grab() (*image.RGBA, error) {
	dxgiMu.Lock()
	defer dxgiMu.Unlock()
	if dxgi == nil && !now().Before(dxgiRetry) {
		g, err := newDXGIFn()
		if err != nil {
			log.Printf("capture: DXGI unavailable, using GDI: %v", err)
			dxgiRetry = now().Add(dxgiRetryEvery)
		} else {
			log.Print("capture: using DXGI")
			dxgi, dxgiOK = g, false
			if dxgiBuf == nil || dxgiBuf.Rect != g.rect {
				dxgiBuf = image.NewRGBA(g.rect) // GetImage needs a buffer of the exact output size
			}
		}
	}
	if dxgi != nil {
		switch err := dxgi.grab(dxgiBuf); {
		case err == nil:
			dxgiOK = true
			return dxgiBuf, nil
		case errors.Is(err, errNoNewFrame):
			if dxgiOK { // still screen: dxgiBuf already holds the current image
				return dxgiBuf, nil
			}
		default:
			log.Printf("capture: DXGI lost, using GDI: %v", err)
			dxgi.close()
			dxgi, dxgiRetry = nil, now().Add(dxgiRetryEvery)
		}
	}
	return gdiGrab()
}

// Encode JPEG-encodes img as a frame (Codec 1) placed at img's bounds (a SubImage keeps its Min).
func Encode(img image.Image, quality int) (*protocol.Frame, error) {
	return EncodeTo(new(bytes.Buffer), img, quality)
}

// EncodeTo is Encode into a caller-owned buf (reset first). Frame.Data aliases buf.Bytes(), so it
// is valid only until buf is next used.
func EncodeTo(buf *bytes.Buffer, img image.Image, quality int) (*protocol.Frame, error) {
	w, h, err := dims(img.Bounds())
	if err != nil {
		return nil, err
	}
	buf.Reset()
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	b := img.Bounds()
	return &protocol.Frame{X: uint16(b.Min.X), Y: uint16(b.Min.Y), W: w, H: h, Codec: 1, Data: buf.Bytes()}, nil
}

// Scale returns img scaled to width maxW (aspect kept) into *dst, reallocating dst only when the
// size changes. Returns img itself when it is already at most maxW wide.
func Scale(img *image.RGBA, maxW int, dst **image.RGBA) *image.RGBA {
	b := img.Bounds()
	if b.Dx() <= maxW {
		return img
	}
	r := image.Rect(0, 0, maxW, max(b.Dy()*maxW/b.Dx(), 1))
	if *dst == nil || (*dst).Rect != r {
		*dst = image.NewRGBA(r)
	}
	xdraw.ApproxBiLinear.Scale(*dst, r, img, b, xdraw.Src, nil)
	return *dst
}

// DiffRect returns the bounding box of pixels that differ between a and b (same bounds, Min 0,0),
// grown to 16-px alignment and clamped to the image. Empty if identical.
func DiffRect(a, b *image.RGBA) image.Rectangle {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	row := func(m *image.RGBA, y int) []byte { return m.Pix[y*m.Stride : y*m.Stride+4*w] }
	top := 0
	for top < h && bytes.Equal(row(a, top), row(b, top)) {
		top++
	}
	if top == h {
		return image.Rectangle{}
	}
	bot := h - 1
	for bytes.Equal(row(a, bot), row(b, bot)) {
		bot--
	}
	x0, x1 := w, -1
	for y := top; y <= bot; y++ {
		ra, rb := row(a, y), row(b, y)
		for x := 0; x < x0 && x < w; x++ { // ponytail: per-pixel scan, only left of the current min
			if string(ra[4*x:4*x+4]) != string(rb[4*x:4*x+4]) {
				x0 = x
				break
			}
		}
		for x := w - 1; x > x1; x-- {
			if string(ra[4*x:4*x+4]) != string(rb[4*x:4*x+4]) {
				x1 = x
				break
			}
		}
	}
	return image.Rect(x0&^15, top&^15, min((x1+16)&^15, w), min((bot+16)&^15, h))
}

// DiffRects returns the changed tile x tile cells between a and b (same bounds, Min 0,0), with
// horizontally adjacent dirty tiles merged into one rect and edges clamped to the image. Falls
// back to the single DiffRect box when there would be more than maxRects rects or more than half
// the image is dirty. Empty if identical.
func DiffRects(a, b *image.RGBA, tile, maxRects int) []image.Rectangle {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	dirty := func(x0, y0, x1, y1 int) bool {
		for y := y0; y < y1; y++ {
			o := y*a.Stride + 4*x0
			if !bytes.Equal(a.Pix[o:o+4*(x1-x0)], b.Pix[o:o+4*(x1-x0)]) {
				return true
			}
		}
		return false
	}
	var rs []image.Rectangle
	area := 0
	for y0 := 0; y0 < h; y0 += tile {
		y1 := min(y0+tile, h)
		run := -1 // start x of the current dirty run
		// one step past the edge closes a trailing run
		for x0 := 0; x0 < w+tile; x0 += tile {
			if x0 < w && dirty(x0, y0, min(x0+tile, w), y1) {
				if run < 0 {
					run = x0
				}
				continue
			}
			if run >= 0 {
				r := image.Rect(run, y0, min(x0, w), y1)
				rs, area, run = append(rs, r), area+r.Dx()*r.Dy(), -1
				if len(rs) > maxRects || 2*area > w*h {
					return []image.Rectangle{DiffRect(a, b)}
				}
			}
		}
	}
	return rs
}
