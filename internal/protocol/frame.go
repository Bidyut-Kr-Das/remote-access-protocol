package protocol

import (
	"io"
)

// Write sends one framed message: [u32 BE len][u8 type][payload], len = 1+len(payload).
func Write(w io.Writer, m Msg) error {
	p := m.Marshal()
	if 1+len(p) > MaxLen {
		return ErrOversize
	}
	buf := make([]byte, 5, 5+len(p))
	be.PutUint32(buf, uint32(1+len(p)))
	buf[4] = m.Type()
	_, err := w.Write(append(buf, p...))
	return err
}

// Read receives one framed message.
func Read(r io.Reader) (Msg, error) { return ReadMax(r, MaxLen) }

// ReadMax is Read with a smaller length cap, rejected before allocating.
func ReadMax(r io.Reader, max uint32) (Msg, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	n := be.Uint32(h[:])
	if n == 0 || n > max {
		return nil, ErrOversize
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return unmarshal(body[0], body[1:])
}

func unmarshal(t byte, p []byte) (Msg, error) {
	need := func(n int) error {
		if len(p) < n {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	switch t {
	case TypeChallenge:
		if err := need(32); err != nil {
			return nil, err
		}
		var m Challenge
		copy(m.Nonce[:], p)
		return m, nil
	case TypeHello:
		if err := need(33); err != nil {
			return nil, err
		}
		m := Hello{Version: p[0]}
		copy(m.Hash[:], p[1:])
		return m, nil
	case TypeOK:
		if err := need(4); err != nil {
			return nil, err
		}
		return OK{be.Uint16(p), be.Uint16(p[2:])}, nil
	case TypeDeny:
		return Deny{}, nil
	case TypeFrame:
		if err := need(9); err != nil {
			return nil, err
		}
		return Frame{be.Uint16(p), be.Uint16(p[2:]), be.Uint16(p[4:]), be.Uint16(p[6:]), p[8], p[9:]}, nil
	case TypeMouseMove:
		if err := need(4); err != nil {
			return nil, err
		}
		return MouseMove{be.Uint16(p), be.Uint16(p[2:])}, nil
	case TypeMouseButton:
		if err := need(2); err != nil {
			return nil, err
		}
		return MouseButton{p[0], p[1]}, nil
	case TypeMouseWheel:
		if err := need(2); err != nil {
			return nil, err
		}
		return MouseWheel{int16(be.Uint16(p))}, nil
	case TypeKey:
		if err := need(3); err != nil {
			return nil, err
		}
		return Key{be.Uint16(p), p[2]}, nil
	case TypeRelease:
		return Release{}, nil
	case TypePing:
		return Ping{}, nil
	case TypePong:
		return Pong{}, nil
	case TypeAck:
		return Ack{}, nil
	case TypeClipboard:
		if len(p) > MaxClipboard {
			return nil, ErrOversize
		}
		return Clipboard{string(p)}, nil
	}
	return nil, ErrUnknownType
}
