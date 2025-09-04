package climg

import (
	"image"
	"testing"
)

func BenchmarkDenoiseImage(b *testing.B) {
	rect := image.Rect(0, 0, 64, 64)
	src := image.NewRGBA(rect)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			off := (y*64 + x) * 4
			src.Pix[off] = uint8(x * 4)
			src.Pix[off+1] = uint8(y * 4)
			src.Pix[off+2] = uint8((x + y) * 2)
			src.Pix[off+3] = 0xFF
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img := image.NewRGBA(rect)
		copy(img.Pix, src.Pix)
		denoiseImage(img, 2, 0.5)
	}
}
