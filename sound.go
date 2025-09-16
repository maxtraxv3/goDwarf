package main

import (
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"gothoom/clsnd"
)

const (
	maxSounds              = 64
	dbPad                  = -3
	enhancementTailSeconds = 0.45 // allow time for ambience tails to decay
	referenceSampleRate    = 48000.0
)

var (
	soundMu  sync.Mutex
	clSounds *clsnd.CLSounds
	pcmCache = make(map[uint16][]byte)

	audioContext   *audio.Context
	soundPlayers   = make(map[*audio.Player]struct{})
	notifPlayers   = make(map[*audio.Player]struct{})
	notifPlayersMu sync.Mutex

	sndDumpOnce   sync.Once
	sndDumpMu     sync.Mutex
	dumpedSndIDs  = make(map[uint16]struct{})
	sndMetaWriter *csv.Writer
)

// stopAllSounds halts and disposes all currently playing audio players.
func stopAllSounds() {
	soundMu.Lock()
	for sp := range soundPlayers {
		_ = sp.Close()
		delete(soundPlayers, sp)
	}
	soundMu.Unlock()

	notifPlayersMu.Lock()
	for sp := range notifPlayers {
		delete(notifPlayers, sp)
	}
	notifPlayersMu.Unlock()
}

// stopAllAudioPlayers stops and disposes every active audio player type.
func stopAllAudioPlayers() {
	stopAllSounds()
	stopAllTTS()
	stopAllMusic()
}

// playSound mixes the provided sound IDs and plays the result asynchronously.
// Each ID is loaded, mixed with simple clipping and then played at the current
// global volume. The function returns immediately after scheduling playback.
func playSound(ids []uint16) {
	if len(ids) == 0 || gs.Mute || focusMuted || !gs.GameSound {
		return
	}
	useEnhancement := gs.SoundEnhancement
	go func(ids []uint16, enableEnhancement bool) {
		if gs.Mute || focusMuted || !gs.GameSound {
			return
		}
		//logDebug("playSound %v called", ids)
		if blockSound {
			//logDebug("playSound blocked by blockSound")
			return
		}
		if audioContext == nil {
			logDebug("playSound no audio context")
			return
		}

		var valid map[uint16]struct{}
		soundMu.Lock()
		c := clSounds
		soundMu.Unlock()
		if c != nil {
			vid := c.IDs()
			valid = make(map[uint16]struct{}, len(vid))
			for _, v := range vid {
				valid[uint16(v)] = struct{}{}
			}
		}

		sounds := make([][]byte, 0, len(ids))
		maxSamples := 0
		for _, id := range ids {
			if valid != nil {
				if _, ok := valid[id]; !ok {
					logDebug("playSound unknown id %d", id)
					continue
				}
			}
			pcm := loadSound(id)
			if pcm == nil {
				continue
			}
			sounds = append(sounds, pcm)
			if n := len(pcm) / 2; n > maxSamples {
				maxSamples = n
			}
		}
		if len(sounds) == 0 {
			logDebug("playSound no pcm returned")
			return
		}

		mixSamples := maxSamples
		if mixSamples == 0 {
			return
		}

		tailSamples := 0
		if enableEnhancement {
			tailSamples = int(float64(audioContext.SampleRate()) * enhancementTailSeconds)
		}
		totalSamples := mixSamples + tailSamples
		mixed := make([]int32, totalSamples)

		chunks := runtime.NumCPU()
		if chunks > mixSamples {
			chunks = mixSamples
		}
		if chunks < 1 {
			chunks = 1
		}
		chunkSize := (mixSamples + chunks - 1) / chunks

		var wg sync.WaitGroup
		maxCh := make(chan int32, chunks)

		for start := 0; start < mixSamples; start += chunkSize {
			end := start + chunkSize
			if end > mixSamples {
				end = mixSamples
			}
			wg.Add(1)
			go func(start, end int) {
				defer wg.Done()
				localMax := int32(0)
				for i := start; i < end; i++ {
					var sum int32
					for _, pcm := range sounds {
						if n := len(pcm) / 2; i < n {
							sample := int16(binary.LittleEndian.Uint16(pcm[2*i:]))
							sum += int32(sample)
						}
					}
					mixed[i] = sum
					if sum < 0 {
						sum = -sum
					}
					if sum > localMax {
						localMax = sum
					}
				}
				maxCh <- localMax
			}(start, end)
		}
		wg.Wait()
		close(maxCh)

		left := mixed
		var right []int32
		if enableEnhancement {
			l, r := applyAudioEnhancement(mixed, audioContext.SampleRate())
			if len(l) == len(r) && len(l) > 0 {
				left = l
				right = r
			} else {
				enableEnhancement = false
				right = nil
			}
		}

		maxVal := int32(0)
		for v := range maxCh {
			if !enableEnhancement && v > maxVal {
				maxVal = v
			}
		}
		if enableEnhancement {
			for i := 0; i < len(left); i++ {
				v := left[i]
				if v < 0 {
					v = -v
				}
				if v > maxVal {
					maxVal = v
				}
				if right != nil {
					vr := right[i]
					if vr < 0 {
						vr = -vr
					}
					if vr > maxVal {
						maxVal = vr
					}
				}
			}
		}

		// Apply peak normalization and reduce volume for overlapping sounds
		scale := 1 / float64(len(sounds))
		if maxVal > 0 {
			scale *= math.Min(1, 32767.0/float64(maxVal))
		}

		var out []byte
		if enableEnhancement {
			out = make([]byte, len(left)*4)
		} else {
			out = make([]byte, len(left)*2)
		}

		wg = sync.WaitGroup{}
		for start := 0; start < len(left); start += chunkSize {
			end := start + chunkSize
			if end > len(left) {
				end = len(left)
			}
			wg.Add(1)
			go func(start, end int, stereo bool) {
				defer wg.Done()
				for i := start; i < end; i++ {
					lv := int32(float64(left[i]) * scale)
					if lv > 32767 {
						lv = 32767
					} else if lv < -32768 {
						lv = -32768
					}
					if stereo {
						rv := lv
						if right != nil {
							rv = int32(float64(right[i]) * scale)
							if rv > 32767 {
								rv = 32767
							} else if rv < -32768 {
								rv = -32768
							}
						}
						off := 4 * i
						binary.LittleEndian.PutUint16(out[off:], uint16(int16(lv)))
						binary.LittleEndian.PutUint16(out[off+2:], uint16(int16(rv)))
					} else {
						binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(lv)))
					}
				}
			}(start, end, enableEnhancement)
		}
		wg.Wait()

		p := audioContext.NewPlayerFromBytes(out)
		vol := gs.MasterVolume * gs.GameVolume
		if gs.Mute || focusMuted {
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
			logDebug("playSound too many sound players (%d)", len(soundPlayers))
			p.Close()
			return
		}
		soundPlayers[p] = struct{}{}
		soundMu.Unlock()

		//logDebug("playSound playing")
		p.Play()
	}(ids, useEnhancement)
}

// initSoundContext initializes the global audio context.
func initSoundContext() {
	rate := sampleRate
	audioContext = audio.NewContext(rate)
}

func updateSoundVolume() {
	gameVol := gs.MasterVolume * gs.GameVolume
	ttsVol := gs.MasterVolume * gs.ChatTTSVolume
	musicVol := gs.MasterVolume * gs.MusicVolume
	notifVol := gs.MasterVolume * gs.NotificationVolume
	if !gs.GameSound {
		gameVol = 0
		notifVol = 0
	}
	if !gs.ChatTTS {
		ttsVol = 0
	}
	if !gs.Music {
		musicVol = 0
	}
	if !gs.NotificationBeep {
		notifVol = 0
	}
	if gs.Mute || focusMuted {
		gameVol = 0
		ttsVol = 0
		musicVol = 0
		notifVol = 0
	}

	soundMu.Lock()
	players := make([]*audio.Player, 0, len(soundPlayers))
	for sp := range soundPlayers {
		players = append(players, sp)
	}
	soundMu.Unlock()

	notifPlayersMu.Lock()
	notif := make(map[*audio.Player]struct{}, len(notifPlayers))
	for sp := range notifPlayers {
		notif[sp] = struct{}{}
	}
	notifPlayersMu.Unlock()

	ttsPlayersMu.Lock()
	tts := make([]*audio.Player, 0, len(ttsPlayers))
	for p := range ttsPlayers {
		tts = append(tts, p)
	}
	ttsPlayersMu.Unlock()

	musicPlayersMu.Lock()
	music := make([]*audio.Player, 0, len(musicPlayers))
	for p := range musicPlayers {
		music = append(music, p)
	}
	musicPlayersMu.Unlock()

	stopped := make([]*audio.Player, 0)
	notifStopped := make([]*audio.Player, 0)
	for _, sp := range players {
		if sp.IsPlaying() {
			if _, ok := notif[sp]; ok {
				sp.SetVolume(notifVol)
			} else {
				sp.SetVolume(gameVol)
			}
		} else {
			stopped = append(stopped, sp)
			if _, ok := notif[sp]; ok {
				notifStopped = append(notifStopped, sp)
			}
		}
	}

	ttsStopped := make([]*audio.Player, 0)
	for _, p := range tts {
		if p.IsPlaying() {
			p.SetVolume(ttsVol)
		} else {
			ttsStopped = append(ttsStopped, p)
		}
	}

	musicStopped := make([]*audio.Player, 0)
	for _, p := range music {
		if p.IsPlaying() {
			p.SetVolume(musicVol)
		} else {
			musicStopped = append(musicStopped, p)
		}
	}

	if len(stopped) > 0 {
		soundMu.Lock()
		for _, sp := range stopped {
			delete(soundPlayers, sp)
			sp.Close()
		}
		soundMu.Unlock()
	}

	if len(notifStopped) > 0 {
		notifPlayersMu.Lock()
		for _, sp := range notifStopped {
			delete(notifPlayers, sp)
		}
		notifPlayersMu.Unlock()
	}

	if len(ttsStopped) > 0 {
		ttsPlayersMu.Lock()
		for _, p := range ttsStopped {
			delete(ttsPlayers, p)
			p.Close()
		}
		ttsPlayersMu.Unlock()
	}

	if len(musicStopped) > 0 {
		musicPlayersMu.Lock()
		for _, p := range musicStopped {
			delete(musicPlayers, p)
			p.Close()
		}
		musicPlayersMu.Unlock()
	}
}

// fast xorshift32 PRNG
type rnd32 uint32

func (r *rnd32) next() float64 {
	x := uint32(*r)
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	*r = rnd32(x)
	// scale to [0,1)
	return float64(x) * (1.0 / 4294967296.0)
}

// u8 PCM (0..255) -> s16 PCM (-32768..32767) with TPDF dither and 257 scaling
func u8ToS16TPDF(data []byte, seed uint32) []int16 {
	out := make([]int16, len(data))
	r1, r2 := rnd32(seed|1), rnd32(seed*1664525+1013904223)

	for i, b := range data {
		// TPDF dither in [-0.5, +0.5): (rand - rand)
		noise := (r1.next() - r2.next()) * 0.5
		v := float64(b) + noise

		// Map 0..255 -> -32768..32767 using *257 then offset
		// (257 uses full 16-bit span slightly better than <<8)
		s := int32(math.Round(v*257.0)) - 32768
		if s > math.MaxInt16 {
			s = math.MaxInt16
		} else if s < math.MinInt16 {
			s = math.MinInt16
		}
		out[i] = int16(s)
	}
	return out
}

func u8ToS16Fast(data []byte) []int16 {
	out := make([]int16, len(data))
	for i, b := range data {
		v := int32(b)*257 - 32768
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		out[i] = int16(v)
	}
	return out
}

func ResampleLinearInt16(src []int16, srcRate, dstRate int) []int16 {
	if len(src) == 0 {
		return nil
	}
	if srcRate <= 0 || dstRate <= 0 || srcRate == dstRate {
		out := make([]int16, len(src))
		copy(out, src)
		return out
	}

	n := int(math.Round(float64(len(src)) * float64(dstRate) / float64(srcRate)))
	if n < 1 {
		n = 1
	}
	out := make([]int16, n)
	step := float64(srcRate) / float64(dstRate)
	pos := 0.0
	lastIdx := len(src) - 1
	for i := 0; i < n; i++ {
		idx := int(pos)
		if idx > lastIdx {
			idx = lastIdx
		}
		frac := pos - float64(idx)
		s0 := float64(src[idx])
		var s1 float64
		if idx < lastIdx {
			s1 = float64(src[idx+1])
		} else {
			s1 = s0
		}
		out[i] = int16(math.Round(s0 + (s1-s0)*frac))
		pos += step
	}
	return out
}

// applyFadeInOut applies a tiny fade to the start and end of the samples
// to avoid clicks when sounds begin or end abruptly. The fade length is
// approximately 5ms of audio.
func applyFadeInOut(samples []int16, rate int) {
	fade := 220
	if fade <= 1 {
		return
	}
	if len(samples) < 2*fade {
		fade = len(samples) / 2
		if fade <= 1 {
			return
		}
	}
	for i := 0; i < fade; i++ {
		inScale := float64(i) / float64(fade)
		samples[i] = int16(float64(samples[i]) * inScale)
		outScale := float64(fade-1-i) / float64(fade)
		idx := len(samples) - fade + i
		samples[idx] = int16(float64(samples[idx]) * outScale)

	}
}

// applyAudioEnhancement transforms the mono mix into a wider stereo field while
// adding subtle ambience, tone shaping, and dynamics control suitable for
// open-air sound effects.
func applyAudioEnhancement(mono []int32, rate int) ([]int32, []int32) {
	if len(mono) == 0 || rate <= 0 {
		return nil, nil
	}

	n := len(mono)
	base := make([]float64, n)
	for i, v := range mono {
		base[i] = float64(v)
	}

	left := make([]float64, n)
	right := make([]float64, n)

	copy(left, base)
	delaySamples := int(math.Round(float64(rate) * 0.00032))
	if delaySamples < 1 {
		delaySamples = 1
	}
	first := base[0]
	for i := 0; i < n; i++ {
		idx := i - delaySamples
		if idx >= 0 {
			right[i] = base[idx]
		} else {
			right[i] = first
		}
	}

	applyAllPassInPlace(left, 3, 0.55)
	applyAllPassInPlace(right, 5, 0.5)

	wetLeft := buildMicroAmbience(base, rate, 0)
	wetRight := buildMicroAmbience(base, rate, 23)
	const wetMix = 0.14
	const dryGain = 0.9
	for i := 0; i < n; i++ {
		left[i] = left[i]*dryGain + wetLeft[i]*wetMix
		right[i] = right[i]*dryGain + wetRight[i]*wetMix
	}

	const crossfeed = 0.08
	if crossfeed > 0 {
		for i := 0; i < n; i++ {
			sum := (left[i] + right[i]) * 0.5
			left[i] = left[i]*(1-crossfeed) + sum*crossfeed
			right[i] = right[i]*(1-crossfeed) + sum*crossfeed
		}
	}

	applySlapDelay(left, rate, 0.024, 0.07, 0.06)
	applySlapDelay(right, rate, 0.027, 0.07, 0.06)

	applyTiltEQ(left, rate)
	applyTiltEQ(right, rate)

	applySaturation(left, 1.6, 0.35)
	applySaturation(right, 1.6, 0.35)

	applyDownwardExpander(left, dbToLinear(-45), 1.4)
	applyDownwardExpander(right, dbToLinear(-45), 1.4)

	outL := make([]int32, n)
	outR := make([]int32, n)
	for i := 0; i < n; i++ {
		outL[i] = int32(math.Round(left[i]))
		outR[i] = int32(math.Round(right[i]))
	}
	return outL, outR
}

func applyAllPassInPlace(samples []float64, delay int, gain float64) {
	if delay <= 0 || len(samples) == 0 {
		return
	}
	if gain >= 0.999 {
		gain = 0.999
	} else if gain <= -0.999 {
		gain = -0.999
	}
	buf := make([]float64, delay)
	idx := 0
	for i := 0; i < len(samples); i++ {
		input := samples[i]
		delayed := buf[idx]
		output := -gain*input + delayed
		buf[idx] = input + gain*output
		samples[i] = output
		idx++
		if idx >= delay {
			idx = 0
		}
	}
}

func buildMicroAmbience(input []float64, rate int, offset int) []float64 {
	n := len(input)
	out := make([]float64, n)
	if n == 0 || rate <= 0 {
		return out
	}

	baseDelays := []int{1137, 1277, 1429, 1613}
	rt60 := 0.33
	lpCoef := lowpassCoefficient(rate, 7200)

	type comb struct {
		buf   []float64
		idx   int
		fb    float64
		state float64
	}

	combs := make([]comb, 0, len(baseDelays))
	for i, base := range baseDelays {
		adj := base
		if offset != 0 && i%2 == 1 {
			adj += offset
		}
		delay := scaleDelaySamples(adj, rate)
		if delay < 1 {
			continue
		}
		fb := math.Exp(-3 * (float64(delay) / float64(rate)) / rt60)
		combs = append(combs, comb{buf: make([]float64, delay), fb: fb})
	}

	for i := 0; i < n; i++ {
		in := input[i]
		wet := 0.0
		for j := range combs {
			c := &combs[j]
			delayed := c.buf[c.idx]
			c.state += lpCoef * (delayed - c.state)
			wet += c.state
			c.buf[c.idx] = in + c.state*c.fb
			c.idx++
			if c.idx >= len(c.buf) {
				c.idx = 0
			}
		}
		if len(combs) > 0 {
			out[i] = wet / float64(len(combs))
		}
	}

	apDelays := []int{149, 211}
	for i, base := range apDelays {
		adj := base
		if offset != 0 && i%2 == 0 {
			adj += offset / 2
		}
		delay := scaleDelaySamples(adj, rate)
		if delay > 0 {
			applyAllPassInPlace(out, delay, 0.5)
		}
	}

	return out
}

func scaleDelaySamples(base int, rate int) int {
	if base <= 0 {
		return 0
	}
	if rate <= 0 {
		return base
	}
	scaled := int(math.Round(float64(base) * float64(rate) / referenceSampleRate))
	if scaled < 1 {
		scaled = 1
	}
	return scaled
}

func lowpassCoefficient(rate int, cutoff float64) float64 {
	if rate <= 0 || cutoff <= 0 {
		return 1
	}
	omega := 2 * math.Pi * cutoff / float64(rate)
	if omega > math.Pi {
		omega = math.Pi
	}
	return 1 - math.Exp(-omega)
}

func applySlapDelay(samples []float64, rate int, delaySec, feedback, mix float64) {
	if rate <= 0 || len(samples) == 0 || delaySec <= 0 || mix <= 0 {
		return
	}
	delay := int(math.Round(delaySec * float64(rate)))
	if delay < 1 {
		delay = 1
	}
	buf := make([]float64, delay)
	coef := lowpassCoefficient(rate, 7000)
	idx := 0
	var state float64
	for i := 0; i < len(samples); i++ {
		delayed := buf[idx]
		state += coef * (delayed - state)
		buf[idx] = samples[i] + state*feedback
		samples[i] += state * mix
		idx++
		if idx >= delay {
			idx = 0
		}
	}
}

func applyTiltEQ(samples []float64, rate int) {
	if rate <= 0 || len(samples) == 0 {
		return
	}
	fs := float64(rate)
	low := newLowShelf(fs, 320, 1.5)
	high := newHighShelf(fs, 4800, -1.0)
	if low != nil {
		low.process(samples)
	}
	if high != nil {
		high.process(samples)
	}
}

type biquad struct {
	b0, b1, b2 float64
	a1, a2     float64
	z1, z2     float64
}

func (b *biquad) process(samples []float64) {
	if b == nil {
		return
	}
	for i := range samples {
		in := samples[i]
		out := in*b.b0 + b.z1
		b.z1 = in*b.b1 + b.z2 - b.a1*out
		b.z2 = in*b.b2 - b.a2*out
		samples[i] = out
	}
}

func newBiquad(b0, b1, b2, a0, a1, a2 float64) *biquad {
	if a0 == 0 || math.IsNaN(a0) || math.IsInf(a0, 0) {
		return nil
	}
	invA0 := 1 / a0
	return &biquad{
		b0: b0 * invA0,
		b1: b1 * invA0,
		b2: b2 * invA0,
		a1: a1 * invA0,
		a2: a2 * invA0,
	}
}

func newLowShelf(fs, freq, gainDB float64) *biquad {
	if fs <= 0 || freq <= 0 {
		return nil
	}
	if freq >= fs/2 {
		freq = fs/2 - 1
		if freq <= 0 {
			freq = fs / 4
		}
	}
	A := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / fs
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / math.Sqrt2
	sqrtA := math.Sqrt(A)
	beta := 2 * sqrtA * alpha

	b0 := A * ((A + 1) - (A-1)*cosW0 + beta)
	b1 := 2 * A * ((A - 1) - (A+1)*cosW0)
	b2 := A * ((A + 1) - (A-1)*cosW0 - beta)
	a0 := (A + 1) + (A-1)*cosW0 + beta
	a1 := -2 * ((A - 1) + (A+1)*cosW0)
	a2 := (A + 1) + (A-1)*cosW0 - beta

	return newBiquad(b0, b1, b2, a0, a1, a2)
}

func newHighShelf(fs, freq, gainDB float64) *biquad {
	if fs <= 0 || freq <= 0 {
		return nil
	}
	if freq >= fs/2 {
		freq = fs/2 - 1
		if freq <= 0 {
			freq = fs / 4
		}
	}
	A := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / fs
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / math.Sqrt2
	sqrtA := math.Sqrt(A)
	beta := 2 * sqrtA * alpha

	b0 := A * ((A + 1) + (A-1)*cosW0 + beta)
	b1 := -2 * A * ((A - 1) + (A+1)*cosW0)
	b2 := A * ((A + 1) + (A-1)*cosW0 - beta)
	a0 := (A + 1) - (A-1)*cosW0 + beta
	a1 := 2 * ((A - 1) - (A+1)*cosW0)
	a2 := (A + 1) - (A-1)*cosW0 - beta

	return newBiquad(b0, b1, b2, a0, a1, a2)
}

func applySaturation(samples []float64, drive, mix float64) {
	if drive <= 0 || len(samples) == 0 {
		return
	}
	if mix < 0 {
		mix = 0
	} else if mix > 1 {
		mix = 1
	}
	const toFloat = 1.0 / 32768.0
	const fromFloat = 32768.0
	dryMix := 1 - mix
	norm := math.Tanh(drive)
	if norm == 0 {
		norm = 1
	}
	for i := range samples {
		x := samples[i] * toFloat
		sat := math.Tanh(x*drive) / norm
		samples[i] = ((dryMix * x) + (mix * sat)) * fromFloat
	}
}

func applyDownwardExpander(samples []float64, threshold float64, ratio float64) {
	if len(samples) == 0 || threshold <= 0 || ratio <= 1 {
		return
	}
	const toFloat = 1.0 / 32768.0
	const fromFloat = 32768.0
	for i := range samples {
		x := samples[i] * toFloat
		ax := math.Abs(x)
		if ax < threshold {
			if ax < 1e-6 {
				samples[i] = 0
				continue
			}
			gain := math.Pow(ax/threshold, ratio-1)
			x *= gain
		}
		samples[i] = x * fromFloat
	}
}

func dbToLinear(db float64) float64 {
	return math.Pow(10, db/20)
}

// loadSound retrieves a sound by ID, resamples it to match the audio context's
// sample rate, and caches the resulting PCM bytes. The CL_Sounds archive is
// opened on first use and individual sounds are parsed lazily.
func loadSound(id uint16) []byte {
	//logDebug("loadSound(%d) called", id)
	if audioContext == nil {
		logDebug("loadSound(%d) no audio context", id)
		return nil
	}

	soundMu.Lock()
	if pcm, ok := pcmCache[id]; ok {
		soundMu.Unlock()
		if pcm == nil {
			logDebug("loadSound(%d) cached as missing", id)
		} else {
			//logDebug("loadSound(%d) cache hit (%d bytes)", id, len(pcm))
		}
		return pcm
	}
	c := clSounds
	soundMu.Unlock()

	if c == nil {
		logDebug("loadSound(%d) CL sounds not loaded", id)
		return nil
	}

	//logDebug("loadSound(%d) fetching from archive", id)
	var t0 time.Time
	if measureLoads {
		t0 = time.Now()
	}
	s, err := c.Get(uint32(id))
	if s == nil {
		if err != nil {
			logError("unable to decode sound %d: %v", id, err)
		} else {
			logError("missing sound %d", id)
		}
		soundMu.Lock()
		pcmCache[id] = nil
		soundMu.Unlock()
		return nil
	}
	statSoundLoaded(id)
	//logDebug("loadSound(%d) loaded %d Hz %d-bit %d bytes", id, s.SampleRate, s.Bits, len(s.Data))

	srcRate := int(s.SampleRate / 2)
	dstRate := audioContext.SampleRate()

	// Decode the sound data into 16-bit samples.
	var samples []int16
	useHighQuality := gs.HighQualityResampling
	switch s.Bits {
	case 8:
		if useHighQuality {
			if s.Channels > 1 {
				frames := len(s.Data) / int(s.Channels)
				mono := make([]byte, frames)
				for i := 0; i < frames; i++ {
					mono[i] = s.Data[i*int(s.Channels)]
				}
				samples = u8ToS16TPDF(mono, 0xC0FFEE)
			} else {
				samples = u8ToS16TPDF(s.Data, 0xC0FFEE)
			}
		} else {
			if s.Channels > 1 {
				frames := len(s.Data) / int(s.Channels)
				mono := make([]byte, frames)
				for i := 0; i < frames; i++ {
					mono[i] = s.Data[i*int(s.Channels)]
				}
				samples = u8ToS16Fast(mono)
			} else {
				samples = u8ToS16Fast(s.Data)
			}
		}
	case 16:
		if len(s.Data)%2 != 0 {
			s.Data = append(s.Data, 0x00)
		}
		if s.Channels > 1 {
			frameSize := int(s.Channels) * 2
			frames := len(s.Data) / frameSize
			samples = make([]int16, frames)
			for i := 0; i < frames; i++ {
				off := i * frameSize
				samples[i] = int16(binary.BigEndian.Uint16(s.Data[off : off+2]))
			}
		} else {
			samples = make([]int16, len(s.Data)/2)
			for i := 0; i < len(samples); i++ {
				samples[i] = int16(binary.BigEndian.Uint16(s.Data[2*i : 2*i+2]))
			}
		}
	default:
		log.Fatalf("Invalid number of bits: %v: ID: %v", s.Bits, id)
	}

	if srcRate != dstRate {
		if useHighQuality {
			samples = ResampleLanczosInt16PadDB(samples, srcRate, dstRate, dbPad)
		} else {
			samples = ResampleLinearInt16(samples, srcRate, dstRate)
			samples = PadDB(samples, dbPad)
		}
	} else {
		samples = PadDB(samples, dbPad)
	}
	defer putInt16Buf(samples) // return pooled buffer

	applyFadeInOut(samples, dstRate)

	pcm := make([]byte, len(samples)*2)
	for i, v := range samples {
		pcm[2*i] = byte(v)
		pcm[2*i+1] = byte(v >> 8)
	}

	if sndDump {
		dumpSound(id, s, pcm, dstRate)
	}

	soundMu.Lock()
	pcmCache[id] = pcm
	soundMu.Unlock()
	//logDebug("loadSound(%d) cached %d bytes", id, len(pcm))
	if measureLoads && !t0.IsZero() {
		dtms := float64(time.Since(t0).Nanoseconds()) / 1e6
		log.Printf("measure: sound id=%d rate=%dHz bits=%d ch=%d load=%.2fms frame=%d", id, s.SampleRate, s.Bits, s.Channels, dtms, frameCounter)
	}
	return pcm
}

func dumpSound(id uint16, s *clsnd.Sound, pcm []byte, rate int) {
	sndDumpOnce.Do(func() {
		os.MkdirAll(filepath.Join("dump", "snd"), 0755)
		if f, err := os.Create(filepath.Join("dump", "snd", "metadata.csv")); err == nil {
			sndMetaWriter = csv.NewWriter(f)
			sndMetaWriter.Write([]string{"id", "origRate", "origChannels", "origBits", "bytes"})
		}
	})
	sndDumpMu.Lock()
	if _, ok := dumpedSndIDs[id]; ok {
		sndDumpMu.Unlock()
		return
	}
	dumpedSndIDs[id] = struct{}{}
	sndDumpMu.Unlock()

	fn := filepath.Join("dump", "snd", fmt.Sprintf("%d.wav", id))
	f, err := os.Create(fn)
	if err != nil {
		log.Printf("dump sound %d: %v", id, err)
		return
	}
	defer f.Close()

	dataLen := uint32(len(pcm))
	var header [44]byte
	copy(header[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:], 36+dataLen)
	copy(header[8:], []byte("WAVE"))
	copy(header[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:], dataLen)

	if _, err := f.Write(header[:]); err != nil {
		log.Printf("dump sound %d header: %v", id, err)
		return
	}
	if _, err := f.Write(pcm); err != nil {
		log.Printf("dump sound %d data: %v", id, err)
		return
	}

	if sndMetaWriter != nil {
		sndMetaWriter.Write([]string{
			strconv.Itoa(int(id)),
			strconv.FormatUint(uint64(s.SampleRate), 10),
			strconv.FormatUint(uint64(s.Channels), 10),
			strconv.FormatUint(uint64(s.Bits), 10),
			strconv.Itoa(len(pcm)),
		})
		sndMetaWriter.Flush()
	}
}

// soundCacheStats returns the number of cached sounds and total bytes used.
func soundCacheStats() (count, bytes int) {
	soundMu.Lock()
	defer soundMu.Unlock()
	for _, pcm := range pcmCache {
		if pcm != nil {
			count++
			bytes += len(pcm)
		}
	}
	return
}
