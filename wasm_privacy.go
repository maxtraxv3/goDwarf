package main

import "sync/atomic"

var wasmPrivacyFlag atomic.Bool

func enterWasmPrivacyMode() {
	if !isWASM {
		return
	}
	if wasmPrivacyFlag.CompareAndSwap(false, true) {
		killNameTagCache()
	}
}

func exitWasmPrivacyMode() {
	if wasmPrivacyFlag.Swap(false) {
		killNameTagCache()
	}
}

func wasmPrivacyActive() bool {
	return wasmPrivacyFlag.Load()
}
