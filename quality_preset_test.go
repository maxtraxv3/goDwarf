package main

import "testing"

func TestQualityPresetPersisted(t *testing.T) {
	origDir := dataDirPath
	dataDirPath = t.TempDir()
	t.Cleanup(func() { dataDirPath = origDir })

	gs = gsdef
	applyQualityPreset("Low")
	saveSettings()

	gs = gsdef
	loadSettings()

	if gs.ShaderLighting {
		t.Errorf("ShaderLighting loaded as true, want false")
	}
	if preset := detectQualityPreset(); preset != 1 {
		t.Errorf("detectQualityPreset()=%d, want 1", preset)
	}
}
