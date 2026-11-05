package fm

import (
	"math"
	"sync/atomic"
)

const (
	MaxVoices = 8
	tableBits = 12
	tableSize = 1 << tableBits
	modDepth  = 6.0 // radians of phase modulation at modulator level 1
)

var sine [tableSize + 1]float32

func init() {
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * float64(i) / tableSize))
	}
}

// sin2pi returns sin(2*pi*x) for any x (phase in cycles), table + lerp.
func sin2pi(x float64) float64 {
	x -= math.Floor(x)
	f := x * tableSize
	i := int(f)
	fr := float32(f - float64(i))
	return float64(sine[i] + (sine[i+1]-sine[i])*fr)
}

type envStage uint8

const (
	stIdle envStage = iota
	stAttack
	stDecay
	stSustain
	stRelease
)

type env struct {
	stage envStage
	v     float64
	step  float64 // attack increment per sample
	mul   float64 // decay/release multiplier per sample
}

type voice struct {
	note  int
	vel   float64
	gate  bool
	age   uint64 // allocation order
	phase [4]float64
	inc   [4]float64
	envs  [4]env
	out   [4]float64
	fb    [2]float64
}

// event from the UI thread to the audio thread.
type event struct {
	on   bool
	note int
	vel  float64
}

// Engine renders polyphonic FM. NoteOn/NoteOff/SetPatch may be called from
// any goroutine; Render must be called from the audio thread only.
type Engine struct {
	rate    float64
	patch   atomic.Pointer[Patch]
	events  chan event
	voices  [MaxVoices]voice
	counter uint64
	Master  atomic.Uint32 // 0..1000 master gain (per mille)
	active  atomic.Int32
	gainCur float64
}

func NewEngine(rate int) *Engine {
	e := &Engine{rate: float64(rate), events: make(chan event, 256)}
	p := Init()
	e.patch.Store(&p)
	e.Master.Store(1000)
	return e
}

func (e *Engine) SetPatch(p Patch) {
	p.Sanitize()
	e.patch.Store(&p)
}

func (e *Engine) Patch() Patch { return *e.patch.Load() }

func (e *Engine) NoteOn(note int, vel float64) {
	select {
	case e.events <- event{true, note, vel}:
	default:
	}
}

func (e *Engine) NoteOff(note int) {
	select {
	case e.events <- event{false, note, 0}:
	default:
	}
}

// ActiveVoices is the number of sounding voices after the last Render.
func (e *Engine) ActiveVoices() int { return int(e.active.Load()) }

func noteHz(n int) float64 { return 440 * math.Pow(2, float64(n-69)/12) }

func (e *Engine) allocate(note int) *voice {
	// same note retriggers its own voice
	for i := range e.voices {
		v := &e.voices[i]
		if v.note == note && v.envs[0].stage != stIdle {
			return v
		}
	}
	// a free voice
	for i := range e.voices {
		if e.idle(&e.voices[i]) {
			return &e.voices[i]
		}
	}
	// the oldest released voice, else the oldest voice
	var best *voice
	for i := range e.voices {
		v := &e.voices[i]
		if !v.gate && (best == nil || v.age < best.age) {
			best = v
		}
	}
	if best != nil {
		return best
	}
	best = &e.voices[0]
	for i := range e.voices {
		if e.voices[i].age < best.age {
			best = &e.voices[i]
		}
	}
	return best
}

func (e *Engine) idle(v *voice) bool {
	for i := range v.envs {
		if v.envs[i].stage != stIdle {
			return false
		}
	}
	return true
}

func (e *Engine) start(p *Patch, ev event) {
	v := e.allocate(ev.note)
	fresh := e.idle(v)
	e.counter++
	v.note, v.vel, v.gate, v.age = ev.note, ev.vel, true, e.counter
	hz := noteHz(ev.note)
	for i := 0; i < 4; i++ {
		o := p.Ops[i]
		v.inc[i] = hz * o.Ratio * math.Pow(2, o.Detune/1200) / e.rate
		if fresh {
			v.phase[i] = 0
			v.envs[i].v = 0
		}
		// attack from the current level (no click when retriggering/stealing)
		v.envs[i].stage = stAttack
		v.envs[i].step = 1 / (o.Attack * e.rate)
	}
	if fresh {
		v.fb = [2]float64{}
	}
}

func (e *Engine) release(p *Patch, note int) {
	for i := range e.voices {
		v := &e.voices[i]
		if v.note == note && v.gate {
			v.gate = false
			for k := 0; k < 4; k++ {
				v.envs[k].stage = stRelease
				v.envs[k].mul = math.Exp(-6.9 / (p.Ops[k].Release * e.rate))
			}
		}
	}
}

func (en *env) tick(o *Op, rate float64) float64 {
	switch en.stage {
	case stAttack:
		en.v += en.step
		if en.v >= 1 {
			en.v = 1
			en.stage = stDecay
			en.mul = math.Exp(-6.9 / (o.Decay * rate))
		}
	case stDecay:
		en.v = o.Sustain + (en.v-o.Sustain)*en.mul
		if en.v-o.Sustain < 1e-4 {
			en.v = o.Sustain
			en.stage = stSustain
		}
	case stSustain:
		en.v = o.Sustain
	case stRelease:
		en.v *= en.mul
		if en.v < 1e-5 {
			en.v = 0
			en.stage = stIdle
		}
	}
	return en.v
}

// Render fills out (interleaved stereo, len = frames*2) with the mix.
func (e *Engine) Render(out []float32) {
	p := e.patch.Load()
	for {
		select {
		case ev := <-e.events:
			if ev.on {
				e.start(p, ev)
			} else {
				e.release(p, ev.note)
			}
			continue
		default:
		}
		break
	}
	alg := &Algorithms[p.Algo]
	master := float64(e.Master.Load()) / 1000
	gain := 0.35 * p.Level * master / math.Sqrt(float64(len(alg.Carriers)))
	g0 := e.gainCur // ramp the gain across the period: no clicks on volume changes
	e.gainCur = gain
	fbAmt := p.Feedback * 3
	for i := range out {
		out[i] = 0
	}
	frames := len(out) / 2
	active := 0
	for vi := range e.voices {
		v := &e.voices[vi]
		if e.idle(v) {
			continue
		}
		active++
		var amp [4]float64
		for k := 0; k < 4; k++ {
			o := &p.Ops[k]
			amp[k] = o.Level * (1 - o.VelSens + o.VelSens*v.vel)
		}
		for n := 0; n < frames; n++ {
			for k := 3; k >= 0; k-- {
				o := &p.Ops[k]
				ev := v.envs[k].tick(o, e.rate)
				mod := 0.0
				for _, m := range alg.Mods[k] {
					mod += v.out[m]
				}
				ph := v.phase[k] + mod*modDepth/(2*math.Pi)
				if k == 3 && fbAmt > 0 {
					ph += (v.fb[0] + v.fb[1]) * 0.5 * fbAmt / (2 * math.Pi)
				}
				s := sin2pi(ph) * ev * amp[k]
				if k == 3 {
					v.fb[1], v.fb[0] = v.fb[0], s
				}
				v.out[k] = s
				v.phase[k] += v.inc[k]
				if v.phase[k] >= 1 {
					v.phase[k] -= 1
				}
			}
			mix := 0.0
			for _, c := range alg.Carriers {
				mix += v.out[c]
			}
			s := float32(mix)
			out[2*n] += s
			out[2*n+1] += s
		}
	}
	// apply the ramped gain, then a gentle soft clip
	for n := 0; n < frames; n++ {
		g := float32(g0 + (gain-g0)*float64(n+1)/float64(frames))
		out[2*n] *= g
		out[2*n+1] *= g
	}
	for i, x := range out {
		out[i] = x / (1 + float32(math.Abs(float64(x)))*0.25)
	}
	e.active.Store(int32(active))
}
