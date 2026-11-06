package view

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestVKKnown(t *testing.T) {
	cases := map[ebiten.Key]uint16{
		ebiten.KeyA: 0x41, ebiten.KeyZ: 0x5A, ebiten.KeyDigit0: 0x30, ebiten.KeyDigit9: 0x39,
		ebiten.KeyF1: 0x70, ebiten.KeyF12: 0x7B, ebiten.KeyNumpad0: 0x60, ebiten.KeyNumpad9: 0x69,
		ebiten.KeyArrowLeft: 0x25, ebiten.KeyControlLeft: 0xA2, ebiten.KeyEnter: 0x0D,
	}
	for k, want := range cases {
		if got, ok := VK(k); !ok || got != want {
			t.Errorf("VK(%v)=%#x,%v want %#x", k, got, ok, want)
		}
	}
	if _, ok := VK(ebiten.KeyMax + 1); ok {
		t.Error("KeyMax+1 should be unmapped")
	}
}

func TestFitLetterbox(t *testing.T) {
	s, ox, oy := Fit(1920, 1080, 960, 1080)
	if s != 0.5 || ox != 0 || oy != 270 {
		t.Fatalf("got %v %v %v", s, ox, oy)
	}
}

func TestMapCursorZeroWindow(t *testing.T) {
	if _, _, ok := MapCursor(1, 1, 1920, 1080, 0, 100); ok {
		t.Fatal("ok")
	}
}

func TestMapCursorInBars(t *testing.T) {
	if _, _, ok := MapCursor(10, 10, 1920, 1080, 960, 1080); ok {
		t.Fatal("bar should be !ok")
	}
	x, y, ok := MapCursor(10, 300, 1920, 1080, 960, 1080)
	if !ok || x != 20 || y != 60 {
		t.Fatalf("got %d %d %v", x, y, ok)
	}
}

func TestMapCursorClampsEdge(t *testing.T) {
	x, _, ok := MapCursor(1919, 300, 1920, 1080, 1920, 1080)
	if !ok || x != 1919 {
		t.Fatalf("got %d %v", x, ok)
	}
}
