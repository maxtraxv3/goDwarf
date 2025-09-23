//go:build script

package main

import (
	"strings"
	"time"

	"gt"
)

// "Yes Boats" – automatically whisper "yes" when a ferryman offers a ride.
//
// Notes for non‑technical players:
// - This runs when an NPC says something like "My fine boats" and mentions
//   your character by name, to avoid false triggers.
// - It replies only once every few seconds so it won’t spam.

const scriptName = "Yes Boats"
const scriptAuthor = "Example"
const scriptCategory = "Quality Of Life"
const scriptAPIVersion = 1

var (
	lastYes     time.Time         // last time we replied
	yesCooldown = 8 * time.Second // throttle window
)

func Init() {
	// React when an NPC message contains this phrase (case‑insensitive).
	gt.NPCChat("my fine boats", sendYes)
	// Some ferrymen NPCs fail to register as NPC chat because of how their
	// names are formatted, so fall back to the general chat stream and
	// filter by the speaker ourselves.
	gt.Chat("my fine boats", sendYesIfNPC)
}

func sendYes(msg string) {
	// Only respond when our name appears in the message, because we will overhear other people buying boats
	me := gt.PlayerName()
	if me == "" || !gt.Includes(gt.Lower(msg), gt.Lower(me)) {
		return
	}
	now := time.Now()
	if !lastYes.IsZero() && now.Sub(lastYes) < yesCooldown {
		// Too soon since last reply: skip to avoid spam.
		return
	}
	lastYes = now
	gt.Run("/whisper yes")
}

func sendYesIfNPC(msg string) {
	if !speakerIsNPC(msg) {
		return
	}
	sendYes(msg)
}

func speakerIsNPC(msg string) bool {
	name := strings.ToLower(strings.TrimSpace(extractSpeaker(msg)))
	if name == "" {
		return false
	}
	for _, p := range gt.Players() {
		if strings.ToLower(p.Name) == name && p.IsNPC {
			return true
		}
	}
	return false
}

func extractSpeaker(msg string) string {
	m := strings.TrimSpace(msg)
	if m == "" {
		return ""
	}
	if strings.HasPrefix(m, "(") {
		if end := strings.IndexByte(m, ')'); end > 1 {
			return strings.TrimSpace(m[1:end])
		}
	}
	lower := strings.ToLower(m)
	for _, sep := range []string{" says", " yells", " whispers", " asks", " exclaims"} {
		if idx := strings.Index(lower, sep); idx > 0 {
			return strings.TrimSpace(m[:idx])
		}
	}
	if i := strings.IndexByte(m, ' '); i > 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}
