// Package capture grabs the primary display and encodes it as a protocol FRAME.
package capture

import (
	"bytes"
	"image"
	"image/jpeg"

	"github.com/kbinani/screenshot"

	"rdp-go/internal/protocol"
)

// ScreenSize returns the primary display size.
func ScreenSize() (w, h uint16, err error) {
	b := screenshot.GetDisplayBounds(0)
	return uint16(b.Dx()), uint16(b.Dy()), nil
}

// Grab captures the primary display.
func Grab() (*image.RGBA, error) { return screenshot.CaptureDisplay(0) }

// Encode JPEG-encodes img as a full-rect frame (Codec 1).
func Encode(img image.Image, quality int) (*protocol.Frame, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	b := img.Bounds()
	return &protocol.Frame{W: uint16(b.Dx()), H: uint16(b.Dy()), Codec: 1, Data: buf.Bytes()}, nil
}
