package main

import "testing"

func TestSetHighQualityResamplingEnabled(t *testing.T) {
	orig := highQualityResampling.Load()
	defer setHighQualityResamplingEnabled(orig)

	setHighQualityResamplingEnabled(false)
	if highQualityResampling.Load() {
		t.Fatalf("expected highQualityResampling to be false")
	}

	setHighQualityResamplingEnabled(true)
	if !highQualityResampling.Load() {
		t.Fatalf("expected highQualityResampling to be true")
	}
}
