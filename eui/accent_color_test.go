package eui

import "testing"

// Test that setting the accent color updates the named "accent" color reference.
func TestSetAccentColorUpdatesNamedColor(t *testing.T) {
	namedColors = map[string]Color{}
	SetAccentColor(NewColor(255, 0, 0, 255))
	var c Color
	if err := c.UnmarshalJSON([]byte("\"accent\"")); err != nil {
		t.Fatalf("failed to unmarshal accent: %v", err)
	}
	if c != AccentColor() {
		t.Fatalf("named accent %v does not match accent color %v", c, AccentColor())
	}
}
