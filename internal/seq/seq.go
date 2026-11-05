// Package seq is FM-STEP's project model: 8 tracks (one sound each), 16
// patterns of up to 32 steps, parameter locks, mutes and a pattern chain.
// The sequencer itself runs inside fmstep-dsp; this package keeps the UI's
// copy, saves it to the SD card, and produces the commands that load it
// into the engine. Lock handling mirrors SetLock() in dsp/fmstep_dsp.cc.
package seq

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fmstep/internal/sound"
)

const (
	Tracks   = 8
	Patterns = 16
	Steps    = 64
	MaxLocks = 4
)

// GlobalKeys are engine parameters shared by all tracks (stored in the
// project, not in sounds).
var GlobalKeys = []string{"tempo", "swing", "rec_quant", "delay_div", "delay_feedback", "delay_tone", "reverb_size", "reverb_tone"}

var GlobalDefaults = map[string]float64{
	"tempo": 94, "swing": 0.45, "rec_quant": 1, "delay_div": 5, "delay_feedback": 0.55,
	"delay_tone": 0.4, "reverb_size": 0.55, "reverb_tone": 0.45,
}

func IsGlobal(k string) bool { _, ok := GlobalDefaults[k]; return ok }

// TrackKeys are the per-track sound parameters (lockable per step).
func TrackKeys() []string {
	var ks []string
	for _, k := range sound.Keys() {
		if !IsGlobal(k) {
			ks = append(ks, k)
		}
	}
	return ks
}

type Lock struct {
	Key string  `json:"k"`
	V   float64 `json:"v"`
}

type Step struct {
	On    bool
	Note  int
	Vel   float64
	Gate  float64 // in steps
	Micro float64 // in steps, -0.45..0.45
	Locks []Lock
}

func NewStep(note int) Step { return Step{Note: note, Vel: 0.8, Gate: 0.5} }

func (s *Step) Empty() bool { return !s.On && len(s.Locks) == 0 }

// SetLock stores a lock the same way the engine does: replace the same key,
// else use a free slot, else overwrite the last slot. Returns the slot.
func (s *Step) SetLock(k string, v float64) int {
	for i := range s.Locks {
		if s.Locks[i].Key == k {
			s.Locks[i].V = v
			return i
		}
	}
	if len(s.Locks) < MaxLocks {
		s.Locks = append(s.Locks, Lock{k, v})
		return len(s.Locks) - 1
	}
	s.Locks[MaxLocks-1] = Lock{k, v}
	return MaxLocks - 1
}

func (s *Step) LockValue(k string) (float64, bool) {
	for _, l := range s.Locks {
		if l.Key == k {
			return l.V, true
		}
	}
	return 0, false
}

type Pattern struct {
	Len int
	S   [Tracks][Steps]Step
}

func (p *Pattern) Clear() {
	for t := range p.S {
		for s := range p.S[t] {
			p.S[t][s] = NewStep(60)
		}
	}
}

func (p *Pattern) HasNotes() bool {
	for t := range p.S {
		for s := 0; s < p.Len; s++ {
			if p.S[t][s].On {
				return true
			}
		}
	}
	return false
}

type Track struct {
	Sound sound.Preset `json:"sound"`
	Slot  int          `json:"slot"` // sound slot it was loaded from (0 = unsaved)
	Muted bool         `json:"muted"`
	Note  int          `json:"note"` // note for new steps (last pad played)
}

type Project struct {
	Name   string
	Tracks [Tracks]Track
	G      map[string]float64
	Pat    [Patterns]Pattern
	Chain  []int
	Sel    int // selected pattern
	Track  int // selected track
}

// ---- JSON (sparse: only steps that hold something are written) ----

type stepJSON struct {
	T     int     `json:"t"`
	S     int     `json:"s"`
	On    bool    `json:"on,omitempty"`
	Note  int     `json:"n"`
	Vel   float64 `json:"v"`
	Gate  float64 `json:"g"`
	Micro float64 `json:"m,omitempty"`
	Locks []Lock  `json:"l,omitempty"`
}

type patJSON struct {
	Len   int        `json:"len"`
	Steps []stepJSON `json:"steps"`
}

type projJSON struct {
	Version  int                `json:"version"`
	Name     string             `json:"name"`
	Tracks   []Track            `json:"tracks"`
	G        map[string]float64 `json:"g"`
	Patterns []patJSON          `json:"patterns"`
	Chain    []int              `json:"chain"`
	Sel      int                `json:"sel"`
	Track    int                `json:"track"`
}

func (p *Project) MarshalJSON() ([]byte, error) {
	j := projJSON{Version: 1, Name: p.Name, Tracks: p.Tracks[:], G: p.G, Chain: p.Chain, Sel: p.Sel, Track: p.Track}
	if j.Chain == nil {
		j.Chain = []int{}
	}
	for i := range p.Pat {
		pj := patJSON{Len: p.Pat[i].Len, Steps: []stepJSON{}}
		for t := 0; t < Tracks; t++ {
			for s := 0; s < Steps; s++ {
				st := p.Pat[i].S[t][s]
				if st.Empty() {
					continue
				}
				pj.Steps = append(pj.Steps, stepJSON{t, s, st.On, st.Note, round3(st.Vel), round3(st.Gate), round3(st.Micro), st.Locks})
			}
		}
		j.Patterns = append(j.Patterns, pj)
	}
	return json.MarshalIndent(j, "", " ")
}

func round3(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 3, 64), 64)
	return f
}

func (p *Project) UnmarshalJSON(b []byte) error {
	var j projJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*p = *New()
	p.Name = j.Name
	for i := 0; i < Tracks && i < len(j.Tracks); i++ {
		tr := j.Tracks[i]
		tr.Sound = sound.Complete(tr.Sound)
		if tr.Note < 0 || tr.Note > 127 {
			tr.Note = 60
		}
		p.Tracks[i] = tr
	}
	for k, v := range j.G {
		if IsGlobal(k) {
			p.G[k] = v
		}
	}
	for i := 0; i < Patterns && i < len(j.Patterns); i++ {
		pj := j.Patterns[i]
		p.Pat[i].Len = clampInt(pj.Len, 1, Steps)
		for _, s := range pj.Steps {
			if s.T < 0 || s.T >= Tracks || s.S < 0 || s.S >= Steps {
				continue
			}
			st := Step{On: s.On, Note: clampInt(s.Note, 0, 127), Vel: clamp(s.Vel, 0, 1), Gate: clamp(s.Gate, 0.05, 16), Micro: clamp(s.Micro, -0.45, 0.45)}
			for _, l := range s.Locks {
				if len(st.Locks) < MaxLocks && !IsGlobal(l.Key) {
					if _, ok := sound.Defaults[l.Key]; ok {
						st.Locks = append(st.Locks, l)
					}
				}
			}
			p.Pat[i].S[s.T][s.S] = st
		}
	}
	for _, c := range j.Chain {
		if c >= 0 && c < Patterns && len(p.Chain) < 8 {
			p.Chain = append(p.Chain, c)
		}
	}
	p.Sel = clampInt(j.Sel, 0, Patterns-1)
	p.Track = clampInt(j.Track, 0, Tracks-1)
	return nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// New returns an empty project (init sounds, empty 16-step patterns).
func New() *Project {
	p := &Project{Name: "NEW PROJECT", G: map[string]float64{}}
	for k, v := range GlobalDefaults {
		p.G[k] = v
	}
	for i := range p.Tracks {
		p.Tracks[i] = Track{Sound: sound.Complete(sound.Preset{Name: "INIT"}), Note: 60}
	}
	for i := range p.Pat {
		p.Pat[i].Len = 16
		p.Pat[i].Clear()
	}
	return p
}

//go:embed default_project.json
var defaultJSON []byte

// Default is the starter project: a jazz/dub/house kit with three grooves.
func Default() *Project {
	p := New()
	if err := json.Unmarshal(defaultJSON, p); err != nil {
		panic(err)
	}
	return p
}

func Load(path string) (*Project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := New()
	if err := json.Unmarshal(b, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Save writes atomically (tmp, fsync, rename).
func (p *Project) Save(path string) error {
	b, err := json.Marshal(p)
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

func SlotPath(dir string, n int) string {
	return filepath.Join(dir, fmt.Sprintf("project%02d.json", n))
}

// ---- engine commands ----

func f(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

func StepCmd(pat, t, s int, st Step) string {
	on := 0
	if st.On {
		on = 1
	}
	return fmt.Sprintf("S %d %d %d %d %d %s %s %s", pat, t, s, on, st.Note, f(st.Vel), f(st.Gate), f(st.Micro))
}

// StepCmds rewrites one step in the engine (step data, then its locks).
func StepCmds(pat, t, s int, st Step) []string {
	out := []string{StepCmd(pat, t, s, st), fmt.Sprintf("LX %d %d %d", pat, t, s)}
	for _, l := range st.Locks {
		out = append(out, fmt.Sprintf("L %d %d %d %s %s", pat, t, s, l.Key, f(l.V)))
	}
	return out
}

func TrackCmds(t int, tr *Track) []string {
	var out []string
	for _, k := range TrackKeys() {
		out = append(out, fmt.Sprintf("T %d %s %s", t, k, f(tr.Sound.P[k])))
	}
	return out
}

func ChainCmd(chain []int) string {
	s := "CHAIN"
	for _, c := range chain {
		s += " " + strconv.Itoa(c)
	}
	return s
}

// PatternCmds rewrites one pattern in the engine.
func (p *Project) PatternCmds(i int) []string {
	out := []string{fmt.Sprintf("CLR %d", i), fmt.Sprintf("LEN %d %d", i, p.Pat[i].Len)}
	for t := 0; t < Tracks; t++ {
		for s := 0; s < Steps; s++ {
			st := p.Pat[i].S[t][s]
			if st.Empty() {
				continue
			}
			out = append(out, StepCmd(i, t, s, st))
			for _, l := range st.Locks {
				out = append(out, fmt.Sprintf("L %d %d %d %s %s", i, t, s, l.Key, f(l.V)))
			}
		}
	}
	return out
}

// CopyPattern copies pattern src over dst (locks are deep-copied).
func (p *Project) CopyPattern(src, dst int) {
	p.Pat[dst] = p.Pat[src]
	for t := range p.Pat[dst].S {
		for s := range p.Pat[dst].S[t] {
			if l := p.Pat[dst].S[t][s].Locks; l != nil {
				p.Pat[dst].S[t][s].Locks = append([]Lock(nil), l...)
			}
		}
	}
}

// Commands loads the whole project into a freshly started (or stopped) engine.
func (p *Project) Commands() []string {
	out := []string{"STOP"}
	gk := make([]string, 0, len(p.G))
	for k := range p.G {
		gk = append(gk, k)
	}
	sort.Strings(gk)
	for _, k := range gk {
		out = append(out, fmt.Sprintf("P %s %s", k, f(p.G[k])))
	}
	for t := range p.Tracks {
		out = append(out, TrackCmds(t, &p.Tracks[t])...)
		m := 0
		if p.Tracks[t].Muted {
			m = 1
		}
		out = append(out, fmt.Sprintf("M %d %d", t, m))
	}
	for i := range p.Pat {
		out = append(out, p.PatternCmds(i)...)
	}
	out = append(out, ChainCmd(p.Chain), fmt.Sprintf("PAT %d", p.Sel), fmt.Sprintf("U %d", p.Track))
	return out
}

// ApplyReply folds the engine's record replies (REC, RECG, RECL) into the
// project. It returns the pattern/track/step touched, or ok=false.
func (p *Project) ApplyReply(line string) (pat, t, s int, ok bool) {
	fs := strings.Fields(line)
	if len(fs) < 5 {
		return
	}
	ints := func(n int) bool {
		var err error
		if pat, err = strconv.Atoi(fs[1]); err != nil {
			return false
		}
		if t, err = strconv.Atoi(fs[2]); err != nil {
			return false
		}
		if s, err = strconv.Atoi(fs[3]); err != nil {
			return false
		}
		return len(fs) >= n && pat >= 0 && pat < Patterns && t >= 0 && t < Tracks && s >= 0 && s < Steps
	}
	num := func(i int) float64 { v, _ := strconv.ParseFloat(fs[i], 64); return v }
	switch fs[0] {
	case "REC":
		if !ints(7) {
			return
		}
		st := &p.Pat[pat].S[t][s]
		st.On = true
		st.Note = clampInt(int(num(4)), 0, 127)
		st.Vel = clamp(num(5), 0, 1)
		st.Gate = 0.5
		st.Micro = clamp(num(6), -0.45, 0.45)
		ok = true
	case "RECG":
		if !ints(5) {
			return
		}
		p.Pat[pat].S[t][s].Gate = clamp(num(4), 0.05, 16)
		ok = true
	case "RECL":
		if !ints(6) {
			return
		}
		p.Pat[pat].S[t][s].SetLock(fs[4], num(5))
		ok = true
	}
	return
}
