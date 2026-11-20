// Package h264 encodes RGBA frames to H.264 Annex B and back using the
// Windows Media Foundation software MFTs. Other platforms get a stub.
package h264

import "image"

// pad16 rounds n up to a whole macroblock. Encoding at 16-aligned sizes keeps
// SPS cropping out of the stream; the decoder output is cropped in Go instead.
func pad16(n int) int { return (n + 15) &^ 15 }

// rgbaToNV12 writes img into dst as NV12 (BT.601 limited range) of size pw×ph,
// replicating edge pixels into the padding. dst must hold pw*ph*3/2 bytes;
// pw and ph must be even and >= the image size.
func rgbaToNV12(img *image.RGBA, pw, ph int, dst []byte) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	pix, st := img.Pix, img.Stride
	for y := 0; y < ph; y++ {
		row := pix[min(y, h-1)*st:]
		d := dst[y*pw : y*pw+pw]
		for x := 0; x < w; x++ {
			r, g, b := int(row[4*x]), int(row[4*x+1]), int(row[4*x+2])
			d[x] = uint8((66*r+129*g+25*b+128)>>8 + 16)
		}
		for x := w; x < pw; x++ {
			d[x] = d[w-1]
		}
	}
	uv := dst[pw*ph:]
	for y := 0; y < ph/2; y++ {
		r0 := pix[min(2*y, h-1)*st:]
		r1 := pix[min(2*y+1, h-1)*st:]
		d := uv[y*pw : y*pw+pw]
		for x := 0; x < pw/2; x++ {
			i0, i1 := 4*min(2*x, w-1), 4*min(2*x+1, w-1)
			r := int(r0[i0]) + int(r0[i1]) + int(r1[i0]) + int(r1[i1])
			g := int(r0[i0+1]) + int(r0[i1+1]) + int(r1[i0+1]) + int(r1[i1+1])
			b := int(r0[i0+2]) + int(r0[i1+2]) + int(r1[i0+2]) + int(r1[i1+2])
			// Sums of 4 pixels: shift by 10 instead of 8 to average.
			d[2*x] = uint8((-38*r-74*g+112*b+512)>>10 + 128)
			d[2*x+1] = uint8((112*r-94*g-18*b+512)>>10 + 128)
		}
	}
}

// nv12ToRGBA converts the top-left w×h of an NV12 buffer whose Y plane is
// stride bytes wide and rows lines tall (UV plane follows at stride*rows).
func nv12ToRGBA(src []byte, stride, rows, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	uv := src[stride*rows:]
	for y := 0; y < h; y++ {
		ys := src[y*stride:]
		us := uv[(y/2)*stride:]
		d := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			c := 298 * (int(ys[x]) - 16)
			u, v := int(us[x&^1])-128, int(us[x|1])-128
			d[4*x] = clip((c + 409*v + 128) >> 8)
			d[4*x+1] = clip((c - 100*u - 208*v + 128) >> 8)
			d[4*x+2] = clip((c + 516*u + 128) >> 8)
			d[4*x+3] = 255
		}
	}
	return img
}

func clip(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
