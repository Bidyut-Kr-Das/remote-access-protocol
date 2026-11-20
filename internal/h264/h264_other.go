//go:build !windows

package h264

import (
	"errors"
	"image"
)

var errUnsupported = errors.New("h264: Media Foundation is Windows-only")

// Available reports false: there is no H.264 codec on this platform.
func Available() bool { return false }

type Encoder struct{}

func NewEncoder(w, h, fps, bitrateKbps int) (*Encoder, error) { return nil, errUnsupported }
func (e *Encoder) Encode(img *image.RGBA) ([]byte, error)     { return nil, errUnsupported }
func (e *Encoder) ForceKeyFrame() error                       { return errUnsupported }
func (e *Encoder) Close()                                     {}

type Decoder struct{}

func NewDecoder() (*Decoder, error)                                { return nil, errUnsupported }
func (d *Decoder) Decode(au []byte, w, h int) (*image.RGBA, error) { return nil, errUnsupported }
func (d *Decoder) Close()                                          {}
