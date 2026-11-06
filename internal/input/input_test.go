package input

import (
	"testing"

	"rdp-go/internal/protocol"
)

func rec(t *testing.T) *[]Event {
	var got []Event
	oldS, oldO := Send, ScanOf
	Send = func(ev []Event) { got = append(got, ev...) }
	ScanOf = func(vk uint16) uint16 { return vk }
	t.Cleanup(func() { Send, ScanOf = oldS, oldO })
	return &got
}

func TestMouseMoveScalesAndClamps(t *testing.T) {
	got := rec(t)
	in := NewInjector(1920, 1080)
	for _, c := range []struct {
		x, y   uint16
		wx, wy int32
	}{{0, 0, 0, 0}, {1919, 1079, 65535, 65535}, {5000, 5000, 65535, 65535}} {
		*got = nil
		in.Apply(protocol.MouseMove{X: c.x, Y: c.y})
		if len(*got) != 1 || (*got)[0].X != c.wx || (*got)[0].Y != c.wy || !(*got)[0].Mouse {
			t.Fatalf("move %d,%d: %+v", c.x, c.y, *got)
		}
		if f := (*got)[0].Flags; f != MouseMoveF|MouseAbsF {
			t.Fatalf("flags %#x", f)
		}
	}
}

func TestKeyFlags(t *testing.T) {
	got := rec(t)
	in := NewInjector(1920, 1080)
	in.Apply(protocol.Key{VK: 0x25, Down: 1})
	in.Apply(protocol.Key{VK: 0x41, Down: 0})
	if (*got)[0].Flags != KeyScancode|KeyExtended {
		t.Fatalf("%#x", (*got)[0].Flags)
	}
	if (*got)[1].Flags != KeyScancode|KeyUp {
		t.Fatalf("%#x", (*got)[1].Flags)
	}
}

func TestReleaseUpsEverything(t *testing.T) {
	got := rec(t)
	in := NewInjector(1920, 1080)
	in.Apply(protocol.Key{VK: 'A', Down: 1})
	in.Apply(protocol.Key{VK: 'B', Down: 1})
	in.Apply(protocol.Key{VK: 'A', Down: 1})
	in.Apply(protocol.MouseButton{Button: 1, Down: 1})
	*got = nil
	in.Apply(protocol.Release{})
	if len(*got) != 3 {
		t.Fatalf("want 3 ups, got %+v", *got)
	}
	ups := 0
	for _, e := range *got {
		if e.Mouse && e.Flags == MouseLUp || !e.Mouse && e.Flags&KeyUp != 0 {
			ups++
		}
	}
	if ups != 3 {
		t.Fatalf("not all ups: %+v", *got)
	}
	*got = nil
	in.Release()
	if len(*got) != 0 {
		t.Fatalf("second release: %+v", *got)
	}
}

func TestWheel(t *testing.T) {
	got := rec(t)
	NewInjector(1920, 1080).Apply(protocol.MouseWheel{Delta: -120})
	if len(*got) != 1 || (*got)[0] != (Event{Mouse: true, Flags: MouseWheelF, Wheel: -120}) {
		t.Fatalf("%+v", *got)
	}
}
