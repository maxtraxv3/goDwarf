package godwarf

import (
	"encoding/binary"
	"log"
	"math"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"godwarf/clsnd"
)

const (
	maxSounds         = 64
	sampleRate        = 22050
	fadeMillis        = 5
	maxPCMCacheBytes  = 8 << 20
)

var (
	soundMu         sync.Mutex
	clSounds        *clsnd.CLSounds
	pcmCache        = make(map[uint16][]byte)
	pcmCacheOrder   []uint16
	pcmCacheBytes   int
	audioCtx        *audio.Context
	soundPlayers    = make(map[*audio.Player]struct{})
	lastPlayedAt    = make(map[uint16]float64)
)

func initAudio() {
	audioCtx = audio.NewContext(sampleRate)
	log.Println("goDwarf: audio context initialized")
}

func loadCLSounds(snd *clsnd.CLSounds) {
	soundMu.Lock()
	clSounds = snd
	soundMu.Unlock()
	log.Println("goDwarf: CL_Sounds loaded")
}

func stopAllSounds() {
	soundMu.Lock()
	for p := range soundPlayers {
		_ = p.Close()
		delete(soundPlayers, p)
	}
	soundMu.Unlock()
}

func playSounds(ids []uint16) {
	if len(ids) == 0 || audioCtx == nil || settings.Mute {
		return
	}
	go func(ids []uint16) {
		soundMu.Lock()
		c := clSounds
		soundMu.Unlock()
		if c == nil {
			return
		}

		var valid map[uint16]struct{}
		vid := c.IDs()
		valid = make(map[uint16]struct{}, len(vid))
		for _, v := range vid {
			valid[uint16(v)] = struct{}{}
		}

		sounds := make([][]byte, 0, len(ids))
		seen := make(map[uint16]bool)
		maxSamples := 0
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, ok := valid[id]; !ok {
				continue
			}
			pcm := loadSound(id)
			if pcm == nil {
				continue
			}
			sounds = append(sounds, pcm)
			n := len(pcm) / 2
			if n > maxSamples {
				maxSamples = n
			}
		}
		if len(sounds) == 0 {
			return
		}

		mixed := make([]int32, maxSamples)
		for i := 0; i < maxSamples; i++ {
			var sum int32
			for _, pcm := range sounds {
				n := len(pcm) / 2
				if i < n {
					sample := int16(binary.LittleEndian.Uint16(pcm[2*i:]))
					sum += int32(sample)
				}
			}
			mixed[i] = sum
		}

		maxVal := int32(0)
		for _, v := range mixed {
			if v < 0 {
				v = -v
			}
			if v > maxVal {
				maxVal = v
			}
		}

		scale := 1.0 / float64(len(sounds))
		if maxVal > 0 {
			scale *= math.Min(1, 32767.0/float64(maxVal))
		}

		out := make([]byte, len(mixed)*4)
		for i, v := range mixed {
			lv := int32(float64(v) * scale)
			if lv > 32767 {
				lv = 32767
			} else if lv < -32768 {
				lv = -32768
			}
			off := 4 * i
			binary.LittleEndian.PutUint16(out[off:], uint16(int16(lv)))
			binary.LittleEndian.PutUint16(out[off+2:], uint16(int16(lv)))
		}

		out = applyFadeInOut(out, sampleRate, fadeMillis)

		p := audioCtx.NewPlayerFromBytes(out)
		vol := float64(settings.SoundVol) / 100.0
		p.SetVolume(vol)

		soundMu.Lock()
		for sp := range soundPlayers {
			if !sp.IsPlaying() {
				sp.Close()
				delete(soundPlayers, sp)
			}
		}
		if len(soundPlayers) >= maxSounds {
			soundMu.Unlock()
			p.Close()
			return
		}
		soundPlayers[p] = struct{}{}
		soundMu.Unlock()
		p.Play()
	}(ids)
}

func applyFadeInOut(pcm []byte, rate int, millis int) []byte {
	samples := len(pcm) / 2
	fadeFrames := rate * millis / 1000
	if fadeFrames*2 > samples {
		return pcm
	}

	factor := 1.0 / float64(fadeFrames)
	for i := 0; i < fadeFrames; i++ {
		f := float64(i) * factor
		off := i * 2
		s := int16(binary.LittleEndian.Uint16(pcm[off:]))
		s = int16(float64(s) * f)
		binary.LittleEndian.PutUint16(pcm[off:], uint16(s))
	}
	for i := 0; i < fadeFrames; i++ {
		f := float64(fadeFrames-1-i) * factor
		off := (samples - 1 - i) * 2
		s := int16(binary.LittleEndian.Uint16(pcm[off:]))
		s = int16(float64(s) * f)
		binary.LittleEndian.PutUint16(pcm[off:], uint16(s))
	}

	return pcm
}

func loadSound(id uint16) []byte {
	soundMu.Lock()
	if pcm, ok := pcmCache[id]; ok {
		soundMu.Unlock()
		return pcm
	}
	c := clSounds
	soundMu.Unlock()

	if c == nil {
		return nil
	}

	s, err := c.Get(uint32(id))
	if s == nil {
		if err != nil {
			log.Printf("goDwarf: sound %d decode error: %v", id, err)
		}
		soundMu.Lock()
		pcmCache[id] = nil
		soundMu.Unlock()
		return nil
	}

	srcRate := int(s.SampleRate)
	dstRate := sampleRate

	var samples []int16
	switch s.Bits {
	case 8:
		if s.Channels > 1 {
			frames := len(s.Data) / int(s.Channels)
			mono := make([]byte, frames)
			for i := 0; i < frames; i++ {
				mono[i] = s.Data[i*int(s.Channels)]
			}
			samples = u8ToS16TPDF(mono)
		} else {
			samples = u8ToS16TPDF(s.Data)
		}
	case 16:
		if s.Channels > 1 {
			frameSize := int(s.Channels) * 2
			frames := len(s.Data) / frameSize
			samples = make([]int16, frames)
			for i := 0; i < frames; i++ {
				off := i * frameSize
				samples[i] = int16(binary.BigEndian.Uint16(s.Data[off : off+2]))
			}
		} else {
			n := len(s.Data) / 2
			samples = make([]int16, n)
			for i := 0; i < n; i++ {
				samples[i] = int16(binary.BigEndian.Uint16(s.Data[2*i:]))
			}
		}
	default:
		return nil
	}

	if srcRate != dstRate && srcRate > 0 {
		samples = resampleLanczos(samples, srcRate, dstRate)
	}

	pcm := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(s))
	}

	soundMu.Lock()
	pcmCache[id] = pcm
	pcmCacheOrder = append(pcmCacheOrder, id)
	pcmCacheBytes += len(pcm)
	for pcmCacheBytes > maxPCMCacheBytes && len(pcmCacheOrder) > 1 {
		old := pcmCacheOrder[0]
		pcmCacheOrder = pcmCacheOrder[1:]
		if b, ok := pcmCache[old]; ok {
			pcmCacheBytes -= len(b)
			delete(pcmCache, old)
		}
	}
	soundMu.Unlock()
	return pcm
}

func u8ToS16TPDF(data []byte) []int16 {
	samples := make([]int16, len(data))
	for i, b := range data {
		samples[i] = (int16(b) - 128) << 8
	}
	return samples
}

func resampleLanczos(samples []int16, srcRate, dstRate int) []int16 {
	ratio := float64(dstRate) / float64(srcRate)
	newLen := int(float64(len(samples)) * ratio)
	if newLen <= 0 {
		return samples
	}

	const a = 4
	const pad = a
	padded := make([]int16, len(samples)+2*pad)
	for i := 0; i < pad; i++ {
		padded[i] = samples[0]
		padded[len(samples)+pad+i] = samples[len(samples)-1]
	}
	copy(padded[pad:], samples)

	resampled := make([]int16, newLen)
	for i := 0; i < newLen; i++ {
		srcPos := float64(i) / ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		var sum float64
		var wsum float64
		for k := -a + 1; k <= a; k++ {
			x := float64(k) - frac
			var w float64
			if x == 0 {
				w = 1.0
			} else {
				ax := math.Abs(x)
				if ax >= a {
					continue
				}
				px := math.Pi * x
				sincX := math.Sin(px) / px
				pax := math.Pi * ax / float64(a)
				sincA := math.Sin(pax) / pax
				w = sincX * sincA
			}
			sampleIdx := idx + k + pad
			if sampleIdx >= 0 && sampleIdx < len(padded) {
				sum += w * float64(padded[sampleIdx])
				wsum += w
			}
		}

		if wsum != 0 {
			v := sum / wsum
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			resampled[i] = int16(v)
		} else if idx >= 0 && idx < len(samples) {
			resampled[i] = samples[idx]
		}
	}

	return resampled
}
