// Package view holds pure helpers for the viewer: key mapping and cursor geometry.
package view

import "github.com/hajimehoshi/ebiten/v2"

var vkMap = map[ebiten.Key]uint16{
	ebiten.KeyArrowLeft: 0x25, ebiten.KeyArrowUp: 0x26, ebiten.KeyArrowRight: 0x27, ebiten.KeyArrowDown: 0x28,
	ebiten.KeyEnter: 0x0D, ebiten.KeyNumpadEnter: 0x0D, ebiten.KeyEscape: 0x1B, ebiten.KeyTab: 0x09,
	ebiten.KeyBackspace: 0x08, ebiten.KeySpace: 0x20, ebiten.KeyInsert: 0x2D, ebiten.KeyDelete: 0x2E,
	ebiten.KeyHome: 0x24, ebiten.KeyEnd: 0x23, ebiten.KeyPageUp: 0x21, ebiten.KeyPageDown: 0x22,
	ebiten.KeyShiftLeft: 0xA0, ebiten.KeyShiftRight: 0xA1,
	ebiten.KeyControlLeft: 0xA2, ebiten.KeyControlRight: 0xA3,
	ebiten.KeyAltLeft: 0xA4, ebiten.KeyAltRight: 0xA5, ebiten.KeyCapsLock: 0x14,
	ebiten.KeySemicolon: 0xBA, ebiten.KeyEqual: 0xBB, ebiten.KeyComma: 0xBC, ebiten.KeyMinus: 0xBD,
	ebiten.KeyPeriod: 0xBE, ebiten.KeySlash: 0xBF, ebiten.KeyBackquote: 0xC0,
	ebiten.KeyBracketLeft: 0xDB, ebiten.KeyBackslash: 0xDC, ebiten.KeyBracketRight: 0xDD, ebiten.KeyQuote: 0xDE,
}

func init() {
	// Ebiten's A-Z, Digit0-9, F1-F12 and Numpad0-9 constants are consecutive.
	for i := 0; i < 26; i++ {
		vkMap[ebiten.KeyA+ebiten.Key(i)] = 0x41 + uint16(i)
	}
	for i := 0; i < 10; i++ {
		vkMap[ebiten.KeyDigit0+ebiten.Key(i)] = 0x30 + uint16(i)
		vkMap[ebiten.KeyNumpad0+ebiten.Key(i)] = 0x60 + uint16(i)
	}
	for i := 0; i < 12; i++ {
		vkMap[ebiten.KeyF1+ebiten.Key(i)] = 0x70 + uint16(i)
	}
}

// VK returns the Windows virtual-key code for an Ebiten key.
func VK(k ebiten.Key) (vk uint16, ok bool) {
	vk, ok = vkMap[k]
	return
}
