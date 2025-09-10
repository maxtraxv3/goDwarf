package main

import (
	_ "embed"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed data/shaders/upscale_lanczos.kage
var upscaleShaderSrc []byte

var upscaleShader *ebiten.Shader

func init() {
	if err := ReloadUpscaleShader(); err != nil {
		panic(err)
	}
}

// ReloadUpscaleShader recompiles the Lanczos upscale shader from disk and swaps it in.
// Falls back to the embedded shader source if reading from disk fails.
func ReloadUpscaleShader() error {
	if b, err := os.ReadFile("data/shaders/upscale_lanczos.kage"); err == nil {
		if sh, err2 := ebiten.NewShader(b); err2 == nil {
			upscaleShader = sh
			return nil
		} else {
			return err2
		}
	}
	sh, err := ebiten.NewShader(upscaleShaderSrc)
	if err != nil {
		return err
	}
	upscaleShader = sh
	return nil
}
