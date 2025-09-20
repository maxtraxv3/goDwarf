package main

import "runtime"

// isWASM is true when running in a WebAssembly (browser) environment.
var isWASM bool

// In-memory archives when running under WASM.
var wasmCLImagesData []byte
var wasmCLSoundsData []byte
var wasmMovieZipData []byte

func init() {
	// GOOS=js and GOARCH=wasm for GopherJS/WebAssembly targets.
	isWASM = (runtime.GOOS == "js" || runtime.GOARCH == "wasm")
}
