// fmstep: FM-STEP 0.7 UI (8-track groovebox). Sound and the sequencer
// clock come from the fmstep-dsp process (Mutable Instruments Plaits voices
// + FM-STEP's dub/club layer), which this program starts and controls over a
// pipe. Runs under fmstep-supervisor. Sequencer screens are in sequi.go.
//
// Controls:
//
//	pads          play the selected track   K1 K2 K3   edit the 3 lines shown
//	DATA / screen buttons: page             SHIFT      alternate parameter
//	SAMPLE        tracks: select / mute     SEQ        step edit
//	LOOP          hold: loop length; SHIFT: patterns   PLAY/STOP  sequencer
//	SEQ RECORD    record pads and knobs     MUTE+pad   mute track 1-8
//	- / +         octave                    NOTE REPEAT (hold) dub throw
//	FADER         performance effect        TAP TEMPO  tap / hold+DATA = BPM
//	SAMPLE SELECT sounds (SHIFT: projects)  SHIFT+STOP, then STOP: Exit to Stock
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/link"
	"fmstep/internal/panel"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

var engineNames = []string{
	"VA + FILTER", "PHASE DIST", "DX7 BANK A", "DX7 BANK B", "DX7 BANK C", "WAVE TERRAIN",
	"STRING MACHINE", "CHIPTUNE", "ANALOG", "WAVESHAPER", "2-OP FM", "GRANULAR",
	"ADDITIVE", "WAVETABLE", "CHORDS", "SPEECH", "SWARM", "NOISE",
	"PARTICLE", "STRING", "MODAL", "BASS DRUM", "SNARE", "HI-HAT",
}

// Models offered on the MODEL page (chiptune left out on request).
// 0.9.24: the MODEL knob offers only the 4 instruments Darren kept (ANALOG,
// 2-OP FM, CHORDS, MODAL). Sounds made with other models still play them.
// 0.9.25: plus 4 more, one for each style Darren named: VA + FILTER (drum &
// bass), WAVETABLE (house), WAVESHAPER (dubstep), NOISE (noise / ambience).
var modelChoices = []float64{8, 10, 14, 20, 0, 13, 9, 17}

var modelStyle = map[int]string{0: "DRUM & BASS", 13: "HOUSE", 9: "DUBSTEP", 17: "NOISE · AMBIENCE"}

// Patch names inside Plaits' three DX7 banks (from plaits/resources.cc).
var dxNames = [3][]string{
	{"SOLID BASS", "MOOGER LOW", "LEADERTAPE", "MORHOL TB1", "BASS 3", "BILL BASS", "BASS 1", "ELEC BASS", "S.BAS 27.7", "RESONANCES", "SYN-BASS 2", "PRC SYNTH1", "CROMA 2", "ANALOG 4", "ANALOG A", "ANALOG 6", "CS 80", "INSERT 1", "SPIRAL", "DX-TROTT", "GASHAUS", "RING DING", "PAPAGAYO", "WINEGLASS", "AMYTAL", "FAIRLIGHT", "PPG VOL.1", "PPG VOL.2", "FAIRL. 3", "VOCODER 2", "SEQUENCE", "BOUNCE 4"},
	{"E.PIANO 1", "FENDER 1", "WINTRHODES", "RS-EP C", "MARK III", "CLAV-E.PNO", "SYN-CLAV", "CLAVINET", "PIANO 5", "GRD PIANO1", "STEINWAY", "GUIT ACOUS", "SITAR", "KOTO", "HARPSICH 1", "CLAV 3", "XYLOPHONE", "MARIMBA", "VIBE 1", "GLOKENSPL", "BELL C", "BELLS", "TUB BELLS", "GONG 2", "KETTLE 6", "MID DRM 3", "ORI DRUM 1", "WOOD 6", "LATN DRM 4", "CIMBAL", "SYNDM 25.8", "B.DRM-SNAR"},
	{"CLICK 124", "HAMMOND 1", "E.ORGAN 3", "60-S ORGAN", "OPTIC 28", "PIPES 1", "PIPES 3", "PIPES 2", "JX-33-P", "SOUNDTRACK", "ICE PAD 2", "M1 PADS", "CARLOS 2", "SOFT TOUCH", "PLANETS", "CIRRUS", "ENTRIX", "MAL POLY", "TEXTURES 6", "ETHERIAL5A", "AIRY", "BORON A", "VANGELIS 1", "STRINGS C", "STRINGS 3", "STRINGS 2", "STRINGS 7", "FULL STRIN", "SYN-ORCH", "BRASS 1", "BRASS 6 BC", "BR TRUMPET"},
}

var chordNames = []string{"OCTAVE", "FIFTH", "SUS4", "MINOR", "MINOR 7", "MINOR 9", "MINOR 11", "6/9", "MAJOR 9", "MAJOR 7", "MAJOR"}

// harmonicsName shows what HARMONICS selects for models where it picks
// something discrete (a DX7 patch or a chord).
func harmonicsName(engine int, h float64) string {
	switch {
	case engine >= 2 && engine <= 4:
		i := int(h * 1.02 * 32)
		if i > 31 {
			i = 31
		}
		return dxNames[engine-2][i]
	case engine == 14:
		i := int(h * 11)
		if i > 10 {
			i = 10
		}
		return chordNames[i]
	}
	return ""
}

// The fader's 8 performance effects (whole mix, not stored in sounds),
// picked directly with KNOB FX + pad 1-8 (or SHIFT + TAP TEMPO to step).
// neutral is where an effect does nothing; an effect you leave glides back
// there. col is its pad colour.
var faderTargets = []struct {
	key, label string
	neutral    float64
	col        [3]byte
}{
	{"fx_lp", "LOW PASS", 1, [3]byte{127, 45, 0}},
	{"fx_hp", "HIGH PASS", 0, [3]byte{0, 60, 127}},
	{"fx_slicer", "SLICER", 0, [3]byte{127, 0, 90}},
	{"fx_stutter", "STUTTER", 0, [3]byte{127, 110, 0}},
	{"fx_space", "SPACE ECHO", 0, [3]byte{0, 120, 50}},
	{"fx_wash", "REVERB WASH", 0, [3]byte{60, 40, 127}},
	{"fx_pan", "PAN", 0.5, [3]byte{0, 110, 110}},
	{"fx_vol", "VOLUME", 1, [3]byte{100, 100, 100}},
}

var divNames = []string{"1/32", "1/16T", "1/16", "1/8T", "1/8", "1/8D", "1/4T", "1/4", "1/4D", "1/2", "1/1"}

// ------------------------------------------------------------------ params

type param struct {
	key     string
	label   string
	min     float64
	max     float64
	steps   int       // >1: discrete values min..max
	choices []float64 // discrete values in this order (overrides steps)
	show    func(v float64) string
}

// eqDb mirrors EqDb() in fmstep-dsp: centre flat, the left half cuts down
// to -36 dB (DJ-style kill), the right half boosts up to +18 dB.
func eqDb(v float64) float64 {
	if v < 0.5 {
		return (v - 0.5) * 72
	}
	return (v - 0.5) * 36
}

func dbShow(v float64) string {
	d := eqDb(v)
	switch {
	case math.Abs(d) < 0.25:
		return "FLAT"
	case d <= -35.5:
		return "KILL"
	}
	return fmt.Sprintf("%+.1f DB", d)
}

func pct(v float64) string { return fmt.Sprintf("%d", int(v*99+0.5)) }

func timeShow(lo, hi float64) func(float64) string {
	return func(v float64) string {
		t := lo * math.Pow(hi/lo, v)
		if t < 1 {
			return fmt.Sprintf("%d MS", int(t*1000+0.5))
		}
		return fmt.Sprintf("%.1f S", t)
	}
}

func cont(key, label string) param { return param{key: key, label: label, max: 1, show: pct} }

var (
	pEngine = param{key: "engine", label: "MODEL", max: 23, choices: modelChoices, show: func(v float64) string { return engineNames[int(v)] }}
	pWobDiv = param{key: "wobble_div", label: "RATE", max: 10, steps: 11, show: func(v float64) string { return divNames[int(v)] }}
	pDlyDiv = param{key: "delay_div", label: "TIME", max: 10, steps: 11, show: func(v float64) string { return divNames[int(v)] }}
	pTempo  = param{key: "tempo", label: "TEMPO", min: 40, max: 200, steps: 161, show: func(v float64) string { return fmt.Sprintf("%d BPM", int(v)) }}
	pSwing  = param{key: "swing", label: "SWING", max: 1, show: func(v float64) string { return fmt.Sprintf("%d%%", int(50+v*21+0.5)) }}
	pQuant  = param{key: "rec_quant", label: "REC QUANT", max: 1, steps: 2, show: func(v float64) string {
		if v > 0.5 {
			return "ON"
		}
		return "OFF (LIVE FEEL)"
	}}
	pPan = param{key: "pan", label: "PAN", max: 1, show: func(v float64) string {
		switch d := int(math.Round((v - 0.5) * 100)); {
		case d < 0:
			return fmt.Sprintf("L%d", -d)
		case d > 0:
			return fmt.Sprintf("R%d", d)
		}
		return "CENTRE"
	}}
	pEqLow      = param{key: "eq_low", label: "LOW", max: 1, show: dbShow}
	pEqMid      = param{key: "eq_mid", label: "MID", max: 1, show: dbShow}
	pEqHigh     = param{key: "eq_high", label: "HIGH", max: 1, show: dbShow}
	pEqFreq     = param{key: "eq_mid_freq", label: "MID FREQ", max: 1, show: func(v float64) string { return hzShow(eqMidHz(v)) }}
	pEqLowFreq  = param{key: "eq_low_freq", label: "LOW FREQ", max: 1, show: func(v float64) string { return hzShow(eqLowHz(v)) }}
	pEqHighFreq = param{key: "eq_high_freq", label: "HIGH FREQ", max: 1, show: func(v float64) string { return hzShow(eqHighHz(v)) }}
	pPanSnap    = param{key: "pan", label: "PAN SNAP", max: 1, choices: []float64{0, 0.5, 1}, show: pPan.show}
	pUsbLevel   = param{key: "usb_level", label: "USB LEVEL", max: 1, show: func(v float64) string {
		return fmt.Sprintf("%.1f DB", -24*(1-v)) // USB recording level, before MAIN VOLUME
	}}
	pVoices   = param{key: "voices", label: "VOICES", min: 1, max: 6, steps: 6, show: func(v float64) string { return fmt.Sprintf("%d", int(v)) }}
	pSubOct   = param{key: "sub_oct", label: "SUB OCT", min: 1, max: 2, steps: 2, show: func(v float64) string { return fmt.Sprintf("-%d", int(v)) }}
	pAttack   = param{key: "attack", label: "ATTACK", max: 1, show: timeShow(0.001, 4)}
	pDecay2   = param{key: "decay2", label: "DECAY", max: 1, show: timeShow(0.01, 6)}
	pRelease  = param{key: "release", label: "RELEASE", max: 1, show: timeShow(0.01, 6)}
	pFeedback = param{key: "delay_feedback", label: "FEEDBACK", max: 1, show: func(v float64) string {
		s := pct(v)
		if v > 0.87 {
			s += " RUNAWAY"
		}
		return s
	}}
)

type page struct {
	name string
	p    [3]param
	alt  [3]*param
}

func ptr(p param) *param { return &p }

// Page order (user's layout, 0.8.2): the core of the sound first, then the
// effects, then the utility pages.
var pages = []page{
	{name: "MODEL", p: [3]param{pEngine, cont("decay", "LPG DECAY"), cont("lpg_colour", "LPG COLOUR")}},
	{name: "EQ", p: [3]param{pEqLow, pEqMid, pEqHigh}, alt: [3]*param{&pEqLowFreq, &pEqFreq, &pEqHighFreq}},
	{name: "AMP", p: [3]param{pAttack, pDecay2, cont("sustain", "SUSTAIN")}, alt: [3]*param{nil, nil, &pRelease}},
	{name: "SUB + FILTER", p: [3]param{cont("sub_level", "SUB"), cont("cutoff", "CUTOFF"), cont("resonance", "RESONANCE")}, alt: [3]*param{&pSubOct, nil, nil}},
	{name: "SOUND", p: [3]param{cont("harmonics", "HARMONICS"), cont("timbre", "TIMBRE"), cont("morph", "MORPH")}},
	// Character page: global "signature" settings, kept across sounds.
	{name: "WARMTH", p: [3]param{cont("drift", "DRIFT"), cont("chorus", "CHORUS"), cont("softness", "SOFTNESS")}},
	{name: "WOBBLE", p: [3]param{pWobDiv, cont("wobble_depth", "DEPTH"), cont("drive", "DRIVE")}},
	{name: "DUB DELAY", p: [3]param{pDlyDiv, pFeedback, cont("delay_send", "SEND")}, alt: [3]*param{nil, nil, ptr(cont("delay_tone", "TONE"))}},
	{name: "REVERB", p: [3]param{cont("reverb_send", "SEND"), cont("reverb_size", "SIZE"), cont("reverb_tone", "TONE")}},
	{name: "TAPE", p: [3]param{cont("tape_drive", "DRIVE"), cont("tape_age", "AGE"), cont("hiss", "HISS")},
		alt: [3]*param{ptr(cont("glue", "GLUE")), nil, ptr(cont("crackle", "CRACKLE"))}},
	{name: "GLOBAL", p: [3]param{pTempo, pVoices, cont("level", "LEVEL")}, alt: [3]*param{nil, nil, &pUsbLevel}},
	{name: "GROOVE", p: [3]param{pSwing, pQuant, pPan}, alt: [3]*param{nil, nil, &pPanSnap}},
}

func pageIndex(name string) int {
	for i := range pages {
		if pages[i].name == name {
			return i
		}
	}
	return 0
}

// Character settings are global (not part of a sound) and live in
// FMSTEP/user/character.json.
var charDefaults = map[string]float64{
	"drift": 0.35, "chorus": 0.3, "softness": 0.35, "tape_drive": 0.4,
	"tape_age": 0.25, "hiss": 0.15, "crackle": 0, "glue": 0.5,
	"usb_level": 0.625, // USB audio copy level: -9 dB (0 = -24 dB, 1 = 0 dB)
}

// ------------------------------------------------------------------ dsp process

type dsp struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	mu    sync.Mutex
	ready chan string
	ev    chan string // POS / REC / RECG / RECL lines for the UI loop

	smp  smpData // FLUX part 2: tape levels, grab overview, zoom (sampler.go)
	mt   mtData  // MIXTAPE and the mixer levels (mixtape.go)
	pads padData // 0.9.27 pads: loads, SAMPLE IN, master gain (pads.go)

	specMu sync.Mutex // latest SPEC line (visualizer)
	spec   [48]float64
	peakL  float64
	peakR  float64
	specAt time.Time
}

// parseSpec reads "SPEC peakL peakR hex": 48 bands, one byte each, 0..255 = -90..0 dB.
func (d *dsp) parseSpec(line string) {
	f := strings.Fields(line)
	if len(f) != 4 || len(f[3]) != 96 {
		return
	}
	l, _ := strconv.ParseFloat(f[1], 64)
	r, _ := strconv.ParseFloat(f[2], 64)
	d.specMu.Lock()
	for b := 0; b < 48; b++ {
		v, err := strconv.ParseUint(f[3][2*b:2*b+2], 16, 8)
		if err == nil {
			d.spec[b] = float64(v)*90/255 - 90
		}
	}
	d.peakL, d.peakR, d.specAt = l, r, time.Now()
	d.specMu.Unlock()
}

func startDSP(bin, dev string, logf func(string, ...any)) (*dsp, error) {
	d := &dsp{ready: make(chan string, 1), ev: make(chan string, 1024)}
	d.cmd = exec.Command(bin, dev)
	// an empty file FMSTEP/NO_USB_AUDIO switches the USB audio copy off
	if _, err := os.Stat(filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "NO_USB_AUDIO")); err == nil {
		d.cmd.Env = append(os.Environ(), "FMSTEP_NO_USB_AUDIO=1")
		logf("USB_AUDIO disabled by NO_USB_AUDIO file")
	}
	var err error
	if d.in, err = d.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	out, err := d.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	d.cmd.Stderr = os.Stderr
	if err := d.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "SPEC ") {
				d.parseSpec(line)
				continue
			}
			if d.pads.parse(line) { // PLOADED / PRECDONE / MGR ...
				if strings.HasPrefix(line, "PLOAD") || strings.HasPrefix(line, "PRECDONE") {
					logf("DSP %s", line)
				}
				continue
			}
			if d.smp.parse(line, logf) { // TW / GW / GZ / GRAB / GRABWAIT
				continue
			}
			if d.mt.parse(line, logf) { // MTR / MTP / MTDONE / LV ...
				continue
			}
			if strings.HasPrefix(line, "POS ") || strings.HasPrefix(line, "REC") {
				if !strings.HasPrefix(line, "POS ") {
					logf("DSP %s", line)
				}
				select {
				case d.ev <- line:
				default:
				}
				continue
			}
			logf("DSP %s", line)
			if strings.HasPrefix(line, "READY") {
				select {
				case d.ready <- line:
				default:
				}
			}
		}
	}()
	select {
	case r := <-d.ready:
		logf("DSP_READY %s", r)
		return d, nil
	case <-time.After(5 * time.Second):
		d.cmd.Process.Kill()
		return nil, fmt.Errorf("fmstep-dsp did not become ready")
	}
}

func (d *dsp) send(f string, a ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.in, f+"\n", a...)
}

func (d *dsp) set(k string, v float64) { d.send("P %s %g", k, v) }

func (d *dsp) lines(ls []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, l := range ls {
		fmt.Fprintln(d.in, l)
	}
}

// stop asks for a fade-out and waits for the process (killed after 3 s).
func (d *dsp) stop() {
	d.send("Q")
	d.in.Close()
	done := make(chan struct{})
	go func() { d.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		d.cmd.Process.Kill()
	}
}

// ------------------------------------------------------------------ app

type mode int

const (
	modePlay mode = iota
	modePreset
	modeConfirmExit
	modeStep     // SEQ: pads are the 16 steps of a page
	modeTracks   // SAMPLE: pads 1-8 select a track, 9-16 mute
	modePatterns // LOOP: pads are the 16 patterns
	modeProject  // project slots (SHIFT + pad 16 PROJECT)
	modeFX       // KNOB FX: pads 1-8 pick the fader effect
	modeRack     // PAD FX: the RACK (0.9.25)
)

type tourStep struct {
	text string
	done func(a *app) bool
}

type app struct {
	smp       smpState         // FLUX part 2: LIBRARY / CHOP / SLICE (sampler.go)
	util      utilState        // 0.9.26 SHIFT + PAD (util.go)
	pads      padsState        // 0.9.27 every pad its own sound (pads.go)
	pui       padUI            // 0.9.27 the sampler screens (pads_ui.go)
	vis       visState         // 0.9.27 MELT / COLLAGE (vis.go)
	kp        [3]knobPickState // 0.9.28 K1-K3 pick-up
	lights    map[byte]bool    // 0.9.27 button lights sent
	mt        mtState          // MIXTAPE (mixtape.go)
	keep      map[int]bool     // the models marked KEEP (keep.go)
	user      string
	char      map[string]float64
	scr       *panel.Screen
	cs        *panel.ControlSurface
	dsp       *dsp
	preset    *sound.Preset // the selected track's sound (points into proj)
	proj      *seq.Project
	slot      int
	modified  bool
	pageIdx   int
	octave    int
	shift     bool
	recHeld   bool
	tapHeld   bool // TAP TEMPO held: DATA sets an exact BPM
	tapTurned bool
	mode      mode
	lastCC    [128]int
	ccKnown   [128]bool
	accum     [3]float64
	held      map[int]int
	taps      []time.Time
	status    string
	statusAt  time.Time
	dirty     bool

	tour         []tourStep
	tourIdx      int
	tourDone     bool
	sawKnob      [3]bool
	sawModel     bool
	sawFader     bool
	bank         int // sound bank 0..3 (slots bank*16+1 ..)
	faderIdx     int
	faderVal     float64    // -1 until moved after picking an effect
	fxNow        [8]float64 // each effect's value now (glides toward fxGoal)
	fxGoal       [8]float64
	fxLast       [8]float64 // value before an effect was toggled off
	faderMoved   bool
	lastKey      string // last parameter touched by K1-K3 (for SHIFT+DATA fine tune)
	usedFine     bool
	changedBank  bool
	changedFader bool
	loadsSince   int
	threw        bool
	tapped       bool
	loadedWob    bool
	saved        bool
	played       int

	sq seqUI   // sequencer UI state (sequi.go)
	ui uiState // drawing / animation state (ui.go)
}

func (a *app) log(f string, x ...any) {
	fmt.Printf("%s %s\n", time.Now().UTC().Format("15:04:05.000"), fmt.Sprintf(f, x...))
}

func (a *app) say(s string) { a.status, a.statusAt, a.dirty = s, time.Now(), true }

func (a *app) soundsDir() string { return filepath.Join(a.user, "sounds") }

func (a *app) load(p sound.Preset) {
	*a.preset = sound.Complete(p)
	a.dsp.lines(seq.TrackCmds(a.proj.Track, &a.proj.Tracks[a.proj.Track]))
	a.sq.projDirty = true
	a.dirty = true
}

// padColour only sends a SysEx when the pad's colour actually changes.
func (a *app) padColour(i int, r, g, b byte) {
	c := [3]byte{r, g, b}
	if a.sq.padKnown[i] && a.sq.pad[i] == c {
		return
	}
	a.sq.pad[i], a.sq.padKnown[i] = c, true
	if a.cs != nil {
		a.cs.Write(controls.PadColour(byte(i), r, g, b))
	}
}

func (a *app) idleColour(i int) {
	c := trackColours[a.proj.Track]
	if i%12 == 0 {
		a.padColour(i, c[0]/2, c[1]/2, c[2]/2)
	} else {
		a.padColour(i, c[0]/7, c[1]/7, c[2]/7)
	}
}

func (a *app) paintPads() {
	if a.sq.muteMode != muteOff {
		a.paintMutePads()
		return
	}
	if a.smp.view != smpOff {
		a.smpPaintPads()
		return
	}
	switch a.mode {
	case modeStep, modeTracks, modePatterns, modeProject, modeFX:
		a.paintSeqPads()
		return
	}
	if a.sq.fxHeld || a.loopShown() {
		a.paintSeqPads()
		return
	}
	for i := 0; i < 16; i++ {
		if a.mode == modePreset {
			n := a.bank*16 + i + 1
			_, user := sound.LoadSlot(a.soundsDir(), n)
			switch {
			case n == a.slot:
				a.padColour(i, 127, 127, 127)
			case user:
				a.padColour(i, 0, 70, 10)
			case n <= 16:
				a.padColour(i, 0, 12, 60)
			default:
				a.padColour(i, 5, 5, 5)
			}
		} else {
			a.idleColour(i)
		}
	}
}

func (a *app) baseNote() int { return 36 + 12*a.octave }

func (a *app) onPad(idx int, vel byte, on bool) {
	if a.util.fullLevel && on && vel > 0 { // SHIFT + pad 1: FULL LEVEL
		vel = 127
	}
	if a.utilPadPress(idx, on) || (a.util.menu && on) {
		return
	}
	if a.recHeld && on {
		a.mt.recUsed = true // SAMPLE RECORD + pad saves; it isn't a MIXTAPE tap
	}
	if a.smp.view != smpOff {
		a.smpPad(idx, vel, on)
		return
	}
	if a.sq.muteMode != muteOff { // 0.9.28: MUTE is on: the pads mute / solo
		if on {
			a.mutePad(idx)
		}
		return
	}
	if a.quickPad(idx, on) {
		return
	}
	switch a.mode {
	case modeStep, modeTracks, modePatterns, modeProject, modeFX:
		a.onSeqPad(idx, vel, on)
		return
	}
	if a.mode == modePreset {
		if !on {
			return
		}
		if seq.IsSample(a.proj.Track) {
			a.say("TRACK " + fmt.Sprint(a.proj.Track+1) + " PLAYS PADS  ·  SAMPLE SELECT FILLS THEM")
			return
		}
		n := a.bank*16 + idx + 1
		if a.recHeld {
			if err := sound.SaveFile(sound.SlotPath(a.soundsDir(), n), *a.preset); err != nil {
				a.say("SAVE FAILED")
				a.log("SAVE_FAILED slot=%d err=%v", n, err)
			} else {
				a.slot, a.modified, a.saved = n, false, true
				a.proj.Tracks[a.proj.Track].Slot = n
				a.say(fmt.Sprintf("SAVED %s TO %s%d", a.preset.Name, string(rune('A'+a.bank)), idx+1))
				a.log("SOUND_SAVED slot=%d name=%q", n, a.preset.Name)
				a.saveCharacter()
			}
		} else {
			p, user := sound.LoadSlot(a.soundsDir(), n)
			a.load(p)
			a.slot, a.modified = n, false
			a.proj.Tracks[a.proj.Track].Slot = n
			if p.Name == "WOBBLE BASS" {
				a.loadedWob = true
			}
			a.loadsSince++
			a.say(fmt.Sprintf("T%d LOADED %s%d: %s", a.proj.Track+1, string(rune('A'+a.bank)), idx+1, p.Name))
			a.log("SOUND_LOADED slot=%d name=%q user=%v", n, p.Name, user)
		}
		a.paintPads()
		return
	}
	note := a.baseNote() + idx
	if seq.IsSample(a.proj.Track) { // a sample track: the pads play its bank
		note = 36 + idx
	}
	rec := a.sq.recArm && a.sq.playing
	if on && vel > 0 {
		if rec {
			a.dsp.send("R %d %.3f", note, float64(vel)/127)
		} else {
			a.dsp.send("N %d %.3f", note, float64(vel)/127)
		}
		if a.sq.playing && a.sq.posPat >= 0 {
			a.addHit(a.proj.Track, note, float64(vel)/127, a.sq.posStep, a.proj.Pat[a.sq.posPat].Len)
		} else {
			a.addHit(a.proj.Track, note, float64(vel)/127, int(time.Since(appStart).Seconds()*8)%16, 16)
		}
		a.held[idx] = note
		a.played++
		a.proj.Tracks[a.proj.Track].Note = note
		if rec {
			a.padColour(idx, 127, 0, 0)
		} else {
			a.padColour(idx, 127, 80, 0)
		}
	} else {
		if n, ok := a.held[idx]; ok {
			if rec {
				a.dsp.send("r %d", n)
			} else {
				a.dsp.send("O %d", n)
			}
			delete(a.held, idx)
		}
		a.idleColour(idx)
	}
}

func (a *app) allNotesOff() {
	a.dsp.send("A")
	for idx := range a.held {
		delete(a.held, idx)
		a.idleColour(idx)
	}
}

func (a *app) val(key string) float64 {
	if _, ok := charDefaults[key]; ok {
		return a.char[key]
	}
	if seq.IsGlobal(key) {
		return a.proj.G[key]
	}
	if v, ok := a.sq.recVal[key]; ok {
		return v
	}
	// 0.9.25: a recorded knob move (step lock) playing now shows on the page
	if v, ok := a.sq.auto[key]; ok && a.sq.playing && (a.ui.touchKey != key || time.Since(a.ui.touchAt) > touchHold) {
		return v
	}
	return a.preset.P[key]
}

// setVal stores a value where it belongs (character, project-global or the
// selected track's sound). While recording with the sequencer running, a
// track parameter is recorded as a lock on the playing step instead.
func (a *app) setVal(key string, v float64) {
	switch {
	case isChar(key):
		a.char[key] = v
		a.dsp.set(key, v)
	case seq.IsGlobal(key):
		a.proj.G[key] = v
		a.sq.projDirty = true
		a.dsp.set(key, v)
	case a.sq.recArm && a.sq.playing && a.mode == modePlay:
		if a.sq.recVal == nil {
			a.sq.recVal = map[string]float64{}
		}
		a.sq.recVal[key] = v
		a.sq.recKnob = true
		a.dsp.send("TL %s %g", key, v)
	default:
		a.preset.P[key] = v
		a.modified = true
		a.sq.projDirty = true
		a.dsp.send("T %d %s %g", a.proj.Track, key, v)
	}
	a.dirty = true
}

func isChar(k string) bool { _, ok := charDefaults[k]; return ok }

func (a *app) loadCharacter() {
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	if b, err := os.ReadFile(filepath.Join(a.user, "character.json")); err == nil {
		var m map[string]float64
		if json.Unmarshal(b, &m) == nil {
			migrateRack(m)
			if _, ok := m["mtfx_comp_on"]; !ok && m["mt_comp"] > 0.1 { // 0.9.26's COMP type becomes the COMP stage
				defer func(p int) { a.mtCompPreset(p) }(int(math.Round(m["mt_comp"] * 3)))
			}
			for k, v := range m {
				if _, ok := charDefaults[k]; ok {
					a.char[k] = math.Max(0, math.Min(1, v))
				}
			}
		}
	}
	for k, v := range a.char {
		a.dsp.set(k, v)
	}
}

func (a *app) saveCharacter() {
	b, _ := json.MarshalIndent(a.char, "", "  ")
	os.WriteFile(filepath.Join(a.user, "character.json"), b, 0644)
}

// mixParams: K1-K3 on the TRACKS (mixer) view act on the selected track.
var mixParams = [3]param{cont("level", "LEVEL"), pPan, cont("delay_send", "DUB SEND")}
var mixAlt = [3]*param{nil, nil, ptr(cont("reverb_send", "REVERB SEND"))}

// the mixer's K3 (0.9.25): send A or B, whichever the tabs show
var (
	pSendALvl = cont("delay_send", "SEND A")
	pSendBLvl = cont("reverb_send", "SEND B")
)

// knobParam: what knob k controls in the current view.
func (a *app) knobParam(k int) *param {
	if a.mode == modeRack {
		return a.rackParams()[k]
	}
	if a.mode == modeTracks || (a.mode == modePlay && seq.IsSample(a.proj.Track)) {
		// 0.9.27: K1 LEVEL, K2 SEND A, K3 SEND B; SHIFT + K1: PAN
		switch {
		case k == 0 && a.shift:
			return &pPan
		case k == 1:
			return &pSendALvl
		case k == 2:
			return &pSendBLvl
		}
		return &mixParams[0]
	}
	return a.cur(k)
}

func (a *app) cur(k int) *param {
	pg := &pages[a.pageIdx]
	if a.shift && pg.alt[k] != nil {
		return pg.alt[k]
	}
	return &pg.p[k]
}

// norm/denorm map a parameter to 0..1 (discrete values to their index).
func (pr *param) count() int {
	if len(pr.choices) > 0 {
		return len(pr.choices)
	}
	return pr.steps
}

func (pr *param) norm(v float64) float64 {
	switch {
	case len(pr.choices) > 0:
		best := 0 // nearest choice (PAN SNAP starts from wherever pan is)
		for i, c := range pr.choices {
			if math.Abs(c-v) < math.Abs(pr.choices[best]-v) {
				best = i
			}
		}
		return float64(best) / float64(len(pr.choices)-1)
	case pr.max == pr.min:
		return 0
	}
	return (v - pr.min) / (pr.max - pr.min)
}

func (pr *param) denorm(n float64) float64 {
	n = math.Max(0, math.Min(1, n))
	switch {
	case len(pr.choices) > 0:
		return pr.choices[int(math.Round(n*float64(len(pr.choices)-1)))]
	case pr.steps > 1:
		return pr.min + math.Round(n*float64(pr.steps-1))*(pr.max-pr.min)/float64(pr.steps-1)
	}
	return pr.min + n*(pr.max-pr.min)
}

// onKnob uses "scaling" pickup: the knob always moves the value in the
// direction turned, and the remaining knob travel maps onto the remaining
// value range, so a full turn always covers the full range (no dead knob at
// an end stop, no jumps when changing page).
func (a *app) onKnob(k, value int) {
	cc := controls.CCK1 + k
	if !a.ccKnown[cc] {
		a.ccKnown[cc], a.lastCC[cc] = true, value
		return
	}
	prev := a.lastCC[cc]
	delta := value - prev
	a.lastCC[cc] = value
	if delta == 0 {
		return
	}
	if a.shift {
		a.util.shiftUsed = true
	}
	if a.mt.view && a.smp.view == smpOff { // 0.9.24: the recorder's own knobs
		a.mtKnob(k, delta)
		return
	}
	if a.smp.view != smpOff {
		a.smpKnob(k, delta)
		return
	}
	if a.mode == modeStep && a.sq.held > 0 {
		a.stepKnob(k, delta, prev)
		return
	}
	if a.mode != modePlay && a.mode != modeStep && a.mode != modeTracks && a.mode != modeRack {
		return
	}
	a.sawKnob[k] = true
	pr := a.knobParam(k)
	if pr.key == "" {
		return
	}
	// 0.9.28: pick-up. The knob does nothing until it passes the stored value,
	// then the value follows it over its whole range.
	n, ok := a.knobPick(k, delta, fmt.Sprintf("%d:%s", a.proj.Track, pr.key), pr.norm(a.val(pr.key)))
	if !ok {
		a.lastKey = pr.key
		a.touched(pr.key)
		return
	}
	_ = prev
	v := pr.denorm(n)
	if pr.key == "engine" && v != a.val("engine") {
		a.sawModel = true
		a.allNotesOff()
	}
	a.lastKey = pr.key
	a.touched(pr.key)
	a.setVal(pr.key, v)
}

// accumNorm returns the unrounded knob position for a discrete parameter if
// it still matches the stored value, so slow turns eventually step.
func (a *app) accumNorm(k int, n float64) float64 {
	pr := a.cur(k)
	if a.accum[k] > 0 && pr.denorm(a.accum[k]) == pr.denorm(n) {
		return a.accum[k]
	}
	return n
}

// fineTune nudges the last touched parameter: SHIFT + DATA turn.
func (a *app) fineTune(dir int) {
	if a.lastKey == "" {
		a.say("TURN A K KNOB FIRST, THEN SHIFT+DATA")
		return
	}
	var pr *param
	for i := range pages {
		for k := 0; k < 3; k++ {
			if pages[i].p[k].key == a.lastKey {
				pr = &pages[i].p[k]
			}
			if pages[i].alt[k] != nil && pages[i].alt[k].key == a.lastKey {
				pr = pages[i].alt[k]
			}
		}
	}
	if pr == nil {
		return
	}
	n := pr.norm(a.val(pr.key))
	step := 0.005
	if c := pr.count(); c > 1 {
		step = 1.0 / float64(c-1)
	}
	v := pr.denorm(n + float64(dir)*step)
	a.setVal(pr.key, v)
	a.usedFine = true
	a.say(fmt.Sprintf("FINE %s: %s", pr.label, pr.show(v)))
}

// selectFader points the fader at effect i. The effect it leaves glides
// back to neutral; the new one waits until the fader moves (no jumps).
func (a *app) selectFader(i int) {
	if i < 0 || i >= len(faderTargets) {
		return
	}
	if i != a.faderIdx {
		a.fxGoal[a.faderIdx] = faderTargets[a.faderIdx].neutral
		a.faderIdx = i
		a.faderVal = -1
		a.changedFader, a.faderMoved = true, false
	}
	a.sq.fxPicked = true
	a.say("FADER: " + faderTargets[i].label + " (MOVE IT TO TAKE OVER)")
	a.paintPads()
}

func (a *app) setFaderTarget(d int) {
	n := len(faderTargets)
	a.selectFader((a.faderIdx + d + n) % n)
}

// resetFX glides every effect back to neutral (SHIFT + KNOB FX, anywhere).
func (a *app) resetFX() {
	for i, t := range faderTargets {
		if a.fxOn(i) {
			a.fxLast[i] = a.fxGoal[i]
		}
		a.fxGoal[i] = t.neutral
	}
	a.faderVal = -1
	a.say("ALL FADER EFFECTS OFF")
	a.paintPads()
}

func (a *app) fxOn(i int) bool { return math.Abs(a.fxGoal[i]-faderTargets[i].neutral) > 0.02 }

// fxDefaultOn: where an effect jumps to the first time it is toggled on.
var fxDefaultOn = []float64{0.45, 0.45, 0.5, 0.42, 0.55, 0.7, 0, 0}

// toggleFX switches effect i straight off (glides back to neutral) or back
// on to where it was last (KNOB FX + pad 9-16, or tapping the active effect).
// PAN toggles centre <-> last side; VOLUME toggles full <-> silent.
func (a *app) toggleFX(i int) {
	ft := faderTargets[i]
	if a.fxOn(i) {
		a.fxLast[i] = a.fxGoal[i]
		a.fxGoal[i] = ft.neutral
		a.say(ft.label + " OFF")
	} else {
		v := a.fxLast[i]
		if math.Abs(v-ft.neutral) <= 0.02 {
			v = fxDefaultOn[i]
		}
		a.fxGoal[i] = v
		a.say(fmt.Sprintf("%s ON (%s)", ft.label, fxAmount(i, v)))
	}
	if i == a.faderIdx {
		a.faderVal = -1 // the fader takes over again when it moves
	}
	a.sq.fxToggled = true
	a.paintPads()
}

// onFader: the fader sets a target and the effect glides to it (see
// glideFader), so moves feel smooth. Holding SHIFT makes the fader fine:
// it moves the effect a quarter as far, relative to where it is.
func (a *app) onFader(v int, prev int) {
	// 0.9.29: only the old chop / slice scrubber (and NOTE REPEAT + fader) take
	// the fader; on the SHAPER, SAMPLE SELECT and PAD BANKS it drives the fader FX
	if a.smp.nrHeld || a.smp.view == smpChop || a.smp.view == smpSlice {
		a.smpFader(v, prev)
		return
	}
	a.sawFader = true
	if a.changedFader {
		a.faderMoved = true
	}
	i := a.faderIdx
	if a.shift && a.faderVal >= 0 {
		a.fxGoal[i] = math.Max(0, math.Min(1, a.fxGoal[i]+float64(v-prev)/127*0.25))
	} else {
		a.fxGoal[i] = float64(v) / 127
	}
	a.faderVal = a.fxGoal[i]
	a.dirty = true
}

// glideFader runs every UI loop (~5 ms): every effect follows its goal in
// steps of 0.05 (~0.1 s for a full sweep) so nothing jumps.
func (a *app) glideFader() {
	for i := range faderTargets {
		d := a.fxGoal[i] - a.fxNow[i]
		if d == 0 {
			continue
		}
		if math.Abs(d) <= 0.05 {
			a.fxNow[i] = a.fxGoal[i]
		} else {
			a.fxNow[i] += math.Copysign(0.05, d)
		}
		a.dsp.set(faderTargets[i].key, a.fxNow[i])
	}
}

func (a *app) tapTempo() {
	now := time.Now()
	if len(a.taps) > 0 && now.Sub(a.taps[len(a.taps)-1]) > 2*time.Second {
		a.taps = nil
	}
	a.taps = append(a.taps, now)
	if len(a.taps) > 5 {
		a.taps = a.taps[1:]
	}
	if len(a.taps) >= 3 {
		span := a.taps[len(a.taps)-1].Sub(a.taps[0]).Seconds() / float64(len(a.taps)-1)
		bpm := math.Round(math.Max(60, math.Min(180, 60/span)))
		a.setVal("tempo", bpm)
		a.tapped = true
		a.say(fmt.Sprintf("TEMPO %d BPM", int(bpm)))
	} else {
		a.say("TAP...")
	}
}

func (a *app) onButton(note byte, down bool) (exit bool) {
	if down { // which buttons get pressed, and which send anything at all
		a.log("BTN %d mode=%d shift=%t", note, a.mode, a.shift)
	}
	if down && note != controls.BtnShift {
		a.util.shiftUsed = true
	}
	if down && a.sq.muteMode != muteOff { // any view button leaves MUTE
		switch note {
		case controls.BtnMute, controls.BtnShift, controls.BtnPlay, controls.BtnStop, controls.BtnSeqRecord,
			controls.BtnSampleRecord, controls.BtnTapTempo, controls.BtnErase, controls.BtnNoteRepeat:
		default:
			a.sq.muteMode = muteOff
			a.paintPads()
		}
	}
	if a.util.menu { // PROJECT menu: DATA push does, any other button closes
		if !down {
			return
		}
		switch note {
		case controls.BtnDataPush, controls.BtnScreenMiddle:
			a.utilMenuPush()
			return
		case controls.BtnShift:
		default:
			a.util.menu = false
			a.dirty = true
			return
		}
	}
	if a.mtButton(note, down) { // MIXTAPE: the cassette's screen buttons
		return
	}
	if a.smpButton(note, down) { // FLUX part 2: PAD FX, and the sampler views' buttons
		return
	}
	switch note {
	case controls.BtnShift:
		a.shift, a.dirty = down, true
		a.utilShift(down)
		return
	case controls.BtnTapTempo:
		a.tapHeld = down
		if !down {
			if a.tapTurned {
				a.tapTurned = false
			} else if a.shift {
				a.setFaderTarget(1)
			} else {
				a.tapTempo()
			}
		}
		a.dirty = true
		return
	case controls.BtnSampleRecord:
		a.recHeld, a.dirty = down, true
		a.mtRecButton(down) // a tap: MIXTAPE start / stop
		return
	case controls.BtnNoteRepeat:
		if down {
			a.dsp.set("throw", 1)
			a.threw = true
			a.say("DUB THROW!")
		} else {
			a.dsp.set("throw", 0)
		}
		return
	}
	if a.seqHeldButton(note, down) {
		return
	}
	if !down {
		return
	}
	if a.mode == modeConfirmExit {
		if note == controls.BtnStop {
			return true
		}
		a.mode = modePlay
		a.say("EXIT CANCELLED")
		a.paintPads()
		return
	}
	if a.onSeqButton(note) {
		return
	}
	switch note {
	case controls.BtnStop:
		a.allNotesOff()
		if a.shift {
			a.mode = modeConfirmExit
			a.dirty = true
			a.log("EXIT_CONFIRM_SHOWN")
		} else {
			a.hardStop()
		}
	case controls.BtnMinus, controls.BtnPlus:
		a.allNotesOff()
		if note == controls.BtnMinus && a.octave > -2 {
			a.octave--
		}
		if note == controls.BtnPlus && a.octave < 4 {
			a.octave++
		}
		a.say(fmt.Sprintf("OCTAVE %d", a.octave))
	case controls.BtnPadBank:
		if a.mode == modePreset {
			a.bank = (a.bank + 1) % 4
			a.changedBank = true
			a.say(fmt.Sprintf("SOUND BANK %s  (A = FACTORY, B-D = YOURS)", string(rune('A'+a.bank))))
			a.paintPads()
		}
	case controls.BtnSampleSelect:
		a.allNotesOff()
		if a.mode == modePreset {
			a.mode = modePlay
			a.say("PLAY")
		} else {
			a.mode = modePreset
			a.say("PAD = LOAD, HOLD SAMPLE RECORD + PAD = SAVE")
		}
		a.paintPads()
	case controls.BtnScreenLeft: // page down
		a.turnPage(-1)
	case controls.BtnScreenRight: // page up
		a.turnPage(1)
	case controls.BtnScreenMiddle: // the other face of this view
		a.flipFace()
	case controls.BtnDataPush:
		switch {
		case a.sq.eraseHeld && !a.tourDone:
			a.tourIdx++
			a.dirty = true
		case a.shift:
			a.resetPress()
		case a.mode == modeRack: // 0.9.27: master comp / limiter on / off
			a.rackPush()
		case a.mode == modeTracks && a.ui.mixer: // 0.9.25: K3 switches between send A and B
			a.ui.sendB = !a.ui.sendB
			s := 0
			if a.ui.sendB {
				s = 1
			}
			a.say(fmt.Sprintf("K3 SENDS TO %s · %s", []string{"A", "B"}[s], a.sendFx(s)))
			a.dirty = true
		}
	}
	return
}

func (a *app) turnPage(d int) {
	if a.mode == modeTracks { // screen buttons / DATA: notes <-> mixer
		a.ui.mixer = !a.ui.mixer
		a.dirty = true
		return
	}
	n := len(pages)
	a.pageIdx = ((a.pageIdx+d)%n + n) % n
	a.accum = [3]float64{}
	a.dirty = true
	if a.mode == modePlay {
		a.startSlide(d)
	}
}

// ------------------------------------------------------------------ screen

const (
	cBg     = 0x00140F0A // warm dark
	cWhite  = 0x00FFF4E0
	cAmber  = 0x00FFB040
	cGreen  = 0x0090E070
	cGrey   = 0x00A09080
	cOrange = 0x00FF7020
)

func (a *app) draw() {
	s := a.scr
	if s == nil {
		return
	}
	if t0 := time.Now(); a.drawV2() {
		if a.loopShown() {
			a.overlayLength(s.C)
		}
		if a.tapHeld {
			a.overlayTempo(s.C)
		}
		d := time.Since(t0)
		a.present()
		a.uiStats(d)
		return
	}
	s.Fill(cBg)
	mod := ""
	if a.modified {
		mod = "*"
	}
	tc := trackColours[a.proj.Track]
	s.Rect(0, 0, 4, 38, rgb7(tc))
	s.Text(8, 4, 2, cWhite, fmt.Sprintf("T%d %s%s", a.proj.Track+1, a.preset.Name, mod))
	a.drawSeqStatus(24)
	switch a.mode {
	case modeStep, modeTracks, modePatterns, modeProject, modeFX:
		a.drawSeq()
	case modeConfirmExit:
		s.Fill(0x00300808)
		s.Text(6, 40, 2, cWhite, "EXIT TO STOCK?")
		s.Text(6, 80, 1, cAmber, "PRESS STOP TO CONFIRM.")
		s.Text(6, 94, 1, cAmber, "ANY OTHER BUTTON CANCELS.")
		s.Text(6, 120, 1, cGrey, "YOUR SOUND IS SAVED; THE NORMAL MPC")
		s.Text(6, 132, 1, cGrey, "SOFTWARE RETURNS IN ABOUT 10 S.")
		s.Flush()
		return
	case modePreset:
		s.Text(6, 44, 2, cGreen, "SOUNDS "+string(rune('A'+a.bank)))
		s.Text(6, 68, 1, cAmber, fmt.Sprintf("BANK %s  PAD = LOAD   PAD BANK = NEXT BANK", string(rune('A'+a.bank))))
		s.Text(6, 80, 1, cAmber, "HOLD SAMPLE RECORD + PAD = SAVE THERE")
		s.Text(6, 92, 1, cGrey, "BLUE FACTORY  GREEN YOURS  WHITE NOW")
		if a.recHeld {
			s.Text(6, 110, 2, cOrange, "SAVE: PICK A PAD")
		}
		s.Text(6, 132, 1, cGrey, "SAMPLE SELECT: BACK TO PLAY")
	default:
		pg := &pages[a.pageIdx]
		s.Text(6, 42, 2, cGreen, fmt.Sprintf("%s  %d/%d", pg.name, a.pageIdx+1, len(pages)))
		for k := 0; k < 3; k++ {
			pr := a.cur(k)
			c := uint32(cWhite)
			if a.shift && pg.alt[k] != nil {
				c = cOrange
			}
			shown := pr.show(a.val(pr.key))
			if pr.key == "harmonics" {
				if nm := harmonicsName(int(a.val("engine")), a.val("harmonics")); nm != "" {
					shown = nm
				}
			}
			if a.sq.resetKey == pr.key && time.Since(a.sq.resetAt) < 400*time.Millisecond {
				s.Rect(0, 64+k*22, 320, 18, 0x00604010) // just reset
			}
			line := fmt.Sprintf("K%d %-9s %s", k+1, pr.label, shown)
			sc := 2
			if len(line) > 26 {
				sc = 1
			}
			s.Text(6, 66+k*22, sc, c, line)
		}
		var hints []string
		for k, al := range pg.alt {
			if al != nil {
				hints = append(hints, fmt.Sprintf("SHIFT: %s ON K%d", al.label, k+1))
			}
		}
		s.Text(6, 134, 1, cGrey, strings.Join(hints, "  "))
		a.drawStrip(200)
	}
	if !a.tourDone && a.tourIdx < len(a.tour) {
		s.Text(6, 150, 1, cOrange, fmt.Sprintf("TOUR %d/%d  (ERASE+PUSH DATA SKIPS)", a.tourIdx+1, len(a.tour)))
		for i, ln := range strings.Split(a.tour[a.tourIdx].text, "\n") {
			sc := 2
			if len(ln) > 26 {
				sc = 1
			}
			s.Text(6, 164+i*18, sc, cAmber, ln)
		}
	}
	if a.loopShown() {
		a.drawLengthOverlay()
	}
	if a.tapHeld {
		s.Fill(cBg)
		s.Text(6, 20, 2, cGreen, "SET TEMPO")
		s.Text(40, 80, 6, cWhite, fmt.Sprintf("%d", int(math.Round(a.proj.G["tempo"]))))
		s.Text(6, 150, 1, cAmber, "KEEP HOLDING TAP TEMPO, TURN DATA: 1 BPM PER CLICK")
		s.Text(6, 164, 1, cGrey, "OR JUST TAP IT IN (RELEASE WITHOUT TURNING)")
	}
	if a.status != "" && time.Since(a.statusAt) < 4*time.Second {
		s.Text(6, 212, 1, cWhite, a.status)
	}
	fv := "--"
	if a.faderVal >= 0 {
		fv = fmt.Sprintf("%d", int(a.faderVal*99+0.5))
	}
	s.Text(6, 228, 1, cGrey, fmt.Sprintf("FADER %s %s  KNOB FX+PAD: PICK", faderTargets[a.faderIdx].label, fv))
	a.present()
}

// ------------------------------------------------------------------ session

type savedState struct {
	Octave   int    `json:"octave"`
	Page     int    `json:"page"`
	FaderIdx int    `json:"fader"`
	Bank     int    `json:"bank"`
	PageName string `json:"page_name"`
}

func (a *app) projectPath() string { return filepath.Join(a.user, "project_current.json") }

// saveSession writes the whole project (all 8 sounds, patterns, mixer) and
// the UI state. Called on exit, every 30 s when something changed, and when
// a project is saved to a slot.
func (a *app) saveSession() {
	if err := a.proj.Save(a.projectPath()); err != nil {
		a.log("PROJECT_SAVE_FAILED %v", err)
	} else {
		a.sq.projDirty = false
	}
	b, _ := json.MarshalIndent(savedState{a.octave, a.pageIdx, a.faderIdx, a.bank, pages[a.pageIdx].name}, "", "  ")
	os.WriteFile(filepath.Join(a.user, "state_v7.json"), b, 0644)
	a.log("SESSION_SAVED project=%q", a.proj.Name)
}

func (a *app) loadSession() {
	a.octave = 0
	var st savedState
	if b, err := os.ReadFile(filepath.Join(a.user, "state_v7.json")); err == nil && json.Unmarshal(b, &st) == nil {
		if st.Octave >= -2 && st.Octave <= 4 {
			a.octave = st.Octave
		}
		if st.PageName != "" {
			a.pageIdx = pageIndex(st.PageName) // page order can change between versions
		}
		if st.FaderIdx >= 0 && st.FaderIdx < len(faderTargets) {
			a.faderIdx = st.FaderIdx
		}
		if st.Bank >= 0 && st.Bank < 4 {
			a.bank = st.Bank
		}
	}
	// 0.9.28: 16 tracks. The first time, keep a copy of the 8-track session and slots.
	if b, err := os.ReadFile(a.projectPath()); err == nil {
		keep := filepath.Join(filepath.Dir(a.projectPath()), "before_0.9.28")
		if _, err := os.Stat(keep); err != nil {
			os.MkdirAll(keep, 0755)
			os.WriteFile(filepath.Join(keep, filepath.Base(a.projectPath())), b, 0644)
			if ents, err := os.ReadDir(a.projectsDir()); err == nil {
				for _, e := range ents {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
						if d, err := os.ReadFile(filepath.Join(a.projectsDir(), e.Name())); err == nil {
							os.WriteFile(filepath.Join(keep, e.Name()), d, 0644)
						}
					}
				}
			}
			a.log("PROJECTS_KEPT %s", keep)
		}
	}
	p, err := seq.Load(a.projectPath())
	if err != nil {
		a.log("PROJECT_DEFAULT reason=%v", err)
		p = seq.Default()
	}
	a.setProject(p)
	a.log("SESSION_LOADED project=%q", a.proj.Name)
}

// ------------------------------------------------------------------ main

func pcmDevice() string {
	if t, err := os.Readlink("/dev/snd/by-path/platform-sound"); err == nil {
		if i := strings.LastIndex(t, "controlC"); i >= 0 {
			if n, err := strconv.Atoi(t[i+8:]); err == nil {
				return fmt.Sprintf("/dev/snd/pcmC%dD0p", n)
			}
		}
	}
	return "/dev/snd/pcmC2D0p"
}

func main() {
	a := &app{held: map[int]int{}}
	a.user = filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "user")
	os.MkdirAll(a.soundsDir(), 0755)
	lk := os.NewFile(link.FD, "link")
	send := func(c byte) { lk.Write([]byte{c}) }
	t0 := time.Now()
	ms := func() int64 { return time.Since(t0).Milliseconds() }
	a.log("FMSTEP_START version=%s uptime=%s", appVersion, uptime())

	// Screen first, so the startup moment plays while everything else loads.
	var err error
	if a.scr, err = panel.OpenScreen("/dev/dri/card0"); err != nil {
		a.log("SCREEN_FAILED %v", err)
		a.scr = nil
	}
	tScreen := ms()
	exe, _ := os.Executable()
	in := &intro{ready: make(chan struct{}), done: make(chan struct{}), ver: appVersion,
		min: introBudget(filepath.Join(filepath.Dir(filepath.Dir(exe)), "launch_uptime"), uptimeSec())}
	a.log("INTRO_BUDGET ms=%d", in.min.Milliseconds())
	if a.scr != nil {
		go runIntro(a.scr, in, a.log)
	} else {
		close(in.done)
	}

	d, err := startDSP(filepath.Join(filepath.Dir(exe), "fmstep-dsp"), pcmDevice(), a.log)
	if err != nil {
		a.log("DSP_FAILED %v", err)
		os.Exit(2) // the supervisor counts a failed launch and restores stock
	}
	a.dsp = d
	a.dsp.send("SPEC 1") // visualizer data (EQ page, 16 LEVELS view)
	tDSP := ms()

	var dev string
	if a.cs, dev, err = panel.OpenControlSurface(true); err != nil {
		a.log("CONTROL_SURFACE_FAILED %v", err)
		a.cs = nil
	} else {
		a.log("CONTROL_SURFACE_OK %s", dev)
		in.cs.Store(a.cs)
	}
	tSurface := ms()
	a.dsp.set("master", 0) // silent until MAIN VOLUME is known
	a.dsp.set("fader_cutoff", 1)
	for i, t := range faderTargets {
		a.fxNow[i], a.fxGoal[i] = t.neutral, t.neutral
		a.dsp.set(t.key, t.neutral)
	}
	a.faderVal = -1
	a.loadCharacter()
	a.padsInit()
	a.mtSendRec() // 0.9.27: the take's TAPE FX chain
	a.loadSession()

	_, err = os.Stat(filepath.Join(a.user, "tour_v8_done"))
	a.tourDone = err == nil
	a.tour = seqTour
	if _, err := os.Stat(filepath.Join(a.user, "tour_v7_done")); err == nil {
		a.tour = liveTour // sequencer tour already done: just the new controls
	}
	tSession := ms()

	// Loaded: tell the supervisor, let the moment land, then hand over.
	send(link.Ready)
	close(in.ready)
	for waiting := true; waiting; {
		select {
		case <-in.done:
			waiting = false
		case <-time.After(100 * time.Millisecond):
			send(link.Heartbeat)
		}
	}
	a.sq.padKnown = [16]bool{} // the intro lit the pads: repaint them all
	a.paintPads()
	if a.scr != nil {
		a.scr.Invalidate()
	}
	a.dirty = true
	a.log("READY")
	a.log("STARTUP screen_ms=%d dsp_ms=%d surface_ms=%d session_ms=%d playable_ms=%d uptime=%s",
		tScreen, tDSP, tSurface, tSession, ms(), uptime())
	a.loadKeep()
	a.log("KEEPERS %s", strings.Join(a.keepList(), ", "))

	buf := make([]byte, 1024)
	var msg []byte
	lastDraw := time.Time{}
	exit := false
	for !exit {
		send(link.Heartbeat)
		if a.cs != nil {
			for _, b := range a.cs.Read(buf) {
				if b >= 0xF8 {
					continue
				}
				if b&0x80 != 0 {
					msg = []byte{b}
					continue
				}
				if len(msg) == 0 {
					continue
				}
				msg = append(msg, b)
				if len(msg) < 3 && msg[0]&0xF0 != 0xC0 && msg[0]&0xF0 != 0xD0 {
					continue
				}
				st, d1, d2 := msg[0], msg[1], byte(0)
				if len(msg) > 2 {
					d2 = msg[2]
				}
				msg = msg[:1]
				switch {
				case st == 0xA9: // pad pressure
					for i, n := range controls.PadNotes {
						if n == d1 {
							a.onPressure(i, d2)
						}
					}
				case st == 0x99 || st == 0x89:
					for i, n := range controls.PadNotes {
						if n == d1 {
							a.onPad(i, d2, st == 0x99 && d2 > 0)
						}
					}
				case st == 0xB0 && d1 >= controls.CCK1 && d1 <= controls.CCK3:
					a.onKnob(int(d1-controls.CCK1), int(d2))
				case st == 0xB0 && d1 == controls.CCVolume:
					a.dsp.set("master", float64(d2)/127)
				case st == 0xB0 && d1 == controls.CCFader:
					if a.ccKnown[controls.CCFader] && int(d2) != a.lastCC[controls.CCFader] {
						a.onFader(int(d2), a.lastCC[controls.CCFader])
					}
					a.ccKnown[controls.CCFader], a.lastCC[controls.CCFader] = true, int(d2)
				case st == 0xB0 && d1 == controls.CCData && a.shift && a.mt.view: // 0.9.27: SHIFT + DATA scrolls the tape (no sheet)
					a.util.shiftUsed = true
					a.mtDataTurn(d2)
				case st == 0xB0 && d1 == controls.CCData && a.util.menu:
					if d2 == 1 {
						a.utilMenuData(1)
					} else if d2 == 127 {
						a.utilMenuData(-1)
					}
				case st == 0xB0 && d1 == controls.CCData && a.tapHeld:
					step := 1.0
					if d2 == 127 {
						step = -1
					}
					bpm := math.Max(40, math.Min(200, math.Round(a.proj.G["tempo"])+step))
					a.setVal("tempo", bpm)
					a.tapTurned = true
					a.say(fmt.Sprintf("TEMPO %d BPM", int(bpm)))
				case st == 0xB0 && d1 == controls.CCData && a.mt.view:
					a.mtDataTurn(d2)
				case st == 0xB0 && d1 == controls.CCData && a.smp.view != smpOff:
					a.smpData(d2)
				case st == 0xB0 && d1 == controls.CCData && a.seqData(d2):
				case st == 0xB0 && d1 == controls.CCData && a.shift:
					a.util.shiftUsed = true
					if d2 == 1 {
						a.fineTune(1)
					} else if d2 == 127 {
						a.fineTune(-1)
					}
				case st == 0xB0 && d1 == controls.CCData:
					if d2 == 1 {
						a.turnPage(1)
					} else if d2 == 127 {
						a.turnPage(-1)
					}
				case st == 0x90 || st == 0x80:
					if a.onButton(d1, st == 0x90 && d2 > 0) {
						exit = true
					}
				}
			}
		}
		if !a.tourDone {
			for a.tourIdx < len(a.tour) && a.tour[a.tourIdx].done(a) {
				a.log("TOUR_STEP_DONE %d", a.tourIdx+1)
				a.tourIdx++
				a.dirty = true
			}
			if a.tourIdx >= len(a.tour) {
				a.tourDone = true
				os.WriteFile(filepath.Join(a.user, "tour_v7_done"), []byte("1\n"), 0644)
				os.WriteFile(filepath.Join(a.user, "tour_v8_done"), []byte("1\n"), 0644)
				a.say("TOUR COMPLETE - GO PLAY OUT!")
				a.log("TOUR_COMPLETE")
			}
		}
		a.seqTick()
		a.mtTick()
		a.padsTick()
		if a.dirty || time.Now().Before(a.ui.animUntil) || time.Since(lastDraw) > 300*time.Millisecond {
			a.draw()
			a.dirty = false
			lastDraw = time.Now()
		}
		a.glideFader()
		time.Sleep(5 * time.Millisecond)
	}

	a.log("EXIT_TO_STOCK begin")
	if a.mt.rec || a.mt.stopping { // finish the take properly first
		a.mtStop()
		for t0 := time.Now(); time.Since(t0) < 4*time.Second && a.mt.stopping; time.Sleep(20 * time.Millisecond) {
			a.mtTick()
		}
	}
	a.mtListenStop()
	a.padsSave(true)
	a.lightsOff() // button lights off for the stock software
	a.dsp.send("STOP")
	a.allNotesOff()
	if a.scr != nil {
		a.scr.Fill(0)
		a.scr.Text(6, 90, 2, cWhite, "SAVING...")
		a.scr.Flush()
	}
	a.saveSession()
	a.saveCharacter()
	a.dsp.stop()
	a.log("DSP_STOPPED")
	if a.scr != nil {
		a.scr.Fill(0)
		a.scr.Text(6, 90, 2, cWhite, "RETURNING TO STOCK")
		a.scr.Flush()
		a.scr.Close()
	}
	if a.cs != nil {
		for i := 0; i < 16; i++ {
			a.padColour(i, 0, 0, 0)
		}
		a.cs.Close()
	}
	a.log("EXIT_TO_STOCK done")
	send(link.ExitStock)
	os.Exit(0)
}

// EQ band frequencies (must match fmstep-dsp): low shelf 40-400 Hz, mid
// peak 200 Hz-5 kHz, high shelf 2-16 kHz.
func eqLowHz(v float64) float64  { return 40 * math.Pow(10, v) }
func eqMidHz(v float64) float64  { return 200 * math.Pow(25, v) }
func eqHighHz(v float64) float64 { return 2000 * math.Pow(8, v) }

func hzShow(hz float64) string {
	if hz >= 1000 {
		return fmt.Sprintf("%.1f KHZ", hz/1000)
	}
	return fmt.Sprintf("%d HZ", int(hz+0.5))
}
