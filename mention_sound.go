package main

import (
	"sync"
	"time"
)

var (
	mentionOnce sync.Once
	mentionPCM  []byte
)

func playMentionSound() {
	if gs.Mute || !gs.GameSound {
		return
	}
	mentionOnce.Do(func() {
		notes := []Note{{Key: 84, Velocity: 120, Start: 0, Duration: 200 * time.Millisecond}}
		left, right, err := renderSong(0, notes)
		if err != nil {
			return
		}
		mentionPCM = mixPCM(left, right)
	})
	if audioContext == nil || mentionPCM == nil {
		return
	}
	p := audioContext.NewPlayerFromBytes(mentionPCM)
	vol := gs.MasterVolume * gs.GameVolume
	if gs.Mute {
		vol = 0
	}
	p.SetVolume(vol)

	soundMu.Lock()
	for sp := range soundPlayers {
		if !sp.IsPlaying() {
			sp.Close()
			delete(soundPlayers, sp)
		}
	}
	if maxSounds > 0 && len(soundPlayers) >= maxSounds {
		soundMu.Unlock()
		logDebug("playMentionSound too many sound players (%d)", len(soundPlayers))
		p.Close()
		return
	}
	soundPlayers[p] = struct{}{}
	soundMu.Unlock()
	p.Play()
}
