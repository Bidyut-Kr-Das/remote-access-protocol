// Package capture grabs the primary display and encodes it as a protocol FRAME.
package capture

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

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

// Grab captures the primary display.
func Grab() (*image.RGBA, error) { return screenshot.CaptureDisplay(0) }

// Encode JPEG-encodes img as a frame (Codec 1) placed at img's bounds (a SubImage keeps its Min).
func Encode(img image.Image, quality int) (*protocol.Frame, error) {
	w, h, err := dims(img.Bounds())
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
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
