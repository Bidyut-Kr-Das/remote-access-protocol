//go:build !windows

package capture

import "errors"

func newDXGI() (*dxgiGrabber, error) { return nil, errors.New("DXGI is Windows-only") }
