// Package protocol implements the raphost/rapview wire framing and messages.
package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	TypeChallenge   byte = 0x01
	TypeHello       byte = 0x02
	TypeOK          byte = 0x03
	TypeDeny        byte = 0x04
	TypeFrame       byte = 0x10
	TypeMouseMove   byte = 0x20
	TypeMouseButton byte = 0x21
	TypeMouseWheel  byte = 0x22
	TypeKey         byte = 0x23
	TypeRelease     byte = 0x24
	TypePing        byte = 0x30
	TypePong        byte = 0x31
	TypeClipboard   byte = 0x40
)

const (
	MaxLen            = 16 << 20
	MaxClipboard      = 256 << 10
	Version      byte = 1
)

var (
	ErrOversize    = errors.New("protocol: message too large")
	ErrUnknownType = errors.New("protocol: unknown message type")
)

// Msg is a protocol message; Marshal returns the payload only.
type Msg interface {
	Type() byte
	Marshal() []byte
}

type Challenge struct{ Nonce [32]byte }
type Hello struct {
	Version byte
	Hash    [32]byte
}
type OK struct{ W, H uint16 }
type Deny struct{}
type Frame struct {
	X, Y, W, H uint16
	Codec      byte
	Data       []byte
}
type MouseMove struct{ X, Y uint16 }
type MouseButton struct{ Button, Down byte }
type MouseWheel struct{ Delta int16 }
type Key struct {
	VK   uint16
	Down byte
}
type Release struct{}
type Ping struct{}
type Pong struct{}

// Clipboard carries UTF-8 text; either side sends it when its local clipboard changes.
type Clipboard struct{ Text string }

func (Challenge) Type() byte   { return TypeChallenge }
func (Hello) Type() byte       { return TypeHello }
func (OK) Type() byte          { return TypeOK }
func (Deny) Type() byte        { return TypeDeny }
func (Frame) Type() byte       { return TypeFrame }
func (MouseMove) Type() byte   { return TypeMouseMove }
func (MouseButton) Type() byte { return TypeMouseButton }
func (MouseWheel) Type() byte  { return TypeMouseWheel }
func (Key) Type() byte         { return TypeKey }
func (Release) Type() byte     { return TypeRelease }
func (Ping) Type() byte        { return TypePing }
func (Pong) Type() byte        { return TypePong }
func (Clipboard) Type() byte   { return TypeClipboard }

var be = binary.BigEndian

func (m Challenge) Marshal() []byte { return m.Nonce[:] }
func (m Hello) Marshal() []byte     { return append([]byte{m.Version}, m.Hash[:]...) }
func (m OK) Marshal() []byte        { return be.AppendUint16(be.AppendUint16(nil, m.W), m.H) }
func (Deny) Marshal() []byte        { return nil }
func (m Frame) Marshal() []byte {
	b := make([]byte, 0, 9+len(m.Data))
	for _, v := range []uint16{m.X, m.Y, m.W, m.H} {
		b = be.AppendUint16(b, v)
	}
	b = append(b, m.Codec)
	return append(b, m.Data...)
}
func (m MouseMove) Marshal() []byte   { return be.AppendUint16(be.AppendUint16(nil, m.X), m.Y) }
func (m MouseButton) Marshal() []byte { return []byte{m.Button, m.Down} }
func (m MouseWheel) Marshal() []byte  { return be.AppendUint16(nil, uint16(m.Delta)) }
func (m Key) Marshal() []byte         { return append(be.AppendUint16(nil, m.VK), m.Down) }
func (Release) Marshal() []byte       { return nil }
func (Ping) Marshal() []byte          { return nil }
func (Pong) Marshal() []byte          { return nil }
func (m Clipboard) Marshal() []byte   { return []byte(m.Text) }
