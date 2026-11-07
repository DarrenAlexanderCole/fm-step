package main

// FLUX part 2 in the app: the LIBRARY, CHOP and SLICE views and their
// controls. The sound engine side (WAV loading, slice voices, scrub, the chop
// track) is dsp/sampler.h; this file sends it commands and draws what it
// reports. (0.9.22: the rolling TAPE view is gone; MIXTAPE takes show up in
// the LIBRARY instead, see mixtape.go.)
//
//   PAD FX            from any view: LIBRARY · in LIBRARY: LOAD · in CHOP / SLICE: back to LIBRARY
//   SHIFT + PAD FX    CHOP: save the chop to the card
//   DATA              LIBRARY: pick a sample, kit or take · CHOP / SLICE: the chop track
//   K1                LIBRARY, a take: where the 16 s to chop start
//   middle button     LIBRARY: LISTEN to a take (else to CHOP) · CHOP ↔ SLICE
//   pads              play the slices (CHOP / SLICE); the last one hit is selected
//                     · SEQ REC + PLAY: recorded onto the chop track
//   pad pressure      DRIFT: drags the slice (slower, with tape wow), as much as K1 allows
//   fader             CHOP / SLICE: scrub the reel forward and backward
//   NOTE REPEAT+fader the reel spins with the low end; the fader sets how hard
//   K1-K3             CHOP: DRIFT, CHOP (16 / 8 / BEATS / MAN), MOTION
//                     SLICE: START (MAN: the marker), END, PITCH (SHIFT + K3: reverse)
//   view buttons      leave (SEQ, SAMPLE, KNOB FX, 16 LEVELS, …); transport keeps working

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fmstep/internal/gfx"
)

// ---------------------------------------------------------------- engine data

// smpData is what the engine reports (written by the DSP reader goroutine).
type smpData struct {
	mu       sync.Mutex
	tw       []uint16 // tape levels: lo<<8 | hi per 1024 frames, newest last
	gw       [256][2]uint8
	gwf      [2048]uint8 // 0.9.24: fine peaks for the pad previews (GWF)
	grabLen  int
	grabBars int
	grabBpm  float64
	grabHow  int      // 0 grab, 1 grab on bar lines, 2 file, 3 kit
	grabSeq  int      // bumps on every new buffer
	gt       []int    // hits (frames)
	kit      [][2]int // kit sounds (frames)
	waitMs   int
	waitAt   time.Time
	gz       [128][2]uint8
	gzStart  int
	gzEnd    int
	gzSeq    int
	gzm      [296][2]int8 // GZM: min / max per column of the zoom trim window
	gzmStart int
	gzmEnd   int
	msg      string // GRABEMPTY / LOADERR / SAVED / SAVEERR, for the UI to say
	msgSeq   int
}

func hexPairs(s string, out [][2]uint8) bool {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) < 2*len(out) {
		return false
	}
	for i := range out {
		out[i] = [2]uint8{b[2*i], b[2*i+1]}
	}
	return true
}

func atoiAll(f []string) []int {
	out := make([]int, 0, len(f))
	for _, x := range f {
		v, err := strconv.Atoi(x)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

// parse handles the sampler lines; returns false for anything else.
func (d *smpData) parse(line string, logf func(string, ...any)) bool {
	switch {
	case strings.HasPrefix(line, "TW "):
		b, err := hex.DecodeString(line[3:])
		if err != nil {
			return true
		}
		d.mu.Lock()
		for i := 0; i+1 < len(b); i += 2 {
			d.tw = append(d.tw, uint16(b[i])<<8|uint16(b[i+1]))
		}
		if len(d.tw) > 4096 {
			d.tw = append(d.tw[:0], d.tw[len(d.tw)-2048:]...)
		}
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "GWF "):
		d.mu.Lock()
		for i := 0; i < 2048 && 2*i+6 <= len(line); i++ {
			v, _ := strconv.ParseUint(line[4+2*i:6+2*i], 16, 8)
			d.gwf[i] = uint8(v)
		}
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "GW "):
		d.mu.Lock()
		hexPairs(line[3:], d.gw[:])
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "GT "):
		v := atoiAll(strings.Fields(line)[1:])
		d.mu.Lock()
		if len(v) > 0 {
			d.gt = v[1:]
		}
		if d.grabHow != 3 {
			d.grabSeq++ // GT is the last line of a grab / load (KIT follows for kits)
		}
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "KIT "):
		v := atoiAll(strings.Fields(line)[1:])
		d.mu.Lock()
		d.kit = nil
		for i := 1; i+1 < len(v); i += 2 {
			d.kit = append(d.kit, [2]int{v[i], v[i+1]})
		}
		d.grabSeq++
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	case strings.HasPrefix(line, "GZ "):
		f := strings.Fields(line)
		if len(f) != 4 {
			return true
		}
		d.mu.Lock()
		d.gzStart, _ = strconv.Atoi(f[1])
		d.gzEnd, _ = strconv.Atoi(f[2])
		if hexPairs(f[3], d.gz[:]) {
			d.gzSeq++
		}
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "GZM "):
		f := strings.Fields(line)
		if len(f) != 4 {
			return true
		}
		b, err := hex.DecodeString(f[3])
		if err != nil || len(b) < 2*296 {
			return true
		}
		d.mu.Lock()
		d.gzmStart, _ = strconv.Atoi(f[1])
		d.gzmEnd, _ = strconv.Atoi(f[2])
		for i := 0; i < 296; i++ {
			d.gzm[i] = [2]int8{int8(b[2*i]), int8(b[2*i+1])}
		}
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "GRAB "):
		f := strings.Fields(line)
		if len(f) >= 5 {
			d.mu.Lock()
			d.grabLen, _ = strconv.Atoi(f[1])
			d.grabBars, _ = strconv.Atoi(f[2])
			d.grabBpm, _ = strconv.ParseFloat(f[3], 64)
			d.grabHow, _ = strconv.Atoi(f[4])
			d.waitMs = 0
			d.mu.Unlock()
		}
		logf("DSP %s", line)
		return true
	case strings.HasPrefix(line, "GRABWAIT "):
		ms, _ := strconv.Atoi(strings.TrimSpace(line[9:]))
		d.mu.Lock()
		d.waitMs, d.waitAt = ms, time.Now()
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	case line == "GRABEMPTY" || strings.HasPrefix(line, "LOADERR ") || strings.HasPrefix(line, "SAVED ") || strings.HasPrefix(line, "SAVEERR "):
		d.mu.Lock()
		d.msg = line
		d.msgSeq++
		d.waitMs = 0
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	}
	return false
}

// ---------------------------------------------------------------- app state

const (
	smpOff  = iota
	smpTape // the LIBRARY (the card's samples, kits and MIXTAPE takes)
	smpChop
	smpSlice
	smpShaper // 0.9.26: CHOP opens the SHAPER (pads_ui.go); CHOP / SLICE are no longer reached
	smpBanks  // 0.9.27: PAD BANK, the 8 banks
	smpRecIn  // 0.9.27: SAMPLE IN
)

const (
	chop16 = iota
	chop8
	chopBeats
	chopMan
)

var (
	smpLengths = []int{1, 2, 4}
	smpMotions = []string{"ONE SHOT", "GATE", "LOOP"}
	smpChops   = []string{"16", "8", "BEATS", "MAN"}
	// chopOrder: how the CHOP knob and tabs list the modes (0.9.24: MAN first)
	chopOrder  = []int{chopMan, chop16, chop8, chopBeats}
	smpSources = []string{"JAM", "CARD", "INPUT", "USB"}
)

type sliceEdit struct {
	startFr, endFr int // trims (frames, from the chop's own edges)
	pitch          int // semitones
	rev            bool
}

type cardEntry struct {
	name string
	kit  bool
	take bool    // a MIXTAPE take
	secs float64 // length (loops and takes)
	info string  // "KIT · 12" / "2.0 S" / "4:12"
	path string
}

type smpState struct {
	view     int
	source   int // 0 JAM, 1 CARD
	lenIdx   int // LENGTH: 1 / 2 / 4 bars
	drift    float64
	chop     int // chop16 / chop8 / chopBeats / chopMan
	motion   int
	edits    [16]sliceEdit
	marks    [16]int // MAN: slice starts (frames)
	sel      int
	grabSeq  int // last buffer the app has set slices for
	msgSeq   int
	grabbing bool
	acc      [3]int // knob accumulators for stepped settings
	acc2     int    // the knob turned last (for the bars)
	dataAcc  int
	chopTrk  int // the track whose notes play slices (-1 none)
	srcName  string
	// the card
	files    []cardEntry
	fileIdx  int
	scanned  time.Time
	takeFrom float64 // LIBRARY, a take: where the 16 s start (seconds)
	takeFor  string  // the take takeFrom belongs to
	takeAcc  float64
	// playing slices (for the reel and pads)
	hitAt   [16]time.Time
	held    [16]bool
	press   [16]float64
	scrubX  float64
	scrubAt time.Time
	spin    float64 // NOTE REPEAT + fader: how hard the low end drives the reel
	nrHeld  bool
	angle   float64
	lastT   time.Time
	gzFor   int // slice the zoom was asked for
	// SLICE: the zoom trim
	zoomW   int     // frames across the screen (0 = the whole buffer)
	focus   int     // the marker the view follows: 0 IN, 1 OUT
	zh      float64 // wave height (SHIFT + K3)
	gzmKey  int
	trimSel int
	inited  bool
	// SHAPER (0.9.26)
	env      [16]padEnv
	envDirty bool
	envSaved time.Time
	envKnob  int
	envSus   bool
	ask      int  // the empty pad asking for a sound (-1 none)
	btnHeld  bool // the middle button is playing the pad
}

func (a *app) samplesDir() string {
	return filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "samples")
}

const samplesReadme = `FM-STEP samples

Put WAV files here to chop them in FM-STEP (PAD FX opens the LIBRARY).
A folder of WAV files is a KIT: one sound per pad, in name order (up to 16).
SHIFT + PAD FX in CHOP saves the current chop here as "chop 001.wav".
Any rate, mono or stereo, 16/24/32-bit; up to 16 seconds are used.
`

func (a *app) smpInit() {
	if a.smp.inited {
		return
	}
	a.smp.inited = true
	a.smp.lenIdx = 1 // 2 bars
	a.smp.drift = 0.4
	a.smp.gzFor = -1
	a.smp.trimSel = -1
	a.smp.chopTrk = 7 // T8
	a.smp.ask = -1
	if dir := a.samplesDir(); os.Getenv("FMSTEP_SD") != "" {
		os.MkdirAll(dir, 0755)
		if _, err := os.Stat(filepath.Join(dir, "README.txt")); err != nil {
			os.WriteFile(filepath.Join(dir, "README.txt"), []byte(samplesReadme), 0644)
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (a *app) smpEnter(view int) {
	a.smpInit()
	prev := a.smp.view
	a.smp.view = view
	if a.dsp != nil {
		a.dsp.send("SCRUB %d", b2i(view == smpChop || view == smpSlice))
	}
	if view == smpTape && prev != smpTape {
		a.smpScan()
	}
	if view != smpTape {
		a.mtListenStop()
	}
	if a.scr != nil {
		a.scr.Invalidate()
	}
	a.dirty = true
	a.paintPads()
	a.log("SAMPLER_VIEW %d", view)
}

func (a *app) smpLeave() {
	if a.smp.view == smpOff {
		return
	}
	if a.dsp != nil {
		a.dsp.send("SCRUB 0")
	}
	a.mtListenStop()
	a.padsSave(true)
	a.smp.view = smpOff
	if a.scr != nil {
		a.scr.Invalidate()
	}
	a.dirty = true
	for i := range a.sq.padKnown {
		a.sq.padKnown[i] = false
	}
	a.paintPads()
}

func (a *app) smpGrabbed() bool {
	a.dsp.smp.mu.Lock()
	defer a.dsp.smp.mu.Unlock()
	return a.dsp.smp.grabLen > 0
}

func (a *app) smpIsKit() bool {
	a.dsp.smp.mu.Lock()
	defer a.dsp.smp.mu.Unlock()
	return a.dsp.smp.grabHow == 3
}

func (a *app) smpCfg() {
	a.dsp.send("CHOPCFG %d %.3f", a.smp.motion, a.smp.drift)
}

// smpSync picks up engine news: a new buffer (slices are sent, CHOP opens)
// and messages (empty tape, load errors, saved).
func (a *app) smpSync(force bool) {
	d := &a.dsp.smp
	d.mu.Lock()
	seq, n, msg, mseq, how := d.grabSeq, d.grabLen, d.msg, d.msgSeq, d.grabHow
	d.mu.Unlock()
	if mseq != a.smp.msgSeq {
		a.smp.msgSeq = mseq
		a.smp.grabbing = false
		switch {
		case msg == "GRABEMPTY":
			a.say("NOTHING ON THE TAPE  ·  PLAY SOMETHING, THEN GRAB")
		case strings.HasPrefix(msg, "LOADERR "):
			a.say("CAN'T LOAD THAT FILE (" + strings.ToUpper(strings.Fields(msg)[1]) + ")")
		case strings.HasPrefix(msg, "SAVED "):
			a.say("SAVED TO THE CARD: " + strings.ToUpper(filepath.Base(msg[6:])))
			a.smp.scanned = time.Time{}
		case strings.HasPrefix(msg, "SAVEERR "):
			a.say("COULDN'T SAVE TO THE CARD")
		}
		a.dirty = true
	}
	if n <= 0 || (!force && seq == a.smp.grabSeq) {
		return
	}
	if seq != a.smp.grabSeq {
		a.smp.edits = [16]sliceEdit{}
		a.smp.grabSeq = seq
		if how == 3 {
			a.smp.chop = chop16 // kits: one sound per pad
		} else {
			a.smpToMan(n) // 0.9.24: you pick your own chops first (MAN), the automatic ones are a turn away
		}
		a.dsp.send("CHOPTRK %d", a.smp.chopTrk)
		a.smpCfg()
		if a.smp.grabbing {
			a.smp.grabbing = false
			a.smp.ask = -1
			a.smpEnter(smpShaper)
			switch how {
			case 2:
				a.say("LOADED " + strings.ToUpper(a.smp.srcName) + "  ·  PADS PLAY THE SLICES")
				a.log("LOADED %s", a.smp.srcName)
			case 3:
				a.say("KIT " + strings.ToUpper(a.smp.srcName) + "  ·  ONE SOUND PER PAD")
			default:
				a.say(fmt.Sprintf("GRABBED %d BARS  ·  PADS PLAY THE SLICES", smpLengths[a.smp.lenIdx]))
			}
		}
	}
	for i := 0; i < 16; i++ {
		s, e, p, r := a.smpSlice(i, n)
		a.dsp.send("SLC %d %d %d %d %d", i, s, e, p, b2i(r))
	}
	a.smp.gzFor = -1
	a.dirty = true
}

// smpBase: slice i before edits, in frames ([0,0] = no slice on this pad).
func (a *app) smpBase(i, n int) (int, int) {
	d := &a.dsp.smp
	d.mu.Lock()
	how, gt, kit := d.grabHow, d.gt, d.kit
	d.mu.Unlock()
	if how == 3 {
		if i < len(kit) {
			return kit[i][0], kit[i][1]
		}
		return 0, 0
	}
	switch a.smp.chop {
	case chop8:
		k := i % 8
		return n * k / 8, n * (k + 1) / 8
	case chopBeats:
		if i >= len(gt) {
			return 0, 0
		}
		e := n
		if i+1 < len(gt) {
			e = gt[i+1]
		}
		return gt[i], e
	case chopMan:
		e := n
		if i+1 < 16 && a.smp.marks[i+1] > a.smp.marks[i] {
			e = a.smp.marks[i+1]
		}
		return a.smp.marks[i], e
	}
	return n * i / 16, n * (i + 1) / 16
}

// smpSlice: slice i with its edits. 8: pads 9-16 = the same 8 reversed.
func (a *app) smpSlice(i, n int) (start, end, pitch int, rev bool) {
	start, end = a.smpBase(i, n)
	if end <= start {
		return 0, 0, 0, false
	}
	e := a.smp.edits[i]
	start += e.startFr
	end += e.endFr
	start = max(0, min(start, n-128))
	end = max(start+128, min(end, n))
	rev = e.rev
	if a.smp.chop == chop8 && i >= 8 && !a.smpIsKit() {
		rev = !rev
	}
	return start, end, e.pitch, rev
}

// smpToMan: MAN starts from the slices you have now, on the same pads. With
// fewer than 16 (BEATS found few hits), the rest share out what follows.
func (a *app) smpToMan(n int) {
	starts := []int{}
	for i := 0; i < 16; i++ {
		if s, e := a.smpBase(i, n); e > s {
			starts = append(starts, s)
		}
	}
	if len(starts) == 0 || starts[0] != 0 {
		starts = append([]int{0}, starts...)
	}
	sort.Ints(starts)
	// the slices you have keep their pads; the extra markers share out the
	// stretch after the last one
	if k := len(starts); k < 16 {
		last := starts[k-1]
		for j := 1; j <= 16-k; j++ {
			starts = append(starts, last+(n-last)*j/(16-k+1))
		}
	}
	copy(a.smp.marks[:], starts[:16])
	a.smp.chop = chopMan
}

// ---------------------------------------------------------------- the card

func wavSeconds(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	h := make([]byte, 12)
	if _, err := f.Read(h); err != nil || string(h[:4]) != "RIFF" || string(h[8:]) != "WAVE" {
		return 0
	}
	var rate, bytesPerSec uint32
	for {
		ch := make([]byte, 8)
		if _, err := f.Read(ch); err != nil {
			return 0
		}
		sz := binary.LittleEndian.Uint32(ch[4:])
		switch string(ch[:4]) {
		case "fmt ":
			b := make([]byte, sz)
			f.Read(b)
			if len(b) >= 12 {
				rate = binary.LittleEndian.Uint32(b[4:])
				bytesPerSec = binary.LittleEndian.Uint32(b[8:])
			}
		case "data":
			if bytesPerSec == 0 || rate == 0 {
				return 0
			}
			return float64(sz) / float64(bytesPerSec)
		default:
			f.Seek(int64(sz+sz&1), 1)
		}
	}
}

// smpScan lists the card's samples folder: WAVs (loops) and folders of WAVs (kits).
func (a *app) smpScan() {
	a.smp.files = nil
	ents, _ := os.ReadDir(a.samplesDir())
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		p := filepath.Join(a.samplesDir(), n)
		if e.IsDir() {
			sub, _ := os.ReadDir(p)
			c := 0
			for _, s := range sub {
				if !s.IsDir() && strings.EqualFold(filepath.Ext(s.Name()), ".wav") && !strings.HasPrefix(s.Name(), ".") {
					c++
				}
			}
			if c > 0 {
				a.smp.files = append(a.smp.files, cardEntry{name: n, kit: true, info: fmt.Sprintf("KIT · %d", min(c, 16)), path: p})
			}
		} else if strings.EqualFold(filepath.Ext(n), ".wav") {
			sec := wavSeconds(p)
			a.smp.files = append(a.smp.files, cardEntry{name: strings.TrimSuffix(n, filepath.Ext(n)), secs: sec, info: fmt.Sprintf("%.1f S", sec), path: p})
		}
	}
	sort.Slice(a.smp.files, func(i, j int) bool {
		x, y := a.smp.files[i], a.smp.files[j]
		if x.kit != y.kit {
			return y.kit // loops first, then kits
		}
		return strings.ToLower(x.name) < strings.ToLower(y.name)
	})
	// MIXTAPE takes first, newest on top
	takes := []cardEntry{}
	tents, _ := os.ReadDir(a.mixtapeDir())
	for _, e := range tents {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") || !strings.EqualFold(filepath.Ext(n), ".wav") {
			continue
		}
		if a.mt.rec && filepath.Join(a.mixtapeDir(), n) == a.mt.path {
			continue // still recording
		}
		p := filepath.Join(a.mixtapeDir(), n)
		sec := wavSeconds(p)
		takes = append(takes, cardEntry{name: strings.TrimSuffix(n, filepath.Ext(n)), take: true, secs: sec, info: clock(sec), path: p})
	}
	sort.Slice(takes, func(i, j int) bool { return strings.ToLower(takes[i].name) > strings.ToLower(takes[j].name) })
	a.smp.files = append(takes, a.smp.files...)
	a.smp.fileIdx = max(0, min(a.smp.fileIdx, len(a.smp.files)-1))
	if a.mt.saved != "" { // a take just saved: it's the one to chop
		for i, e := range a.smp.files {
			if e.path == a.mt.saved {
				a.smp.fileIdx = i
			}
		}
		a.mt.saved = ""
	}
	a.smp.scanned = time.Now()
}

func (a *app) smpLoadSelected() {
	if len(a.smp.files) == 0 {
		a.say("NOTHING ON THE CARD YET  ·  SAMPLE RECORD MAKES A TAKE")
		return
	}
	a.mtListenStop()
	e := a.smp.files[a.smp.fileIdx]
	a.smp.srcName = e.name
	a.smp.grabbing = true
	if e.take {
		from := a.takeFrom(e)
		a.smp.srcName = fmt.Sprintf("%s FROM %s", e.name, clock(from))
		a.dsp.send("LOADAT %.3f %s", from, e.path)
		a.log("LOAD_PRESSED %s from=%.1f", e.name, from)
		a.say("LOADING " + strings.ToUpper(a.smp.srcName))
		return
	}
	if e.kit {
		a.dsp.send("LOADKIT %s", e.path)
	} else {
		a.dsp.send("LOAD %s", e.path)
	}
	a.log("LOAD_PRESSED %s kit=%t", e.name, e.kit)
	a.say("LOADING " + strings.ToUpper(e.name))
}

func (a *app) smpSave() {
	if !a.smpGrabbed() {
		a.say("NOTHING TO SAVE YET")
		return
	}
	dir := a.samplesDir()
	os.MkdirAll(dir, 0755)
	for i := 1; i < 1000; i++ {
		p := filepath.Join(dir, fmt.Sprintf("chop %03d.wav", i))
		if _, err := os.Stat(p); err != nil {
			a.dsp.send("SAVE %s", p)
			a.say("SAVING...")
			a.log("SAVE_PRESSED %s", p)
			return
		}
	}
}

// ---------------------------------------------------------------- controls




// onPressure: poly pressure from a pad (any view; used by the sampler).
func (a *app) onPressure(idx int, val byte) {
	if a.smp.view != smpChop && a.smp.view != smpSlice {
		return
	}
	p := float64(val) / 127
	a.smp.press[idx] = p
	a.dsp.send("SLP %d %.3f", idx, p*a.smp.drift*2.5)
	a.animate(time.Now().Add(200 * time.Millisecond))
}

func (a *app) smpStep(k, delta int) int {
	a.smp.acc[k] += delta
	if a.smp.acc[k] >= 8 {
		a.smp.acc[k] = 0
		return 1
	}
	if a.smp.acc[k] <= -8 {
		a.smp.acc[k] = 0
		return -1
	}
	return 0
}


func (a *app) smpFader(v, prev int) {
	x := float64(v) / 127
	if a.smp.nrHeld {
		a.smp.spin = x
		a.dirty = true
		return
	}
	if a.smp.view == smpChop || a.smp.view == smpSlice {
		a.smp.scrubX, a.smp.scrubAt = x, time.Now()
		a.dsp.send("SCRUBPOS %.4f", x)
		a.animate(time.Now().Add(300 * time.Millisecond))
		a.dirty = true
	}
}

// ---------------------------------------------------------------- pads

func smpCol(i int) uint32 {
	return []uint32{pBlue, pCream, pLilac, pYellow}[i%4]
}



// ---------------------------------------------------------------- drawing

func lvl(b uint8) float64 { return float64(b) / 255 }

// levelWave draws engine levels as the two-colour wave: the lows mirrored
// in magenta, the rest mirrored in lime on top, narrower.
func levelWave(c *gfx.Canvas, x0, x1, ym, h float64, pts [][2]uint8, lo, hi uint32) {
	if len(pts) == 0 {
		return
	}
	for x := x0; x < x1; x++ {
		f := (x - x0) / (x1 - x0) * float64(len(pts))
		i := min(len(pts)-1, int(f))
		j := min(len(pts)-1, i+1)
		t := f - float64(i)
		l := lvl(pts[i][0])*(1-t) + lvl(pts[j][0])*t
		hh := lvl(pts[i][1])*(1-t) + lvl(pts[j][1])*t
		c.Line(x+0.5, ym-l*h, x+0.5, ym+l*h, 1.2, lo, 1)
		c.Line(x+0.5, ym-hh*h*0.62, x+0.5, ym+hh*h*0.62, 1.2, hi, 1)
	}
}


// drawCard: the LIBRARY. MIXTAPE takes (newest first), then the samples and
// kits in FMSTEP/samples. A take shows its whole length with the 16 seconds
// that PAD FX will load; K1 moves them, the middle button listens.
func (a *app) drawCard(c *gfx.Canvas) {
	if time.Since(a.smp.scanned) > 5*time.Second {
		a.smpScan()
	}
	n := len(a.smp.files)
	bigNum(c, 10, 25, strconv.Itoa(min(n, a.smp.fileIdx+1)), fmt.Sprintf("/%d", n), "LIBRARY", 2.9)
	sel, ok := a.smpSelected()
	seg := 0
	if ok && !sel.take {
		seg = 1
	}
	segTabs(c, 310, 33, []string{"MIXTAPE", "SAMPLES"}, seg, uLime)
	if n == 0 {
		rule(c, 10, 310, 52, "THE CARD", uDim)
		c.Text(160, 92, "NOTHING HERE YET", gfx.Body, uDim, gfx.Center|gfx.Mid)
		c.Text(160, 112, "SAMPLE RECORD: RECORD YOUR JAM (A TAKE)", gfx.Small, uFaint, gfx.Center|gfx.Mid)
		c.Text(160, 126, "OR COPY WAV FILES INTO FMSTEP/SAMPLES", gfx.Small, uFaint, gfx.Center|gfx.Mid)
		c.Text(160, 140, "A FOLDER OF WAVS = A KIT (ONE SOUND PER PAD)", gfx.Small, uFaint, gfx.Center|gfx.Mid)
		mockKnobs(c, [3]string{"START", "", ""}, [3]string{"-", "", ""}, [3]float64{0, 0, 0}, -1)
		a.uiBottom(c, "SAMPLE RECORD: START A TAKE")
		return
	}
	rows := 9
	if ok && sel.take {
		rows = 4
	}
	const y0, rh = 58.0, 15.0
	first := max(0, min(a.smp.fileIdx-rows/2, n-rows))
	for r := 0; r < rows && first+r < n; r++ {
		i := first + r
		e := a.smp.files[i]
		y := y0 + float64(r)*rh
		if i == a.smp.fileIdx {
			c.RoundRect(8, y-7, 304, 14, 7, uPanel2, 1)
			diamond(c, 16, y, 3.5, uLime)
		}
		// a tiny icon: a cassette for a take, four dots for a kit, a little wave for a loop
		switch {
		case e.take:
			c.RoundRect(23, y-4, 13, 8, 1.5, pLilac, 1)
			c.Circle(27, y+0.5, 1.4, uBG, 1)
			c.Circle(32, y+0.5, 1.4, uBG, 1)
		case e.kit:
			for k := 0; k < 4; k++ {
				c.Circle(28+float64(k%2)*4, y-2+float64(k/2)*4, 1.2, smpCol(k), 1)
			}
		default:
			for k := 0; k < 8; k++ {
				hh := 1.5 + 3*math.Abs(math.Sin(float64(k)*1.3))
				c.Line(25+float64(k)*1.3, y-hh, 25+float64(k)*1.3, y+hh, 1, knobCols[2], 1)
			}
		}
		name := strings.ToUpper(e.name)
		if len(name) > 30 {
			name = name[:28] + "..."
		}
		col := uint32(uDim)
		if i == a.smp.fileIdx {
			col = uText
		}
		c.Text(42, y, name, gfx.Small, col, gfx.Left|gfx.Mid)
		c.Text(306, y, e.info, gfx.Small, uFaint, gfx.Right|gfx.Mid)
	}
	if !(ok && sel.take) {
		a.uiBottom(c, "DATA: PICK  ·  PUSH: LOAD  ·  SAMPLE SELECT: BACK")
		return
	}
	// the take: its whole length, the 16 seconds to load, where LISTEN is
	from := a.takeFrom(sel)
	const x0, x1, ym = 12.0, 308.0, 142.0
	pk := a.mtPeaks(sel.path)
	w := x1 - x0
	for i := 0; i < int(w); i++ {
		v := 0.15
		if len(pk) > 0 {
			k0 := i * len(pk) / int(w)
			k1 := max(k0+1, (i+1)*len(pk)/int(w))
			m := 0.0
			for k := k0; k < k1 && k < len(pk); k++ {
				m = math.Max(m, lvl(pk[k]))
			}
			v = m
		}
		c.Line(x0+float64(i)+0.5, ym-v*16, x0+float64(i)+0.5, ym+v*16, 1, 0x56632E, 1)
	}
	if sel.secs > 0 {
		f0 := from / sel.secs
		f1 := math.Min(1, (from+16)/sel.secs)
		wx0, wx1 := x0+w*f0, x0+w*f1
		for i := int(wx0 - x0); i < int(wx1-x0) && i < int(w); i++ {
			v := 0.15
			if len(pk) > 0 {
				v = lvl(pk[min(len(pk)-1, i*len(pk)/int(w))])
			}
			c.Line(x0+float64(i)+0.5, ym-v*16, x0+float64(i)+0.5, ym+v*16, 1, uLime, 1)
		}
		c.RoundRectLine(wx0-1, ym-21, math.Max(4, wx1-wx0+2), 42, 2, 1.4, uLime, 1)
		c.Text(wx0, ym-26, clock(from)+" - "+clock(math.Min(sel.secs, from+16)), gfx.Small, uLime, gfx.Left|gfx.Mid)
		if a.mt.playing && a.mt.playPath == sel.path && a.mt.playTotal > 0 {
			px := x0 + w*float64(a.mt.playPos)/float64(a.mt.playTotal)
			c.Line(px, ym-20, px, ym+20, 1.2, uText, 1)
			diamond(c, px, ym-20, 3, uText)
		}
		c.Text(x1, ym-26, clock(sel.secs), gfx.Small, uFaint, gfx.Right|gfx.Mid)
	}
	lis := "LISTEN"
	if a.mt.playing && a.mt.playPath == sel.path {
		lis = "STOP"
	}
	mockKnobs(c, [3]string{"START", "", ""}, [3]string{clock(from), "", ""}, [3]float64{from / math.Max(1, sel.secs), 0, 0}, a.smpActiveKnob())
	a.uiBottom(c, "K1: MOVE THE 16 S  ·  MIDDLE: "+lis+"  ·  PUSH: LOAD")
}

// smpSelected: the LIBRARY entry under the cursor.
func (a *app) smpSelected() (cardEntry, bool) {
	if a.smp.fileIdx < 0 || a.smp.fileIdx >= len(a.smp.files) {
		return cardEntry{}, false
	}
	return a.smp.files[a.smp.fileIdx], true
}

// takeFrom: where the 16 s start in take e (kept per take while it's selected).
func (a *app) takeFrom(e cardEntry) float64 {
	if a.smp.takeFor != e.path {
		a.smp.takeFor, a.smp.takeFrom = e.path, 0
	}
	return a.smp.takeFrom
}

// clock: seconds as M:SS.
func clock(s float64) string {
	t := int(math.Max(0, s))
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

func (a *app) smpActiveKnob() int {
	if time.Since(a.ui.touchAt) < 2*time.Second && strings.HasPrefix(a.ui.touchKey, "smp") {
		return a.smp.acc2
	}
	return -1
}

// sliceLevels: the overview points of slice i, and its loudness 0..1.
func (a *app) sliceLevels(i int) ([][2]uint8, float64) {
	n := a.dsp.smp.grabLen
	if n <= 0 {
		return nil, 0
	}
	s, e, _, _ := a.smpSlice(i, n)
	if e <= s {
		return nil, -1
	}
	d := &a.dsp.smp
	d.mu.Lock()
	defer d.mu.Unlock()
	p0, p1 := s*256/n, max(s*256/n+1, e*256/n)
	pts := make([][2]uint8, 0, p1-p0)
	pk := 0.0
	for p := p0; p < p1 && p < 256; p++ {
		pts = append(pts, d.gw[p])
		pk = math.Max(pk, math.Max(lvl(d.gw[p][0]), lvl(d.gw[p][1])))
	}
	return pts, pk
}

// sliceDur: how long slice i plays (seconds, at its pitch).
func (a *app) sliceDur(i int) float64 {
	n := a.dsp.smp.grabLen
	if n <= 0 {
		return 0
	}
	s, e, p, _ := a.smpSlice(i, n)
	return float64(e-s) / 48000 / math.Pow(2, float64(p)/12)
}

// slicePlaying: how far through slice i is (0..1), or -1 if it isn't playing.
func (a *app) slicePlaying(i int) float64 {
	if a.smp.hitAt[i].IsZero() {
		return -1
	}
	dur := a.sliceDur(i)
	t := time.Since(a.smp.hitAt[i]).Seconds()
	if a.smp.motion == 2 && a.smp.held[i] && dur > 0 { // LOOP: round and round while held
		return math.Mod(t, dur) / dur
	}
	if dur <= 0 || t > dur || (a.smp.motion >= 1 && !a.smp.held[i] && t > 0.03) {
		return -1
	}
	return t / dur
}

func (a *app) drawChop(c *gfx.Canvas, dt float64) {
	c.Text(10, 33, "CHOP", gfx.Big, uText, gfx.Left|gfx.Mid)
	kit := a.smpIsKit()
	if kit {
		segTabs(c, 310, 33, []string{"KIT"}, 0, uText)
	} else {
		tabs := make([]string, len(chopOrder))
		for i, m := range chopOrder {
			tabs[i] = smpChops[m]
		}
		segTabs(c, 310, 33, tabs, indexOfInt(chopOrder, a.smp.chop), uText)
	}
	d := &a.dsp.smp
	d.mu.Lock()
	bars, bpm, how := d.grabBars, d.grabBpm, d.grabHow
	d.mu.Unlock()
	info := fmt.Sprintf("%d BARS · %.0f BPM", bars, bpm)
	if how >= 2 {
		info = strings.ToUpper(a.smp.srcName)
		if len(info) > 16 {
			info = info[:14] + "..."
		}
	}
	c.Text(72, 33, info, gfx.Small, uDim, gfx.Left|gfx.Mid)
	const cx, cy = 77.0, 117.0
	// the reel's turn: scrub (fader) sets it, NOTE REPEAT + fader spins it
	// with the low end, otherwise it follows the slice that is playing
	playing, prog := -1, 0.0
	for i := 0; i < 16; i++ {
		if p := a.slicePlaying(i); p >= 0 && (playing < 0 || a.smp.hitAt[i].After(a.smp.hitAt[playing])) {
			playing, prog = i, p
		}
	}
	switch {
	case a.smp.nrHeld:
		low := 0.0
		for b := 0; b < 10; b++ {
			low = math.Max(low, math.Max(0, (a.ui.spec.show[b]+60)/60))
		}
		a.smp.angle += dt * a.smp.spin * low * 14
	case time.Since(a.smp.scrubAt) < 700*time.Millisecond:
		target := a.smp.scrubX * 2 * math.Pi
		a.smp.angle += (target - a.smp.angle) * math.Min(1, dt*14)
	case playing >= 0:
		target := -(float64(playing) + prog) * 2 * math.Pi / 16
		da := math.Remainder(target-a.smp.angle, 2*math.Pi)
		a.smp.angle += da * math.Min(1, dt*10)
	}
	press := 0.0
	for _, p := range a.smp.press {
		press = math.Max(press, p)
	}
	wob := press * a.smp.drift * 0.08 * math.Sin(float64(time.Now().UnixMilli())/1000*2*math.Pi*5.5)
	ang0 := a.smp.angle + wob
	// the chop track: where recorded hits go
	trk := "SEQ OFF"
	tcl := uint32(uFaint)
	if a.smp.chopTrk >= 0 {
		trk = fmt.Sprintf("SEQ ▶ T%d", a.smp.chopTrk+1)
		tcl = tcol(a.smp.chopTrk)
		if a.sq.recArm {
			trk += "  ·  REC"
		}
	}
	rule(c, 10, 144, 50, "REEL", uDim)
	for i := 0; i < 16; i++ {
		_, pk := a.sliceLevels(i)
		if pk < 0 {
			continue // no slice on this pad
		}
		ang := -math.Pi/2 + (float64(i)+0.5)*2*math.Pi/16 + ang0
		r := 7 + 12*pk
		x, y := cx+36*math.Cos(ang), cy+36*math.Sin(ang)
		col := smpCol(i)
		if p := a.slicePlaying(i); p >= 0 {
			g := 1 - p*0.5
			c.Glow(x, y, r+8, col, 0.5*g)
			c.Circle(x, y, r, col, 0.6+0.3*g)
			c.Text(x, y, strconv.Itoa(i+1), gfx.Label, uBG, gfx.Center|gfx.Mid)
		} else {
			al := 0.85
			if i == a.smp.sel {
				al = 1
			}
			c.Ring(x, y, r, 1, col, al)
		}
	}
	c.Ring(cx, cy, 14, 1, uFaint, 1)
	shown := a.smp.sel
	if playing >= 0 {
		shown = playing
	}
	c.Dots(cx-8.5, cy-5, fmt.Sprintf("%02d", shown+1), dotGlyph, 1.5, uLime, 0)
	c.Text(cx, 178, trk, gfx.Small, tcl, gfx.Center|gfx.Mid)
	rule(c, 162, 310, 50, "PADS", uDim)
	const px0, py0, pw = 163.0, 58.0, 34.0
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		x, y := px0+float64(col)*(pw+3), py0+float64(row)*(pw-3)
		hgt := pw - 6
		_, pk := a.sliceLevels(i)
		if pk < 0 {
			c.RoundRectLine(x, y, pw, hgt, 4, 1, uPanel2, 1)
			continue
		}
		on := a.slicePlaying(i) >= 0 || a.smp.held[i]
		if on {
			c.Glow(x+pw/2, y+hgt/2, 26, knobCols[0], 0.55)
			c.RoundRect(x, y, pw, hgt, 4, 0xBFE9F0, 1)
		} else {
			c.RoundRect(x, y, pw, hgt, 4, uPanel2, 1)
		}
		if i == a.smp.sel {
			c.RoundRectLine(x, y, pw, hgt, 4, 1, uText, 0.8)
		}
		wc := uint32(0xE8E4D8)
		if on {
			wc = 0x1D4F5E
		}
		a.fineWave(c, i, x+3, x+pw-3, y+hgt/2, hgt*0.42, wc)
		if a.smp.press[i] > 0.05 {
			c.Rect(x+3, y+hgt-3, (pw-6)*a.smp.press[i], 2, uOrange, 1)
		}
	}
	chopVal := "EVEN " + smpChops[a.smp.chop]
	switch {
	case kit:
		chopVal = "KIT"
	case a.smp.chop == chopBeats:
		chopVal = "BEATS"
	case a.smp.chop == chopMan:
		chopVal = "MANUAL"
	}
	mockKnobs(c, [3]string{"DRIFT", "CHOP", "MOTION"},
		[3]string{fmt.Sprintf("DILLA %d%%", int(a.smp.drift*100+0.5)), chopVal, smpMotions[a.smp.motion]},
		[3]float64{a.smp.drift, float64(indexOfInt(chopOrder, a.smp.chop)+1) / float64(len(chopOrder)), float64(a.smp.motion+1) / 3}, a.smpActiveKnob())
	hint := "PADS: SLICES  ·  MIDDLE: TRIM + ZOOM THE SLICE  ·  DATA: CHOP TRACK"
	if a.smp.nrHeld {
		hint = fmt.Sprintf("LOW END SPINS THE REEL  ·  FADER: HOW HARD (%d%%)", int(a.smp.spin*100+0.5))
	}
	a.uiBottom(c, hint)
}

// rand01: a small xorshift for DRIFT (UI thread only).
var rndState uint32 = 0x9E3779B9

func rand01() float64 {
	rndState ^= rndState << 13
	rndState ^= rndState >> 17
	rndState ^= rndState << 5
	return float64(rndState) / 4294967296.0
}

func indexOfInt(l []int, v int) int {
	for i, x := range l {
		if x == v {
			return i
		}
	}
	return 0
}

// fineWave (0.9.24): a light, thin preview of slice i from the fine peaks,
// scaled to the slice's own loudest point so its hits stand out.
func (a *app) fineWave(c *gfx.Canvas, i int, x0, x1, ym, h float64, col uint32) {
	n := a.dsp.smp.grabLen
	if n <= 0 {
		return
	}
	s, e, _, _ := a.smpSlice(i, n)
	if e <= s {
		return
	}
	d := &a.dsp.smp
	d.mu.Lock()
	p0, p1 := s*2048/n, max(s*2048/n+1, e*2048/n)
	vals := make([]float64, 0, p1-p0)
	pk := 0.0
	for p := p0; p < p1 && p < 2048; p++ {
		v := lvl(d.gwf[p])
		vals = append(vals, v)
		pk = math.Max(pk, v)
	}
	d.mu.Unlock()
	if len(vals) == 0 || pk <= 0 {
		return
	}
	w := x1 - x0
	cols := int(w)
	c.Line(x0, ym, x1, ym, 0.6, col, 0.35)
	for k := 0; k < cols; k++ {
		a0 := k * len(vals) / cols
		b0 := max(a0+1, (k+1)*len(vals)/cols)
		v := 0.0
		for j := a0; j < b0 && j < len(vals); j++ {
			v = math.Max(v, vals[j])
		}
		v /= pk
		x := x0 + float64(k) + 0.5
		c.Line(x, ym-v*h, x, ym+v*h, 0.8, col, 0.9)
	}
}
