// Package fm is FM-STEP's 4-operator FM synthesizer engine (original code).
package fm

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// Op is one operator's settings.
type Op struct {
	Ratio   float64 `json:"ratio"`   // frequency multiple of the note
	Detune  float64 `json:"detune"`  // cents, -50..+50
	Level   float64 `json:"level"`   // 0..1 (output level, or modulation depth)
	Attack  float64 `json:"attack"`  // seconds
	Decay   float64 `json:"decay"`   // seconds (to sustain)
	Sustain float64 `json:"sustain"` // 0..1
	Release float64 `json:"release"` // seconds
	VelSens float64 `json:"vel"`     // 0..1 velocity sensitivity
}

// Patch is a complete sound.
type Patch struct {
	Name     string  `json:"name"`
	Algo     int     `json:"algo"`     // 0..len(Algorithms)-1
	Feedback float64 `json:"feedback"` // 0..1, operator 4 self-modulation
	Level    float64 `json:"level"`    // 0..1 patch volume
	Ops      [4]Op   `json:"ops"`      // Ops[0] = operator 1 ... Ops[3] = operator 4
}

// Algorithm describes how the 4 operators connect. Mods[i] lists the
// operators (0-based) that modulate operator i; every modulator has a higher
// index than its target, so operators are computed 4,3,2,1.
type Algorithm struct {
	Name     string
	Diagram  string
	Mods     [4][]int
	Carriers []int
}

var Algorithms = []Algorithm{
	{"STACK", "4>3>2>1", [4][]int{{1}, {2}, {3}, nil}, []int{0}},
	{"Y", "(3+4)>2>1", [4][]int{{1}, {2, 3}, nil, nil}, []int{0}},
	{"BRANCH", "4>3>1 2>1", [4][]int{{1, 2}, nil, {3}, nil}, []int{0}},
	{"2 PAIRS", "2>1 4>3", [4][]int{{1}, nil, {3}, nil}, []int{0, 2}},
	{"1 TO 3", "4>1,2,3", [4][]int{{3}, {3}, {3}, nil}, []int{0, 1, 2}},
	{"STACK+1", "4>3>2 1", [4][]int{nil, {2}, {3}, nil}, []int{0, 1}},
	{"PAIR+2", "4>3 1 2", [4][]int{nil, nil, {3}, nil}, []int{0, 1, 2}},
	{"ORGAN", "1 2 3 4", [4][]int{nil, nil, nil, nil}, []int{0, 1, 2, 3}},
}

// Ratios offered on the knob.
var Ratios = []float64{0.5, 1, 1.5, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16}

func op(r, lvl, a, d, s, rel float64) Op {
	return Op{Ratio: r, Level: lvl, Attack: a, Decay: d, Sustain: s, Release: rel, VelSens: 0.5}
}

// Factory presets, slots 1-8.
var Factory = []Patch{
	{Name: "E.PIANO", Algo: 3, Feedback: 0.1, Level: 0.8, Ops: [4]Op{
		op(1, 0.9, 0.002, 1.8, 0.0, 0.4), op(14, 0.25, 0.001, 0.3, 0.0, 0.2),
		op(1, 0.8, 0.002, 2.5, 0.1, 0.5), op(1, 0.45, 0.001, 1.2, 0.0, 0.3)}},
	{Name: "FM BASS", Algo: 0, Feedback: 0.3, Level: 0.9, Ops: [4]Op{
		op(0.5, 1.0, 0.002, 0.6, 0.6, 0.12), op(1, 0.55, 0.002, 0.25, 0.3, 0.1),
		op(1, 0.35, 0.002, 0.15, 0.2, 0.1), op(2, 0.2, 0.002, 0.1, 0.0, 0.1)}},
	{Name: "BELL", Algo: 3, Feedback: 0.0, Level: 0.7, Ops: [4]Op{
		op(1, 0.9, 0.001, 4.0, 0.0, 3.0), op(3.5, 0.5, 0.001, 3.0, 0.0, 2.5),
		op(2, 0.7, 0.001, 3.0, 0.0, 2.5), op(7, 0.45, 0.001, 2.0, 0.0, 2.0)}},
	{Name: "BRASS", Algo: 1, Feedback: 0.4, Level: 0.8, Ops: [4]Op{
		op(1, 0.9, 0.05, 0.3, 0.8, 0.25), op(1, 0.6, 0.08, 0.4, 0.6, 0.25),
		op(1, 0.3, 0.1, 0.5, 0.5, 0.3), op(1, 0.3, 0.1, 0.5, 0.4, 0.3)}},
	{Name: "PLUCK", Algo: 0, Feedback: 0.2, Level: 0.85, Ops: [4]Op{
		op(1, 0.95, 0.001, 0.8, 0.0, 0.2), op(2, 0.5, 0.001, 0.25, 0.0, 0.1),
		op(3, 0.3, 0.001, 0.12, 0.0, 0.1), op(5, 0.2, 0.001, 0.06, 0.0, 0.05)}},
	{Name: "SOFT PAD", Algo: 3, Feedback: 0.05, Level: 0.6, Ops: [4]Op{
		op(1, 0.8, 0.8, 1.0, 0.8, 1.5), op(2, 0.2, 1.2, 1.0, 0.6, 1.5),
		op(1, 0.7, 1.0, 1.0, 0.8, 1.8), op(1, 0.15, 1.5, 1.0, 0.7, 1.8)}},
	{Name: "ORGAN", Algo: 7, Feedback: 0.0, Level: 0.6, Ops: [4]Op{
		op(1, 0.8, 0.005, 0.1, 1.0, 0.05), op(2, 0.5, 0.005, 0.1, 1.0, 0.05),
		op(3, 0.35, 0.005, 0.1, 1.0, 0.05), op(4, 0.25, 0.005, 0.1, 1.0, 0.05)}},
	{Name: "METAL PERC", Algo: 4, Feedback: 0.6, Level: 0.75, Ops: [4]Op{
		op(1, 0.8, 0.001, 0.4, 0.0, 0.2), op(1.5, 0.6, 0.001, 0.3, 0.0, 0.2),
		op(2, 0.5, 0.001, 0.25, 0.0, 0.2), op(9, 0.6, 0.001, 0.2, 0.0, 0.15)}},
}

// Init is an empty starting patch (slots 9-16).
func Init() Patch {
	return Patch{Name: "INIT", Algo: 0, Level: 0.8, Ops: [4]Op{
		op(1, 0.9, 0.005, 0.5, 0.7, 0.3), op(1, 0.3, 0.005, 0.5, 0.5, 0.3),
		op(1, 0, 0.005, 0.5, 0.5, 0.3), op(1, 0, 0.005, 0.5, 0.5, 0.3)}}
}

func clamp(x, lo, hi float64) float64 {
	if math.IsNaN(x) {
		return lo
	}
	return math.Max(lo, math.Min(hi, x))
}

// Sanitize keeps every value in range (protects against hand-edited files).
func (p *Patch) Sanitize() {
	if p.Algo < 0 || p.Algo >= len(Algorithms) {
		p.Algo = 0
	}
	p.Feedback = clamp(p.Feedback, 0, 1)
	p.Level = clamp(p.Level, 0, 1)
	if len(p.Name) > 16 {
		p.Name = p.Name[:16]
	}
	for i := range p.Ops {
		o := &p.Ops[i]
		o.Ratio = clamp(o.Ratio, 0.125, 32)
		o.Detune = clamp(o.Detune, -50, 50)
		o.Level = clamp(o.Level, 0, 1)
		o.Attack = clamp(o.Attack, 0.001, 10)
		o.Decay = clamp(o.Decay, 0.005, 20)
		o.Sustain = clamp(o.Sustain, 0, 1)
		o.Release = clamp(o.Release, 0.005, 20)
		o.VelSens = clamp(o.VelSens, 0, 1)
	}
}

// SlotPath is where user preset slot n (1..16) lives.
func SlotPath(dir string, n int) string {
	return filepath.Join(dir, fmt.Sprintf("preset%02d.json", n))
}

// LoadSlot loads slot n, falling back to the factory/init patch.
func LoadSlot(dir string, n int) (Patch, bool) {
	if p, err := LoadFile(SlotPath(dir, n)); err == nil {
		return p, true
	}
	if n >= 1 && n <= len(Factory) {
		return Factory[n-1], false
	}
	return Init(), false
}

func LoadFile(path string) (Patch, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Patch{}, err
	}
	var p Patch
	if err := json.Unmarshal(b, &p); err != nil {
		return Patch{}, err
	}
	p.Sanitize()
	return p, nil
}

// SaveFile writes atomically (tmp + fsync + rename) so a power cut never
// leaves a half-written preset.
func SaveFile(path string, p Patch) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
