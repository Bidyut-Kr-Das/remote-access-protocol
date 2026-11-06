package view

// Fit letterboxes an iw x ih image into a ww x wh window. scale is 0 for any non-positive dimension.
func Fit(iw, ih, ww, wh int) (scale, ox, oy float64) {
	if iw <= 0 || ih <= 0 || ww <= 0 || wh <= 0 {
		return 0, 0, 0
	}
	scale = min(float64(ww)/float64(iw), float64(wh)/float64(ih))
	return scale, (float64(ww) - float64(iw)*scale) / 2, (float64(wh) - float64(ih)*scale) / 2
}

// MapCursor maps a window cursor position to image pixels; ok=false in the letterbox bars or on bad sizes.
func MapCursor(cx, cy, iw, ih, ww, wh int) (x, y uint16, ok bool) {
	s, ox, oy := Fit(iw, ih, ww, wh)
	if s == 0 {
		return 0, 0, false
	}
	fx, fy := float64(cx)-ox, float64(cy)-oy
	if fx < 0 || fy < 0 || fx >= float64(iw)*s || fy >= float64(ih)*s {
		return 0, 0, false
	}
	return uint16(min(max(int(fx/s), 0), iw-1)), uint16(min(max(int(fy/s), 0), ih-1)), true
}
