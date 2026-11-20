//go:build windows

package capture

import (
	"errors"
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
	"github.com/kirides/go-d3d/d3d11"
	"github.com/kirides/go-d3d/outputduplication"
)

// newDXGI duplicates output 0 of the default adapter. It fails when that output is not display 0
// (e.g. hybrid-GPU laptops), so the caller falls back to GDI.
func newDXGI() (*dxgiGrabber, error) {
	dev, ctx, err := d3d11.NewD3D11Device()
	if err != nil || dev == nil {
		return nil, fmt.Errorf("D3D11CreateDevice: %v", err)
	}
	dup, err := outputduplication.NewIDXGIOutputDuplication(dev, ctx, 0)
	if err != nil {
		ctx.Release()
		dev.Release()
		return nil, err
	}
	release := func() {
		dup.Release()
		ctx.Release()
		dev.Release()
	}
	b, err := dup.GetBounds()
	if want := screenshot.GetDisplayBounds(0); err == nil && b != want {
		err = fmt.Errorf("%w: DXGI output 0 %v is not display 0 %v", errBoundsMismatch, b, want)
	}
	if err != nil {
		release()
		return nil, err
	}
	return &dxgiGrabber{rect: b, close: release, frame: func(dst *image.RGBA) error {
		// GetImage swizzles to RGBA; on ACCESS_LOST (UAC, lock screen, mode change) it errors and Grab re-inits.
		err := dup.GetImage(dst, 0)
		if errors.Is(err, outputduplication.ErrNoImageYet) {
			return errNoNewFrame
		}
		return err
	}}, nil
}
