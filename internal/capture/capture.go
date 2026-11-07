// Package capture grabs the primary display and encodes it as a protocol FRAME.
package capture

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

	"github.com/kbinani/screenshot"

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

// Encode JPEG-encodes img as a full-rect frame (Codec 1).
func Encode(img image.Image, quality int) (*protocol.Frame, error) {
	w, h, err := dims(img.Bounds())
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return &protocol.Frame{W: w, H: h, Codec: 1, Data: buf.Bytes()}, nil
}
