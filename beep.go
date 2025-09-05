package main

import (
	"sync"
	"time"
)

type beepSpec struct {
	program int
	key     int
}

var (
	beepMu    sync.Mutex
	beepCache = make(map[beepSpec][]byte)
)

// playBeep renders and plays a short note using the given program and key.
// The note is cached after the first render.
func playBeep(program, key int) {
	if gs.Mute || !gs.GameSound || audioContext == nil {
		return
	}

	spec := beepSpec{program: program, key: key}
	beepMu.Lock()
	pcm, ok := beepCache[spec]
	beepMu.Unlock()
	if !ok {
		notes := []Note{{Key: key, Velocity: 120, Start: 0, Duration: 200 * time.Millisecond}}
		left, right, err := renderSong(program, notes)
		if err != nil {
			return
		}
		pcm = mixPCM(left, right)
		beepMu.Lock()
		beepCache[spec] = pcm
		beepMu.Unlock()
	}

	p := audioContext.NewPlayerFromBytes(pcm)
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
		logDebug("playBeep too many sound players (%d)", len(soundPlayers))
		p.Close()
		return
	}
	soundPlayers[p] = struct{}{}
	soundMu.Unlock()
	p.Play()
}
