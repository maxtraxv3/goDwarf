package main

import (
	"strings"
	"sync"
)

const (
	maxChatMessages = 1000
)

var (
	chatLog             = messageLog{max: maxChatMessages}
	chatTTSDisabledOnce sync.Once
)

func chatMessage(msg string) {
	if msg == "" {
		return
	}

	speaker := chatSpeaker(msg)
	if speaker != "" {
		playersMu.RLock()
		p, ok := players[speaker]
		blocked := ok && (p.Blocked || p.Ignored)
		playersMu.RUnlock()
		if blocked {
			return
		}
	}

	tagged := chatHasPlayerTag(msg)

	chatLog.Add(msg)
	appendChatLog(msg)

	updateChatWindow()

	if tagged && !isSelfChatMessage(msg) {
		playSound([]uint16{sndThinkTo})
	}

	if gs.ChatTTS && !blockTTS && !isSelfChatMessage(msg) {
		if speaker == "" || !isTTSBlocked(speaker) {
			speakChatMessage(msg)
		}
	} else if !gs.ChatTTS {
		chatTTSDisabledOnce.Do(func() {
			consoleMessage("Chat TTS is disabled. Enable it in settings to hear messages.")
		})
	}

	runChatTriggers(msg)
}

func getChatMessages() []string {
	format := gs.TimestampFormat
	if format == "" {
		format = "3:04PM"
	}
	return chatLog.Entries(format, gs.ChatTimestamps)
}

func isSelfChatMessage(msg string) bool {
	if playerName == "" {
		return false
	}
	m := strings.ToLower(strings.TrimSpace(msg))
	name := strings.ToLower(playerName)

	// Emotes like "(Hero waves)"
	if strings.HasPrefix(m, "("+name+" ") {
		return true
	}
	// Spoken lines like "Hero says, ..." or "Hero yells, ..."
	if strings.HasPrefix(m, name+" ") {
		rest := strings.TrimSpace(m[len(name):])
		if strings.HasPrefix(rest, "says,") ||
			strings.HasPrefix(rest, "yells,") ||
			strings.HasPrefix(rest, "whispers,") ||
			strings.HasPrefix(rest, "exclaims,") {
			return true
		}
	}
	return false
}

// chatSpeaker extracts the leading player name from a chat message, folded to
// canonical form. It returns an empty string if no name could be parsed.
func chatSpeaker(msg string) string {
	m := strings.TrimSpace(msg)
	m = strings.TrimPrefix(m, "(")
	if i := strings.IndexByte(m, ' '); i > 0 {
		return utfFold(m[:i])
	}
	return ""
}

func chatHasPlayerTag(msg string) bool {
	if playerName == "" {
		return false
	}
	lower := strings.ToLower(msg)
	name := strings.ToLower("@" + playerName)
	if strings.Contains(lower, name) {
		return true
	}
	var prof string
	playersMu.RLock()
	if p, ok := players[playerName]; ok {
		prof = p.Class
	}
	playersMu.RUnlock()
	if prof != "" && strings.Contains(lower, "@"+strings.ToLower(prof)) {
		return true
	}
	return false
}
