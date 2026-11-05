// Package sound holds FM-STEP's sound presets for the Plaits-based engine
// (fmstep-dsp). A preset is a name plus parameter values keyed by the
// engine's parameter names.
package sound

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

//go:embed factory.json
var factoryJSON []byte

type Preset struct {
	Name string             `json:"name"`
	P    map[string]float64 `json:"p"`
}

// Defaults mirror DefaultParams() in dsp/fmstep_dsp.cc.
var Defaults = map[string]float64{
	"engine": 8, "harmonics": 0.3, "timbre": 0.5, "morph": 0.4, "decay": 0.5, "lpg_colour": 0.5,
	"attack": 0.1, "decay2": 0.4, "sustain": 0.8, "release": 0.35,
	"sub_level": 0, "sub_oct": 1, "cutoff": 0.75, "resonance": 0.2, "drive": 0.2,
	"wobble_div": 4, "wobble_depth": 0,
	"delay_div": 5, "delay_feedback": 0.45, "delay_send": 0, "delay_tone": 0.5,
	"reverb_send": 0.15, "reverb_size": 0.5, "reverb_tone": 0.5,
	"tempo": 120, "voices": 6, "level": 0.8, "pan": 0.5,
	"eq_low": 0.5, "eq_mid": 0.5, "eq_high": 0.5, "eq_mid_freq": 0.5,
	"eq_low_freq": 0.574, "eq_high_freq": 0.5283, // 150 Hz, 6 kHz
}

// Factory returns the built-in presets with every parameter filled in.
func Factory() []Preset {
	var ps []Preset
	if err := json.Unmarshal(factoryJSON, &ps); err != nil {
		panic(err)
	}
	for i := range ps {
		ps[i] = Complete(ps[i])
	}
	return ps
}

// Complete fills missing parameters with defaults and drops unknown ones.
func Complete(p Preset) Preset {
	out := Preset{Name: p.Name, P: map[string]float64{}}
	for k, v := range Defaults {
		out.P[k] = v
	}
	for k, v := range p.P {
		if _, ok := Defaults[k]; ok {
			out.P[k] = v
		}
	}
	if len(out.Name) > 16 {
		out.Name = out.Name[:16]
	}
	return out
}

func Keys() []string {
	var ks []string
	for k := range Defaults {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func SlotPath(dir string, n int) string { return filepath.Join(dir, fmt.Sprintf("sound%02d.json", n)) }

// LoadSlot returns the user's preset in slot n (1..16) or the factory one.
func LoadSlot(dir string, n int) (Preset, bool) {
	if p, err := LoadFile(SlotPath(dir, n)); err == nil {
		return p, true
	}
	f := Factory()
	if n >= 1 && n <= len(f) {
		return f[n-1], false
	}
	return Preset{Name: "INIT", P: Complete(Preset{}).P}, false
}

func LoadFile(path string) (Preset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, err
	}
	var p Preset
	if err := json.Unmarshal(b, &p); err != nil {
		return Preset{}, err
	}
	return Complete(p), nil
}

// SaveFile writes atomically (tmp, fsync, rename).
func SaveFile(path string, p Preset) error {
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
