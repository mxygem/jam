package playback

import (
	"encoding/binary"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2/audio"
)

// PCM is mono float audio at SampleRate.
type PCM []float32

// NewPCM allocates seconds of silence.
func NewPCM(seconds float64) PCM { return make(PCM, int(seconds*SampleRate)) }

// Add mixes src into p starting at t seconds.
func (p PCM) Add(t float64, src PCM, gain float32) {
	off := int(t * SampleRate)
	for i, v := range src {
		j := off + i
		if j < 0 {
			continue
		}
		if j >= len(p) {
			break
		}
		p[j] += v * gain
	}
}

// Bytes encodes as 16-bit little endian stereo for an Ebitengine player.
func (p PCM) Bytes() []byte {
	out := make([]byte, len(p)*4)
	for i, v := range p {
		s := int16(math.Max(-1, math.Min(1, float64(v))) * 32767)
		binary.LittleEndian.PutUint16(out[i*4:], uint16(s))
		binary.LittleEndian.PutUint16(out[i*4+2:], uint16(s))
	}
	return out
}

// WAV encodes as a 16-bit mono WAV file.
func (p PCM) WAV() []byte {
	data := len(p) * 2
	b := make([]byte, 44+data)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+data))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], SampleRate)
	binary.LittleEndian.PutUint32(b[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(data))
	for i, v := range p {
		s := int16(math.Max(-1, math.Min(1, float64(v))) * 32767)
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(s))
	}
	return b
}

// Click is a short metronome tick.
func Click(accent bool) PCM {
	n := int(0.03 * SampleRate)
	p := make(PCM, n)
	f := 1500.0
	if accent {
		f = 2200
	}
	for i := range p {
		t := float64(i) / SampleRate
		p[i] = float32(math.Sin(2*math.Pi*f*t) * math.Exp(-t*160) * 0.9)
	}
	return p
}

// MissSound is a dull muted-string thunk played on overstrums.
func MissSound() PCM {
	n := int(0.12 * SampleRate)
	p := make(PCM, n)
	r := rand.New(rand.NewSource(7))
	lp := 0.0
	for i := range p {
		t := float64(i) / SampleRate
		lp += (r.Float64()*2 - 1 - lp) * 0.08
		p[i] = float32((lp*1.6 + math.Sin(2*math.Pi*110*t)*0.5) * math.Exp(-t*35) * 0.6)
	}
	return p
}

// Tone is a plucked/saw-ish note for the demo song.
func Tone(freq, dur float64, kind int) PCM {
	n := int(dur * SampleRate)
	p := make(PCM, n)
	for i := range p {
		t := float64(i) / SampleRate
		env := math.Min(1, t*200) * math.Exp(-t*1.2)
		rel := math.Min(1, (dur-t)*30)
		var v float64
		switch kind {
		case 0: // lead: detuned saws through soft clipping
			ph1 := math.Mod(t*freq, 1)*2 - 1
			ph2 := math.Mod(t*freq*1.006, 1)*2 - 1
			v = math.Tanh((ph1 + ph2) * 1.4)
		case 1: // bass: sine + a little square
			v = math.Sin(2*math.Pi*freq*t) + 0.25*math.Copysign(1, math.Sin(2*math.Pi*freq*t))
			env = math.Min(1, t*300) * math.Exp(-t*2.5)
		default: // pad
			v = math.Sin(2*math.Pi*freq*t) + 0.5*math.Sin(2*math.Pi*freq*2*t)
		}
		p[i] = float32(v * env * rel * 0.35)
	}
	return p
}

// Kick, Snare, HiHat and Crash are simple drum voices.
func Kick() PCM {
	n := int(0.3 * SampleRate)
	p := make(PCM, n)
	ph := 0.0
	for i := range p {
		t := float64(i) / SampleRate
		f := 50 + 110*math.Exp(-t*30)
		ph += 2 * math.Pi * f / SampleRate
		p[i] = float32(math.Sin(ph) * math.Exp(-t*9) * 0.9)
	}
	return p
}

func noise(dur, decay, hp float64, seed int64, gain float64) PCM {
	n := int(dur * SampleRate)
	p := make(PCM, n)
	r := rand.New(rand.NewSource(seed))
	prev := 0.0
	for i := range p {
		t := float64(i) / SampleRate
		x := r.Float64()*2 - 1
		y := x - prev*hp
		prev = x
		p[i] = float32(y * math.Exp(-t*decay) * gain)
	}
	return p
}

func Snare() PCM {
	p := noise(0.22, 18, 0.5, 1, 0.55)
	for i := range p {
		t := float64(i) / SampleRate
		p[i] += float32(math.Sin(2*math.Pi*190*t) * math.Exp(-t*25) * 0.4)
	}
	return p
}

func HiHat(open bool) PCM {
	if open {
		return noise(0.35, 7, 0.95, 2, 0.28)
	}
	return noise(0.06, 60, 0.95, 3, 0.3)
}

func Crash() PCM { return noise(1.4, 2.2, 0.9, 4, 0.32) }

func TomVoice(f float64) PCM {
	n := int(0.35 * SampleRate)
	p := make(PCM, n)
	for i := range p {
		t := float64(i) / SampleRate
		p[i] = float32(math.Sin(2*math.Pi*(f+40*math.Exp(-t*20))*t) * math.Exp(-t*8) * 0.6)
	}
	return p
}

// SFX plays short one-shot sounds.
type SFX struct {
	ctx    *audio.Context
	miss   []byte
	Silent bool
}

// NewSFX prepares sound effects.
func NewSFX(ctx *audio.Context) *SFX {
	return &SFX{ctx: ctx, miss: MissSound().Bytes(), Silent: ctx == nil}
}

// Miss plays the overstrum sound.
func (s *SFX) Miss(vol float64) {
	if s.Silent {
		return
	}
	p := s.ctx.NewPlayerFromBytes(s.miss)
	p.SetVolume(vol)
	p.Play()
}

// Play plays arbitrary PCM.
func (s *SFX) Play(p PCM, vol float64) *audio.Player {
	if s.Silent {
		return nil
	}
	pl := s.ctx.NewPlayerFromBytes(p.Bytes())
	pl.SetVolume(vol)
	pl.Play()
	return pl
}
