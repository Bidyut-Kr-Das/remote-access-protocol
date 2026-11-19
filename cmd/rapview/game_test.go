package main

import (
	"image"
	"testing"
)

func TestNeedRealloc(t *testing.T) {
	b := image.Rect(0, 0, 200, 150)
	for _, c := range []struct {
		name       string
		x, y, w, h int
		got        bool
		want       bool
	}{
		{"no frame yet", 0, 0, 200, 150, false, true},
		{"same size", 0, 0, 200, 150, true, false},
		{"(0,0) tile smaller than backing", 0, 0, 64, 64, true, false},
		{"partial rect inside", 128, 128, 64, 22, true, false},
		{"full frame larger", 0, 0, 256, 150, true, true},
		{"partial rect past right edge", 192, 0, 64, 64, true, true},
		{"partial rect past bottom edge", 0, 128, 64, 64, true, true},
	} {
		if got := needRealloc(b, c.x, c.y, c.w, c.h, c.got); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
