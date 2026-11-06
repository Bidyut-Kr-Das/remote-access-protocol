// Package input injects remote mouse/key messages and tracks held state.
package input

import "rdp-go/internal/protocol"

const (
	MouseMoveF  = 0x0001
	MouseAbsF   = 0x8000
	MouseLDown  = 0x0002
	MouseLUp    = 0x0004
	MouseRDown  = 0x0008
	MouseRUp    = 0x0010
	MouseMDown  = 0x0020
	MouseMUp    = 0x0040
	MouseWheelF = 0x0800

	KeyExtended = 0x0001
	KeyUp       = 0x0002
	KeyScancode = 0x0008
)

type Event struct {
	Mouse bool
	X, Y  int32  // absolute 0..65535 for moves
	Flags uint32 // Mouse*F or Key* values above
	Scan  uint16
	Wheel int32
}

// Send and ScanOf are replaced on Windows by sendinput_windows.go.
var Send = func(ev []Event) {}
var ScanOf = func(vk uint16) uint16 { return vk }

type Injector struct {
	W, H    uint16
	held    map[uint16]bool
	buttons [4]bool
}

func NewInjector(w, h uint16) *Injector {
	return &Injector{W: w, H: h, held: map[uint16]bool{}}
}

var btnDown = [4]uint32{0, MouseLDown, MouseRDown, MouseMDown}
var btnUp = [4]uint32{0, MouseLUp, MouseRUp, MouseMUp}

func scale(v, n uint16) int32 {
	if n < 2 {
		return 0
	}
	if v > n-1 {
		v = n - 1
	}
	return int32(uint32(v) * 65535 / uint32(n-1))
}

func (i *Injector) key(vk uint16, down bool) Event {
	f := uint32(KeyScancode)
	if IsExtended(vk) {
		f |= KeyExtended
	}
	if !down {
		f |= KeyUp
	}
	return Event{Flags: f, Scan: ScanOf(vk)}
}

func (i *Injector) Apply(m protocol.Msg) {
	switch m := m.(type) {
	case protocol.MouseMove:
		Send([]Event{{Mouse: true, X: scale(m.X, i.W), Y: scale(m.Y, i.H), Flags: MouseMoveF | MouseAbsF}})
	case protocol.MouseButton:
		if m.Button < 1 || m.Button > 3 {
			return
		}
		down := m.Down != 0
		i.buttons[m.Button] = down
		f := btnUp[m.Button]
		if down {
			f = btnDown[m.Button]
		}
		Send([]Event{{Mouse: true, Flags: f}})
	case protocol.MouseWheel:
		Send([]Event{{Mouse: true, Flags: MouseWheelF, Wheel: int32(m.Delta)}})
	case protocol.Key:
		down := m.Down != 0
		if down {
			i.held[m.VK] = true
		} else {
			delete(i.held, m.VK)
		}
		Send([]Event{i.key(m.VK, down)})
	case protocol.Release:
		i.Release()
	}
}

// Release sends up for every held key/button, then clears.
func (i *Injector) Release() {
	var ev []Event
	for vk := range i.held {
		ev = append(ev, i.key(vk, false))
	}
	for b := 1; b <= 3; b++ {
		if i.buttons[b] {
			ev = append(ev, Event{Mouse: true, Flags: btnUp[b]})
		}
	}
	i.held = map[uint16]bool{}
	i.buttons = [4]bool{}
	if len(ev) > 0 {
		Send(ev)
	}
}

func IsExtended(vk uint16) bool {
	switch {
	case vk >= 0x21 && vk <= 0x28:
	case vk == 0x2C, vk == 0x2D, vk == 0x2E, vk == 0x5B, vk == 0x5C, vk == 0x5D,
		vk == 0x6F, vk == 0x90, vk == 0xA3, vk == 0xA5:
	default:
		return false
	}
	return true
}
