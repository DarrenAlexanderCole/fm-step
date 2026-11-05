package fm

import (
	"math"
	"path/filepath"
	"testing"
)

func render(e *Engine, frames int) []float32 {
	out := make([]float32, frames*2)
	buf := make([]float32, 256*2)
	for i := 0; i < frames; i += 256 {
		e.Render(buf)
		copy(out[i*2:], buf)
	}
	return out
}

func peak(b []float32) float64 {
	m := 0.0
	for _, x := range b {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return math.Inf(1)
		}
		m = math.Max(m, math.Abs(float64(x)))
	}
	return m
}

func TestEveryFactoryPatchAndAlgorithmSounds(t *testing.T) {
	for _, p := range Factory {
		for a := range Algorithms {
			p.Algo = a
			e := NewEngine(48000)
			e.SetPatch(p)
			e.NoteOn(60, 1)
			out := render(e, 4800)
			pk := peak(out)
			if pk <= 0.001 || pk > 1.0 {
				t.Errorf("%s algo %d: peak %v", p.Name, a, pk)
			}
		}
	}
}

func TestReleaseGoesSilentAndFreesVoice(t *testing.T) {
	e := NewEngine(48000)
	p := Factory[0]
	e.SetPatch(p)
	e.NoteOn(60, 1)
	render(e, 4800)
	e.NoteOff(60)
	render(e, 48000*3)
	if pk := peak(render(e, 256)); pk > 1e-4 {
		t.Fatalf("still sounding after release: %v", pk)
	}
	if e.ActiveVoices() != 0 {
		t.Fatalf("voices still active: %d", e.ActiveVoices())
	}
}

func TestPolyphonyAndStealing(t *testing.T) {
	e := NewEngine(48000)
	e.SetPatch(Factory[5]) // pad, long sustain
	for n := 0; n < 12; n++ {
		e.NoteOn(48+n, 0.8)
	}
	out := render(e, 2400)
	if e.ActiveVoices() != MaxVoices {
		t.Fatalf("active=%d want %d", e.ActiveVoices(), MaxVoices)
	}
	if pk := peak(out); pk > 1.0 {
		t.Fatalf("12-note chord clipped: %v", pk)
	}
}

func TestPresetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Factory[2]
	p.Name = "MY BELL"
	if err := SaveFile(SlotPath(dir, 9), p); err != nil {
		t.Fatal(err)
	}
	q, user := LoadSlot(dir, 9)
	if !user || q.Name != "MY BELL" || q.Ops[1].Ratio != p.Ops[1].Ratio {
		t.Fatalf("round trip: %+v", q)
	}
	if f, user := LoadSlot(dir, 3); user || f.Name != "BELL" {
		t.Fatalf("factory fallback: %+v", f)
	}
	if i, _ := LoadSlot(dir, 12); i.Name != "INIT" {
		t.Fatalf("init fallback: %+v", i)
	}
	if _, err := LoadFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected error")
	}
}

func TestSanitize(t *testing.T) {
	p := Patch{Algo: 99, Feedback: 7, Ops: [4]Op{{Ratio: -1, Attack: 0, Level: math.NaN()}}}
	p.Sanitize()
	if p.Algo != 0 || p.Feedback != 1 || p.Ops[0].Ratio <= 0 || p.Ops[0].Attack <= 0 || p.Ops[0].Level != 0 {
		t.Fatalf("%+v", p)
	}
}

// 8 voices, 4 operators, one 256-frame period. Real-time budget at 48 kHz is
// 5.33 ms; the Cortex-A55 is several times slower than a desktop core.
func BenchmarkPeriod8Voices(b *testing.B) {
	e := NewEngine(48000)
	e.SetPatch(Factory[5])
	for n := 0; n < 8; n++ {
		e.NoteOn(48+n*3, 0.8)
	}
	buf := make([]float32, 512)
	for i := 0; i < b.N; i++ {
		e.Render(buf)
	}
}
