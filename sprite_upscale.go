package main

import "image"

type rgbaPixel struct {
	r uint8
	g uint8
	b uint8
	a uint8
}

func sampleRGBA(src *image.RGBA, x, y int) rgbaPixel {
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w == 0 || h == 0 {
		return rgbaPixel{}
	}
	if x < 0 {
		x = 0
	} else if x >= w {
		x = w - 1
	}
	if y < 0 {
		y = 0
	} else if y >= h {
		y = h - 1
	}
	ax := b.Min.X + x
	ay := b.Min.Y + y
	i := src.PixOffset(ax, ay)
	pix := src.Pix
	return rgbaPixel{r: pix[i+0], g: pix[i+1], b: pix[i+2], a: pix[i+3]}
}

func setRGBA(dst *image.RGBA, x, y int, c rgbaPixel) {
	b := dst.Bounds()
	ax := b.Min.X + x
	ay := b.Min.Y + y
	i := dst.PixOffset(ax, ay)
	pix := dst.Pix
	pix[i+0] = c.r
	pix[i+1] = c.g
	pix[i+2] = c.b
	pix[i+3] = c.a
}

func scale2xRGBA(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w == 0 || h == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w*2, h*2))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bPix := sampleRGBA(src, x, y-1)
			d := sampleRGBA(src, x-1, y)
			e := sampleRGBA(src, x, y)
			f := sampleRGBA(src, x+1, y)
			hPix := sampleRGBA(src, x, y+1)

			e0, e1, e2, e3 := e, e, e, e
			if bPix != hPix && d != f {
				if d == bPix {
					e0 = d
				}
				if bPix == f {
					e1 = f
				}
				if d == hPix {
					e2 = d
				}
				if hPix == f {
					e3 = f
				}
			}
			setRGBA(dst, x*2+0, y*2+0, e0)
			setRGBA(dst, x*2+1, y*2+0, e1)
			setRGBA(dst, x*2+0, y*2+1, e2)
			setRGBA(dst, x*2+1, y*2+1, e3)
		}
	}
	return dst
}

func scale3xRGBA(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w == 0 || h == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w*3, h*3))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := sampleRGBA(src, x-1, y-1)
			bPix := sampleRGBA(src, x, y-1)
			c := sampleRGBA(src, x+1, y-1)
			d := sampleRGBA(src, x-1, y)
			e := sampleRGBA(src, x, y)
			f := sampleRGBA(src, x+1, y)
			g := sampleRGBA(src, x-1, y+1)
			hPix := sampleRGBA(src, x, y+1)
			i := sampleRGBA(src, x+1, y+1)

			e0, e1, e2, e3, e4, e5, e6, e7, e8 := e, e, e, e, e, e, e, e, e
			if bPix != hPix && d != f {
				if d == bPix {
					e0 = d
				}
				if (d == bPix && e != c) || (bPix == f && e != a) {
					e1 = bPix
				}
				if bPix == f {
					e2 = f
				}
				if (d == bPix && e != g) || (d == hPix && e != a) {
					e3 = d
				}
				if (bPix == f && e != i) || (hPix == f && e != c) {
					e5 = f
				}
				if d == hPix {
					e6 = d
				}
				if (d == hPix && e != i) || (hPix == f && e != g) {
					e7 = hPix
				}
				if hPix == f {
					e8 = f
				}
			}
			ox := x * 3
			oy := y * 3
			setRGBA(dst, ox+0, oy+0, e0)
			setRGBA(dst, ox+1, oy+0, e1)
			setRGBA(dst, ox+2, oy+0, e2)
			setRGBA(dst, ox+0, oy+1, e3)
			setRGBA(dst, ox+1, oy+1, e4)
			setRGBA(dst, ox+2, oy+1, e5)
			setRGBA(dst, ox+0, oy+2, e6)
			setRGBA(dst, ox+1, oy+2, e7)
			setRGBA(dst, ox+2, oy+2, e8)
		}
	}
	return dst
}
