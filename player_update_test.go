package main

import (
	"testing"
	"time"
)

func TestUpdatePlayerAppearanceUnmarksDead(t *testing.T) {
	players = map[string]*Player{
		"Bob": {
			Name:       "Bob",
			Dead:       true,
			FellWhere:  "somewhere",
			KillerName: "killer",
			FellTime:   time.Unix(1, 0),
		},
	}
	updatePlayerAppearance("Bob", 1, nil, false)
	p := players["Bob"]
	if p.Dead || p.FellWhere != "" || p.KillerName != "" || !p.FellTime.IsZero() {
		t.Fatalf("expected Bob to be marked alive, got %#v", p)
	}
}
