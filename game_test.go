package main

import (
	"testing"
	"time"
)

func TestStopWalkIfOutside(t *testing.T) {
	old := gs.ClickToToggle
	gs.ClickToToggle = true
	walkToggled = true
	stopWalkIfOutside(true, false)
	if walkToggled {
		t.Fatalf("walkToggled should be false after outside click")
	}

	walkToggled = true
	stopWalkIfOutside(true, true)
	if !walkToggled {
		t.Fatalf("walkToggled should remain true when clicking inside game")
	}

	walkToggled = true
	stopWalkIfOutside(false, false)
	if !walkToggled {
		t.Fatalf("walkToggled should remain true when not clicking")
	}

	gs.ClickToToggle = old
}

func TestContinueHeldWalk(t *testing.T) {
	prev := inputState{mouseDown: true}
	if !continueHeldWalk(prev, false, true, 0, false) {
		t.Fatalf("walk should continue when mouse is held outside")
	}
	if continueHeldWalk(prev, false, false, 0, false) {
		t.Fatalf("walk should stop when mouse button is released")
	}
	if !continueHeldWalk(inputState{}, true, true, 2, false) {
		t.Fatalf("walk should start when mouse is held inside game")
	}
}

func TestAltNetDelay(t *testing.T) {
	full := 100 * time.Millisecond
	var start time.Time

	if d, s := altNetDelay(1, start, time.Now(), full); d != 0 || !s.IsZero() {
		t.Fatalf("frame 1 got delay %v start %v", d, s)
	}

	now := time.Now()
	if d, s := altNetDelay(3, start, now, full); d != 0 || s.IsZero() {
		t.Fatalf("frame 3 got delay %v start %v", d, s)
	} else {
		start = s
	}

	half := start.Add(1500 * time.Millisecond)
	if d, _ := altNetDelay(4, start, half, full); d < 49*time.Millisecond || d > 51*time.Millisecond {
		t.Fatalf("half ramp got %v", d)
	}

	end := start.Add(3 * time.Second)
	if d, _ := altNetDelay(10, start, end, full); d != full {
		t.Fatalf("end ramp got %v want %v", d, full)
	}
}
