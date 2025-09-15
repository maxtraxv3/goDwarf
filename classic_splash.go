package main

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

// Keep a handle to the default splash so we can restore it
var defaultSplashImg *ebiten.Image

// prepareClassicSplash builds an opaque, 2x-nearest version of CL_Images id 4
// cropped to the classic field box and assigns it to splashImg if enabled.
// If disabled or unavailable, restores the default splash image.
func prepareClassicSplash() {
	// Capture the default (embedded or data/splash.png) once.
	if defaultSplashImg == nil && splashImg != nil {
		defaultSplashImg = splashImg
	}

	if !gs.ShowClanLordSplashImage || clImages == nil {
		if defaultSplashImg != nil {
			splashImg = defaultSplashImg
		}
		return
	}

	// Load CL_Images id 4 and crop the classic field area.
	src := loadImage(4)
	if src == nil {
		if defaultSplashImg != nil {
			splashImg = defaultSplashImg
		}
		return
	}

	// Classic field box within id 4 (left=240, top=8, size=547x540)
	const cropX, cropY = 240, 8
	const cropW, cropH = 547, 540
	bounds := src.Bounds()
	r := image.Rect(cropX, cropY, cropX+cropW, cropY+cropH).Intersect(bounds)
	if r.Empty() || r.Dx() <= 0 || r.Dy() <= 0 {
		if defaultSplashImg != nil {
			splashImg = defaultSplashImg
		}
		return
	}

	// 1) Flatten cropped region over white to remove alpha
	flat := newImage(r.Dx(), r.Dy())
	flat.Fill(color.White)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest, DisableMipmaps: false}
	op.GeoM.Translate(float64(-r.Min.X), float64(-r.Min.Y))
	flat.DrawImage(src, op)

	// 2) Scale 2x with nearest-neighbor for crisp pixels
	scaled := newImage(r.Dx()*2, r.Dy()*2)
	sop := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest, DisableMipmaps: false}
	sop.GeoM.Scale(2, 2)
	scaled.DrawImage(flat, sop)

	// Hand the processed image to the splash drawer
	splashImg = scaled
}
