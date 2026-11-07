package main

// The sampler screens (0.9.27), following the sampler infographic:
//
//   1 FILL A PAD   SAMPLE SELECT (the card: folders, the wave, K1 START + K3
//                  ZOOM pick the part; push = onto the pad; SHIFT + push =
//                  CHOP it over the bank's 16 pads) · SAMPLE IN (record the
//                  mix onto an empty pad) · a MIXTAPE take (same browser)
//   2 THE PAD      its own sound, trim, envelope, mode, tune, level, pan
//   3 SHAPE IT     CHOP: SHAPER. Pages ENV → TRIM → TUNE (CHOP moves on; after
//                  TUNE it leaves). ENV: right button = TRIM.
//   4 PLAY + KEEP  pads play the bank; SEQ REC records onto the sampler track
//                  (T8, note = slot); the kit is kept on the card
//
//   PAD BANK       the 8-bank overview; again: next bank
//   ERASE + pad    empty that pad (twice)

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/gfx"
)

const (
	pageEnv = iota
	pageTrim
	pageTune
)

var pageNames = []string{"ENV", "TRIM", "TUNE"}

// sampler UI state (in smpState via these fields)
type padUI struct {
	page     int
	trimOut  bool
	trimSet  bool
	inLock   bool
	outLock  bool
	fine     bool
	zoom     float64 // TRIM: part of the pad shown (1 = all)
	height   float64
	snap     bool
	dataAt   time.Time
	clearArm time.Time
	clearPad int
	// SAMPLE SELECT
	bdir    string
	bents   []browseEnt
	bidx    int
	bscan   time.Time
	bstart  float64 // seconds into the file
	bwin    float64 // seconds taken
	bfor    string
	acc     [3]float64
	recPath string
}

type browseEnt struct {
	name string
	dir  bool
	kit  bool // a .json kit
	path string
}

func (a *app) sampOn() bool { return a.smp.view != smpOff }

// ---------------------------------------------------------------- opening

func (a *app) samplerOpen(view int) {
	a.smpInit()
	a.padsInit()
	a.mt.view = false
	if a.mode == modeRack || a.mode == modeConfirmExit || a.mode == modeFX {
		a.setMode(modePlay)
	}
	if view == smpShaper {
		a.smp.ask = -1
		if a.pslot(a.padSel()).empty() {
			a.smp.ask = a.smp.sel
		}
	}
	a.smpEnter(view)
}

func (a *app) shaperOpen() { a.samplerOpen(smpShaper) }

// smpButton: buttons for the sampler (PAD FX handling moved to fxButtons).
func (a *app) smpButton(note byte, down bool) bool {
	if a.mode == modeConfirmExit {
		return false
	}
	switch note {
	case controls.BtnSampleSelect:
		if a.shift {
			return false // SHIFT + SAMPLE SELECT: sound slots
		}
		if down {
			if a.smp.view == smpTape {
				a.samplerOpen(smpShaper)
			} else {
				a.samplerOpen(smpTape)
				a.say("SAMPLE SELECT  ·  " + slotName(a.padSel()) + " GETS IT  ·  PADS: PICK THE PAD")
			}
		}
		return true
	case controls.BtnChop:
		if a.mode == modePatterns && !a.sampOn() {
			return false // CHOP held in PATTERNS: copy
		}
		if !down {
			return true
		}
		switch {
		case a.shift && a.sampOn():
			a.smpLeave()
		case a.smp.view == smpShaper && a.smp.ask < 0:
			a.pui.page++
			if a.pui.page > pageTune {
				a.pui.page = pageEnv
				a.smpLeave()
				return true
			}
			a.pui.trimSet = false
			a.say(pageNames[a.pui.page] + "  ·  CHOP: NEXT PAGE")
		default:
			a.pui.page = pageEnv
			a.samplerOpen(smpShaper)
		}
		a.dirty = true
		return true
	}
	if !a.sampOn() {
		return false
	}
	switch note {
	case controls.BtnShift:
		return false
	case controls.BtnPlay, controls.BtnStop, controls.BtnTapTempo, controls.BtnPlus, controls.BtnMinus, controls.BtnSeqRecord, controls.BtnSampleRecord:
		return false // transport and recording keep working
	case controls.BtnErase:
		a.sq.eraseHeld = down
		return true
	case controls.BtnPadBank:
		if down {
			if a.smp.view == smpBanks {
				a.pads.bank = (a.pads.bank + 1) % 8
			} else {
				a.smp.view = smpBanks
				a.smpEnter(smpBanks)
			}
			a.say("BANK " + bankName(a.pads.bank))
			a.paintPads()
		}
		return true
	case controls.BtnDataPush:
		if down {
			a.smpPush()
		}
		return true
	case controls.BtnScreenLeft, controls.BtnScreenMiddle, controls.BtnScreenRight:
		b := map[byte]int{controls.BtnScreenLeft: 0, controls.BtnScreenMiddle: 1, controls.BtnScreenRight: 2}[note]
		if down {
			a.smpScreen(b)
		} else if b == 1 && a.smp.btnHeld {
			a.smp.btnHeld = false
			a.smpPad(a.smp.sel, 0, false)
		}
		a.dirty = true
		return true
	case controls.BtnNoteRepeat:
		return true
	}
	if down { // any other button: leave the sampler, then it does its own thing
		a.smpLeave()
	}
	return false
}

// smpScreen: the three buttons above the screen
func (a *app) smpScreen(b int) {
	i := a.padSel()
	switch a.smp.view {
	case smpTape:
		switch b {
		case 0:
			a.browseUp()
		case 1:
			a.browseListen()
		case 2:
			a.browseLoad(false)
		}
	case smpRecIn:
		switch b {
		case 0:
			if !a.pads.recOn {
				a.samplerOpen(smpShaper)
			}
		case 1:
			a.recInToggle()
		}
	case smpBanks:
		if b == 1 {
			a.samplerOpen(smpShaper)
		}
	case smpShaper:
		if a.smp.ask >= 0 {
			switch b {
			case 0:
				a.smp.ask = -1
				a.smpEnter(smpRecIn)
				a.say("SAMPLE IN  ·  MIDDLE: RECORD WHAT YOU HEAR ONTO " + slotName(i))
			case 2:
				a.smp.ask = -1
				a.samplerOpen(smpTape)
			}
			return
		}
		s := a.pslot(i)
		switch a.pui.page {
		case pageEnv:
			switch b {
			case 0:
				s.Env.Mode = (s.Env.Mode + 1) % 3
				a.padSend(i)
				a.padsChanged()
				a.say(fmt.Sprintf("%s: %s", slotName(i), smpMotions[s.Env.Mode]))
			case 1:
				a.smpPad(a.smp.sel, 110, true)
				a.smp.btnHeld = true
			case 2:
				a.pui.page, a.pui.trimSet = pageTrim, false
				a.say("TRIM  ·  LEFT / RIGHT: IN / OUT  ·  AGAIN: LOCK")
			}
		case pageTrim:
			switch b {
			case 1:
				a.smpPad(a.smp.sel, 110, true)
				a.smp.btnHeld = true
			default:
				out := b == 2
				if a.pui.trimSet && a.pui.trimOut == out {
					lk := &a.pui.inLock
					if out {
						lk = &a.pui.outLock
					}
					*lk = !*lk
					a.say(map[bool]string{false: "IN", true: "OUT"}[out] + map[bool]string{true: " LOCKED", false: " UNLOCKED"}[*lk])
				} else {
					a.pui.trimOut, a.pui.trimSet = out, true
					a.say(map[bool]string{false: "IN", true: "OUT"}[out] + "  ·  DATA MOVES IT  ·  PUSH: FINE")
				}
			}
		case pageTune:
			switch b {
			case 0:
				s.Rev = !s.Rev
				a.padSend(i)
				a.padsChanged()
				a.say(map[bool]string{true: "REVERSE", false: "FORWARD"}[s.Rev])
			case 1:
				a.smpPad(a.smp.sel, 110, true)
				a.smp.btnHeld = true
			}
		}
	}
}

func (a *app) smpPush() {
	switch a.smp.view {
	case smpTape:
		a.browseLoad(a.shift)
	case smpBanks:
		a.samplerOpen(smpShaper)
	case smpShaper:
		if a.pui.page == pageTrim {
			a.pui.fine = !a.pui.fine
			a.say(map[bool]string{true: "FINE", false: "BIG MOVES"}[a.pui.fine])
		}
	case smpRecIn:
		a.recInToggle()
	}
}

// smpData: DATA turns in the sampler (d2: 1 = +1, 127 = -1)
func (a *app) smpData(d2 byte) {
	d := 1
	if d2 > 64 {
		d = -1
	}
	switch a.smp.view {
	case smpTape:
		if n := len(a.pui.bents); n > 0 {
			a.pui.bidx = max(0, min(n-1, a.pui.bidx+d*a.padAccel()))
		}
	case smpBanks:
		a.pads.bank = (a.pads.bank + d + 8) % 8
		a.paintPads()
	case smpShaper:
		if a.pui.page == pageTrim && a.smp.ask < 0 {
			a.trimMove(d * a.padAccel())
			break
		}
		a.smp.sel = (a.smp.sel + d + 16) % 16
		a.smp.ask = -1
		if a.pslot(a.padSel()).empty() {
			a.smp.ask = a.smp.sel
		}
		a.paintPads()
	}
	a.dirty = true
}

func (a *app) padAccel() int {
	dt := time.Since(a.pui.dataAt)
	a.pui.dataAt = time.Now()
	switch {
	case dt < 25*time.Millisecond:
		return 8
	case dt < 45*time.Millisecond:
		return 4
	case dt < 80*time.Millisecond:
		return 2
	}
	return 1
}

// ---------------------------------------------------------------- pads

func (a *app) smpPad(idx int, vel byte, on bool) {
	if a.smp.view == smpTape { // SAMPLE SELECT: a pad picks where the sound goes
		if on {
			a.smp.sel = idx
			a.say("IT GOES ONTO " + slotName(a.padSel()))
			a.paintPads()
		}
		return
	}
	i := a.slotIdx(idx)
	rec := a.sq.recArm && a.sq.playing
	t := 8 + a.pads.bank // 0.9.28: the bank's sample track; note 36 + pad
	note := 36 + idx
	if !on {
		if a.smp.held[idx] {
			if rec {
				a.dsp.lines([]string{fmt.Sprintf("U %d", t), fmt.Sprintf("r %d", note), fmt.Sprintf("U %d", a.proj.Track)})
			} else {
				a.dsp.send("PO %d", i)
			}
		}
		a.smp.held[idx] = false
		a.smpPaintPad(idx)
		a.dirty = true
		return
	}
	if on && a.sq.eraseHeld && a.smp.view != smpRecIn { // ERASE + pad: empty it (twice)
		if a.pui.clearPad == i && time.Since(a.pui.clearArm) < 3*time.Second {
			a.padClear(i)
			a.pui.clearArm = time.Time{}
			a.say(slotName(i) + " EMPTIED")
		} else if !a.pslot(i).empty() {
			a.pui.clearPad, a.pui.clearArm = i, time.Now()
			a.say("ERASE + " + slotName(i) + " AGAIN: EMPTY IT")
		}
		a.paintPads()
		return
	}
	a.smp.sel = idx
	s := a.pslot(i)
	if s.empty() {
		if a.smp.view == smpShaper || a.smp.view == smpBanks {
			a.smp.ask = idx
			if a.smp.view == smpBanks {
				a.smpEnter(smpShaper)
			}
		}
		a.paintPads()
		a.dirty = true
		return
	}
	if a.smp.view == smpShaper {
		a.smp.ask = -1
	}
	v := float64(vel) / 127
	if rec {
		a.dsp.lines([]string{fmt.Sprintf("U %d", t), fmt.Sprintf("R %d %.3f", note, v), fmt.Sprintf("U %d", a.proj.Track)})
	} else {
		a.dsp.send("PN %d %.3f", i, v)
	}
	a.smp.hitAt[idx], a.smp.held[idx] = time.Now(), true
	a.proj.Tracks[t].Note = note // new steps on the sample track use the last pad hit
	a.sq.flash[t] = time.Now()
	a.addHit(t, i, v, max(0, a.sq.posStep), 16)
	a.paintPads()
	a.animate(time.Now().Add(400 * time.Millisecond))
	a.dirty = true
}

func (a *app) smpPaintPad(i int) {
	s := a.pslot(a.slotIdx(i))
	c := tcol(a.pads.bank % 8)
	r, g, b := byte(c>>16)>>1, byte(c>>8)>>1, byte(c)>>1
	switch {
	case a.smp.held[i]:
		a.padColour(i, 127, 127, 127)
	case s.empty():
		if i == a.smp.sel {
			a.padColour(i, 14, 14, 14)
		} else {
			a.padColour(i, 2, 2, 2)
		}
	case i == a.smp.sel:
		a.padColour(i, min(127, r+40), min(127, g+40), min(127, b+40))
	default:
		a.padColour(i, r/3, g/3, b/3)
	}
}

func (a *app) smpPaintPads() {
	for i := 0; i < 16; i++ {
		a.smpPaintPad(i)
	}
}

// ---------------------------------------------------------------- knobs

func (a *app) smpKnob(k, delta int) {
	a.ui.touchKey, a.ui.touchAt = fmt.Sprintf("smp%d", k), time.Now()
	a.smp.acc2 = k
	i := a.padSel()
	s := a.pslot(i)
	switch a.smp.view {
	case smpTape:
		a.browseKnob(k, delta)
		return
	case smpShaper:
		if a.smp.ask >= 0 {
			a.say("THIS PAD IS EMPTY  ·  LEFT: SAMPLE IN  ·  RIGHT: SAMPLE SELECT")
			return
		}
	default:
		return
	}
	switch a.pui.page {
	case pageEnv:
		e := &s.Env
		pick := func(name string, v *float64) { // 0.9.28: pick-up
			if n, ok := a.knobPick(k, delta, fmt.Sprintf("pad%d:%s", i, name), *v); ok {
				*v = n
			}
		}
		switch {
		case k == 0:
			pick("A", &e.A)
		case k == 1 && a.shift:
			pick("S", &e.S)
		case k == 1:
			pick("D", &e.D)
		case k == 2:
			pick("R", &e.R)
		}
		a.smp.envKnob, a.smp.envSus = k, k == 1 && a.shift
	case pageTrim:
		switch k {
		case 0:
			a.pui.height = math.Max(1, math.Min(8, a.trimHeight()*math.Pow(1.04, float64(delta))))
		case 1:
			a.pui.acc[1] += float64(delta)
			if math.Abs(a.pui.acc[1]) >= 8 {
				a.pui.acc[1] = 0
				a.pui.snap = !a.pui.snap
				a.say(map[bool]string{true: "SNAP: ZERO CROSSINGS (NO CLICKS)", false: "SNAP OFF"}[a.pui.snap])
			}
		case 2:
			a.pui.zoom = math.Max(0.004, math.Min(1, a.trimZoom()*math.Pow(1.05, -float64(delta))))
		}
	case pageTune:
		key := func(name string) string { return fmt.Sprintf("pad%d:%s", i, name) }
		switch { // 0.9.28: pick-up, then the knob's whole range
		case k == 0 && a.shift:
			if n, ok := a.knobPick(k, delta, key("pan"), s.Pan); ok {
				s.Pan = n
			}
		case k == 0:
			if n, ok := a.knobPick(k, delta, key("pitch"), (s.Pitch+24)/48); ok {
				s.Pitch = math.Round(n*48 - 24)
			}
		case k == 1:
			if n, ok := a.knobPick(k, delta, key("fine"), (s.Fine+100)/200); ok {
				s.Fine = math.Round(n*200 - 100)
			}
		case k == 2:
			if n, ok := a.knobPick(k, delta, key("level"), s.Level/1.5); ok {
				s.Level = n * 1.5
			}
		}
	}
	a.padSend(i)
	a.padsChanged()
	a.dirty = true
}

func (a *app) trimZoom() float64 {
	if a.pui.zoom <= 0 {
		return 1
	}
	return a.pui.zoom
}

func (a *app) trimHeight() float64 { return math.Max(1, a.pui.height) }

// trimMove: DATA moves IN or OUT (big moves = a 60th of the view; fine = a 1200th)
func (a *app) trimMove(clicks int) {
	i := a.padSel()
	s := a.pslot(i)
	if !a.pui.trimSet {
		a.say("LEFT: IN  ·  RIGHT: OUT  ·  THEN DATA MOVES IT")
		return
	}
	if (a.pui.trimOut && a.pui.outLock) || (!a.pui.trimOut && a.pui.inLock) {
		a.say(map[bool]string{false: "IN", true: "OUT"}[a.pui.trimOut] + " IS LOCKED  ·  PRESS ITS BUTTON AGAIN")
		return
	}
	step := a.trimZoom() / 60
	if a.pui.fine {
		step = a.trimZoom() / 1200
	}
	mv := float64(clicks) * step
	taken := a.padTaken(i)
	if a.pui.trimOut {
		s.Out = math.Max(s.In+0.002, math.Min(1, s.Out+mv))
		if a.pui.snap && taken > 0 {
			s.Out = math.Max(s.In+0.002, math.Min(1, (wavZero(s.Path, s.Start+s.Out*taken)-s.Start)/taken))
		}
	} else {
		s.In = math.Max(0, math.Min(s.Out-0.002, s.In+mv))
		if a.pui.snap && taken > 0 {
			s.In = math.Max(0, math.Min(s.Out-0.002, (wavZero(s.Path, s.Start+s.In*taken)-s.Start)/taken))
		}
	}
	a.pui.trimSet = true
	a.padSend(i)
	a.padsChanged()
}

// ---------------------------------------------------------------- SAMPLE IN

func (a *app) recInToggle() {
	i := a.padSel()
	if a.pads.recOn {
		a.dsp.send("PRECSTOP %s", a.pui.recPath)
		a.say("STOPPED  ·  " + slotName(i))
		return
	}
	dir := filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "samples", "recorded")
	if os.Getenv("FMSTEP_SD") == "" {
		dir = filepath.Join(a.user, "recorded")
	}
	os.MkdirAll(dir, 0755)
	n := 1
	for {
		p := filepath.Join(dir, fmt.Sprintf("%s %03d.wav", slotName(i), n))
		if _, err := os.Stat(p); err != nil {
			a.pui.recPath = p
			break
		}
		n++
	}
	a.pads.recSlot, a.pads.recOn, a.pads.recStart = i, true, time.Now()
	a.dsp.pads.mu.Lock()
	a.dsp.pads.recLen = 0
	a.dsp.pads.mu.Unlock()
	a.dsp.send("PREC %d", i)
	a.say("RECORDING ONTO " + slotName(i) + "  ·  MIDDLE: STOP (UP TO 20 S)")
	a.log("PAD_REC_START %s", slotName(i))
}

// ---------------------------------------------------------------- SAMPLE SELECT

func (a *app) cardRoot() string {
	if r := os.Getenv("FMSTEP_SD"); r != "" {
		return r
	}
	return a.user
}

func (a *app) browseScan() {
	if a.pui.bdir == "" {
		a.pui.bdir = filepath.Join(a.cardRoot(), "FMSTEP", "samples")
		if _, err := os.Stat(a.pui.bdir); err != nil {
			a.pui.bdir = a.cardRoot()
		}
	}
	ents, err := os.ReadDir(a.pui.bdir)
	if err != nil {
		a.pui.bdir = a.cardRoot()
		ents, _ = os.ReadDir(a.pui.bdir)
	}
	var dirs, files []browseEnt
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") || n == "System Volume Information" {
			continue
		}
		p := filepath.Join(a.pui.bdir, n)
		switch {
		case e.IsDir():
			dirs = append(dirs, browseEnt{name: n, dir: true, path: p})
		case strings.EqualFold(filepath.Ext(n), ".wav"):
			files = append(files, browseEnt{name: n, path: p})
		case strings.EqualFold(filepath.Ext(n), ".json") && filepath.Base(a.pui.bdir) == "kits":
			files = append(files, browseEnt{name: n, kit: true, path: p})
		}
	}
	less := func(l []browseEnt) {
		sort.Slice(l, func(i, j int) bool { return strings.ToLower(l[i].name) < strings.ToLower(l[j].name) })
	}
	less(dirs)
	less(files)
	all := []browseEnt{}
	if filepath.Clean(a.pui.bdir) != filepath.Clean(a.cardRoot()) {
		all = append(all, browseEnt{name: "..", dir: true, path: filepath.Dir(a.pui.bdir)})
	}
	a.pui.bents = append(append(all, dirs...), files...)
	a.pui.bidx = max(0, min(len(a.pui.bents)-1, a.pui.bidx))
	a.pui.bscan = time.Now()
}

func (a *app) browseSel() (browseEnt, bool) {
	if a.pui.bidx < 0 || a.pui.bidx >= len(a.pui.bents) {
		return browseEnt{}, false
	}
	return a.pui.bents[a.pui.bidx], true
}

func (a *app) browseUp() {
	if filepath.Clean(a.pui.bdir) == filepath.Clean(a.cardRoot()) {
		a.say("THIS IS THE TOP OF THE CARD")
		return
	}
	was := filepath.Base(a.pui.bdir)
	a.pui.bdir = filepath.Dir(a.pui.bdir)
	a.browseScan()
	for k, e := range a.pui.bents {
		if e.name == was {
			a.pui.bidx = k
		}
	}
}

// browseFile: the picked file's length, and the part to take (reset per file)
func (a *app) browseFile(e browseEnt) float64 {
	_, total := wavPeaks(e.path, 0, 0, 160)
	if a.pui.bfor != e.path {
		a.pui.bfor, a.pui.bstart, a.pui.bwin = e.path, 0, math.Min(total, padMaxSecs)
	}
	return total
}

func (a *app) browseKnob(k, delta int) {
	e, ok := a.browseSel()
	if !ok || e.dir || e.kit {
		return
	}
	total := a.browseFile(e)
	switch k {
	case 0: // START: a 50th of the part per tick, more when fast
		step := math.Max(0.01, a.pui.bwin/50)
		if abs(delta) >= 3 {
			step *= 4
		}
		a.pui.bstart = math.Max(0, math.Min(math.Max(0, total-0.05), a.pui.bstart+float64(delta)*step))
	case 2: // ZOOM: how much is taken
		a.pui.bwin = math.Max(0.05, math.Min(math.Min(padMaxSecs, total-a.pui.bstart), a.pui.bwin*math.Pow(1.04, -float64(delta))))
	}
	a.dirty = true
}

func (a *app) browseListen() {
	e, ok := a.browseSel()
	if !ok || e.dir || e.kit {
		return
	}
	if a.mt.playing && a.mt.playPath == e.path {
		a.mtListenStop()
		return
	}
	a.browseFile(e)
	a.mtListen(e.path, a.pui.bstart)
	a.say("LISTENING  ·  MIDDLE: STOP")
}

// browseLoad: push / right button. A folder opens; a WAV goes onto the pad
// (SHIFT: chopped over the bank's 16 pads); a kit loads.
func (a *app) browseLoad(chop bool) {
	e, ok := a.browseSel()
	if !ok {
		return
	}
	switch {
	case e.dir:
		a.pui.bdir, a.pui.bidx = e.path, 0
		if e.name == ".." {
			a.browseUp()
			return
		}
		a.browseScan()
		return
	case e.kit:
		b, err := os.ReadFile(e.path)
		if err != nil || !a.kitApply(b) {
			a.say("CAN'T READ THAT KIT")
			return
		}
		for i := range a.pads.s {
			a.padLoad(i)
		}
		a.padsChanged()
		a.say("KIT " + strings.ToUpper(strings.TrimSuffix(e.name, ".json")) + " LOADED")
		a.samplerOpen(smpBanks)
		return
	}
	a.mtListenStop()
	a.browseFile(e)
	if chop { // CHOP: 16 equal pieces of the part, one per pad of the bank
		piece := a.pui.bwin / 16
		for p := 0; p < 16; p++ {
			a.padAssign(a.slotIdx(p), e.path, a.pui.bstart+float64(p)*piece, piece)
		}
		a.smp.sel = 0
		a.say(fmt.Sprintf("CHOPPED OVER BANK %s  ·  16 PADS OF %.2f S", bankName(a.pads.bank), piece))
		a.samplerOpen(smpBanks)
		return
	}
	i := a.padSel()
	a.padAssign(i, e.path, a.pui.bstart, a.pui.bwin)
	a.say(fmt.Sprintf("%s ONTO %s", strings.ToUpper(strings.TrimSuffix(e.name, filepath.Ext(e.name))), slotName(i)))
	a.pui.page = pageEnv
	a.samplerOpen(smpShaper)
}

// ---------------------------------------------------------------- drawing

func (a *app) drawSampler(c *gfx.Canvas) {
	a.padsTick()
	switch a.smp.view {
	case smpTape:
		a.drawBrowser(c)
	case smpBanks:
		a.drawBanks(c)
	case smpRecIn:
		a.drawRecIn(c)
	default:
		switch {
		case a.smp.ask >= 0:
			a.drawPadEmpty(c)
		case a.pui.page == pageTrim:
			a.drawTrim(c)
		case a.pui.page == pageTune:
			a.drawTune(c)
		default:
			a.drawShaper(c)
		}
	}
	a.animate(time.Now().Add(120 * time.Millisecond))
}

// page tabs on the title row: ENV · TRIM · TUNE
func (a *app) padTitle(c *gfx.Canvas, i int) {
	s := a.pslot(i)
	c.Text(10, 46, slotName(i), gfx.Big, uText, gfx.Left|gfx.Mid)
	name := s.Name
	if len(name) > 16 {
		name = name[:16]
	}
	if s.loading {
		name += "  ·  LOADING"
	}
	c.Text(64, 47, name, gfx.Small, uDim, gfx.Left|gfx.Mid)
	segTabs(c, 310, 46, pageNames, a.pui.page, uText)
}

func (a *app) padPlaying(idx int) float64 {
	if a.smp.hitAt[idx].IsZero() {
		return -1
	}
	s := a.pslot(a.slotIdx(idx))
	dur := s.secs()
	t := time.Since(a.smp.hitAt[idx]).Seconds()
	if s.Env.Mode == 2 && a.smp.held[idx] && dur > 0 {
		return math.Mod(t, dur) / dur
	}
	if dur <= 0 || t > dur || (s.Env.Mode >= 1 && !a.smp.held[idx] && t > envRms(s.Env.R)/1000+0.03) {
		return -1
	}
	return t / dur
}

// trimmed part of the pad's peaks, resampled to cols
func (a *app) padWave(i, cols int) []float64 {
	p := a.padPeaks(i)
	s := a.pslot(i)
	if len(p) < 2 {
		return nil
	}
	out := make([]float64, cols)
	for k := range out {
		t := s.In + (s.Out-s.In)*float64(k)/float64(cols-1)
		x := t * float64(len(p)-1)
		j := int(x)
		f := x - float64(j)
		v := p[min(j, len(p)-1)]
		if j+1 < len(p) {
			v += (p[j+1] - v) * f
		}
		out[k] = v
	}
	if s.Rev {
		for l, r := 0, cols-1; l < r; l, r = l+1, r-1 {
			out[l], out[r] = out[r], out[l]
		}
	}
	return out
}

// ENV: the wave coloured by the envelope stage
func (a *app) drawShaper(c *gfx.Canvas) {
	i := a.padSel()
	s := a.pslot(i)
	e := s.Env
	btnLegend(c, [3]string{smpMotions[e.Mode], "PLAY", "TRIM"}, 0)
	a.padTitle(c, i)
	x0, y0, w, h := 8.0, 58.0, 304.0, 116.0
	glass(c, x0, y0, w, h, 10, 0x9AA0A6)
	for gy := y0 + 6; gy < y0+h-2; gy += 6 {
		for gx := x0 + 6; gx < x0+w-2; gx += 6 {
			c.Circle(gx, gy, 0.5, uDim, 0.12)
		}
	}
	cols := int(w - 16)
	wave := a.padWave(i, cols)
	dur := s.secs()
	mid, amp := y0+h/2+6, h/2-12
	ex := func(t float64) float64 { return x0 + 8 + t*(w-16) }
	R := envRms(e.R) / 1000
	off := math.Max(0, dur-R)
	if e.Mode != 0 {
		off = math.Max(envAms(e.A)/1000+envDms(e.D)/1000, dur*0.7)
	}
	if wave != nil && dur > 0 {
		top, bot, xs := make([]float64, cols), make([]float64, cols), make([]float64, cols)
		for k := 0; k < cols; k++ {
			t := float64(k) / float64(cols-1)
			g := envAt(e, t*dur, off)
			xs[k] = ex(t)
			c.Line(xs[k], mid-wave[k]*amp, xs[k], mid+wave[k]*amp*0.9, 1, uDim, 0.10)
			top[k], bot[k] = mid-wave[k]*amp*g, mid+wave[k]*amp*0.9*g
			if bot[k]-top[k] >= 0.5 {
				col := envCol(e, t*dur, off)
				c.Line(xs[k], top[k], xs[k], mid, 1, col, 0.30+0.35*g)
				c.Line(xs[k], mid, xs[k], bot[k], 1, col, 0.20+0.25*g)
			}
		}
		for k := 1; k < cols; k++ {
			col := envCol(e, float64(k)/float64(cols-1)*dur, off)
			c.Line(xs[k-1], top[k-1], xs[k], top[k], 1, gfx.Mix(col, 0xFFFFFF, 0.35), 0.9)
			c.Line(xs[k-1], bot[k-1], xs[k], bot[k], 1, col, 0.7)
		}
		if bpm := a.proj.G["tempo"]; bpm > 0 {
			beat := 60 / bpm
			for b := 1; float64(b)*beat < dur && b < 64; b++ {
				x := ex(float64(b) * beat / dur)
				c.Line(x, y0+16, x, y0+h-12, 1, shLime, 0.14)
			}
		}
		if e.Mode == 2 {
			c.Line(ex(0), y0+10, ex(1), y0+10, 1.2, shPink, 0.8)
		}
		ey := func(v float64) float64 { return mid - amp - 4 + (1-v)*(amp*0.9) }
		var exs, eys []float64
		for k := 0; k <= 160; k++ {
			t := float64(k) / 160
			exs, eys = append(exs, ex(t)), append(eys, ey(envAt(e, t*dur, off)))
		}
		c.Polyline(exs, eys, 1, 0xFFFFFF, 0.35)
		A, D := envAms(e.A)/1000, envDms(e.D)/1000
		nodes := [4][2]float64{{A, 1}, {A + D, e.S}, {off, e.S}, {math.Min(dur, off+R), 0}}
		hot := -1
		if time.Since(a.ui.touchAt) < 2*time.Second && a.ui.touchKey == fmt.Sprintf("smp%d", a.smp.envKnob) {
			hot = []int{0, 1, 3}[a.smp.envKnob]
			if a.smp.envSus {
				hot = 2
			}
		}
		for k, nd := range nodes {
			col, r := uint32(0xFFFFFF), 2.2
			if k == hot {
				col, r = knobCols[min(2, a.smp.envKnob)], 3
			}
			c.Circle(ex(math.Min(1, nd[0]/dur)), ey(nd[1]), r, col, 0.95)
		}
		if p := a.padPlaying(a.smp.sel); p >= 0 {
			px := ex(p)
			c.Line(px, y0+14, px, y0+h-10, 1.2, shLime, 0.9)
		}
		c.Text(x0+10, y0+h-7, "0:00", gfx.Small, uDim, gfx.Left|gfx.Mid)
		c.Text(x0+w-10, y0+h-7, fmt.Sprintf("%.2f S", dur), gfx.Small, uDim, gfx.Right|gfx.Mid)
	} else if s.loading {
		c.Text(160, y0+h/2, "LOADING...", gfx.Label, uDim, gfx.Center|gfx.Mid)
	}
	labels := [3]string{"ATTACK", "DECAY", "RELEASE"}
	vals := [3]string{msText(envAms(e.A)), msText(envDms(e.D)), msText(envRms(e.R))}
	norms := [3]float64{e.A, e.D, e.R}
	if a.shift {
		labels[1], vals[1], norms[1] = "SUSTAIN", fmt.Sprint(int(e.S*99+0.5)), e.S
	}
	mockKnobs(c, labels, vals, norms, a.smpHot())
	a.uiBottom(c, "LEFT: ONE SHOT / GATE / LOOP  ·  SHIFT + K2: SUSTAIN  ·  CHOP: TRIM")
}

func (a *app) smpHot() int {
	if time.Since(a.ui.touchAt) < 2*time.Second && strings.HasPrefix(a.ui.touchKey, "smp") {
		return a.smp.acc2
	}
	return -1
}

// TRIM: the pad's whole sound, IN / OUT on it (the MIXTAPE way)
func (a *app) drawTrim(c *gfx.Canvas) {
	i := a.padSel()
	s := a.pslot(i)
	in, out := "IN", "OUT"
	if a.pui.inLock {
		in = "IN · LOCK"
	}
	if a.pui.outLock {
		out = "OUT · LOCK"
	}
	on := -1 // 0.9.28: nothing picked until LEFT (IN) or RIGHT (OUT)
	if a.pui.trimSet {
		on = 0
		if a.pui.trimOut {
			on = 2
		}
	}
	btnLegend(c, [3]string{in, "PLAY", out}, on)
	a.padTitle(c, i)
	x0, y0, w, h := 8.0, 58.0, 304.0, 116.0
	glass(c, x0, y0, w, h, 10, 0x9AA0A6)
	taken := a.padTaken(i)
	z := a.trimZoom()
	focus := s.In
	if a.pui.trimOut {
		focus = s.Out
	}
	v0 := math.Max(0, math.Min(1-z, focus-z/2))
	cols := int(w - 16)
	var wave []float64
	if !s.empty() && taken > 0 {
		wave, _ = wavPeaks(s.Path, s.Start+v0*taken, z*taken, cols)
	}
	mid, amp := y0+h/2, (h/2-14)*math.Min(1, 0.6*a.trimHeight())
	ex := func(v float64) float64 { return x0 + 8 + (v-v0)/z*(w-16) }
	for k := range wave {
		v := v0 + z*float64(k)/float64(cols-1)
		col := uint32(uDim)
		if v >= s.In && v <= s.Out {
			col = cvCol(16 + (v-s.In)/math.Max(1e-6, s.Out-s.In)*288)
		}
		hgt := math.Min(h/2-6, wave[k]*amp*a.trimHeight()/math.Max(1, a.trimHeight()*0.6))
		c.Line(ex(v), mid-hgt, ex(v), mid+hgt*0.9, 1, col, 0.6)
	}
	xi, xo := ex(s.In), ex(s.Out)
	clip := func(x float64) float64 { return math.Max(x0+8, math.Min(x0+w-8, x)) }
	c.Rect(x0+8, y0+8, math.Max(0, clip(xi)-(x0+8)), h-16, uBG, 0.45)
	c.Rect(clip(xo), y0+8, math.Max(0, x0+w-8-clip(xo)), h-16, uBG, 0.45)
	if xi >= x0+8 && xi <= x0+w-8 {
		c.Line(xi, y0+8, xi, y0+h-12, 1.4, knobCols[0], 0.95)
		if a.pui.inLock {
			lockIconUI(c, xi, y0+14, knobCols[0])
		}
	}
	if xo >= x0+8 && xo <= x0+w-8 {
		c.Line(xo, y0+8, xo, y0+h-12, 1.4, knobCols[2], 0.95)
		if a.pui.outLock {
			lockIconUI(c, xo, y0+14, knobCols[2])
		}
	}
	if p := a.padPlaying(a.smp.sel); p >= 0 {
		pv := s.In + p*(s.Out-s.In)
		if s.Rev {
			pv = s.Out - p*(s.Out-s.In)
		}
		if x := ex(pv); x >= x0+8 && x <= x0+w-8 {
			c.Line(x, y0+12, x, y0+h-12, 1, shLime, 0.9)
		}
	}
	c.Text(x0+10, y0+h-6, "IN "+clockFine(s.In*taken), gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(x0+w-10, y0+h-6, "OUT "+clockFine(s.Out*taken), gfx.Small, uDim, gfx.Right|gfx.Mid)
	c.Text(160, y0+h-6, fmt.Sprintf("%.3f S", (s.Out-s.In)*taken), gfx.Small, uText, gfx.Center|gfx.Mid)
	zs := fmt.Sprintf("%.2f S", z*taken)
	if z*taken < 1 {
		zs = fmt.Sprintf("%.0f MS", z*taken*1000)
	}
	mockKnobs(c, [3]string{"HEIGHT", "SNAP", "ZOOM"}, [3]string{fmt.Sprintf("x%.1f", a.trimHeight()), map[bool]string{true: "ZERO X", false: "OFF"}[a.pui.snap], zs},
		[3]float64{(a.trimHeight() - 1) / 7, b2f(a.pui.snap), 1 - math.Log(z)/math.Log(0.004)}, a.smpHot())
	fine := "PUSH: FINE"
	if a.pui.fine {
		fine = "FINE  ·  PUSH: BIG MOVES"
	}
	a.uiBottom(c, "DATA: MOVE (FAST: FAR)  ·  "+fine+"  ·  CHOP: TUNE")
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func lockIconUI(c *gfx.Canvas, x, y float64, col uint32) {
	c.RoundRect(x-3.5, y-1, 7, 6, 1.5, col, 1)
	c.Arc(x, y-1, 2.6, 1.2, 180, 360, col, 1)
}

// TUNE: pitch, fine, level; SHIFT + K1 pan; left = reverse
func (a *app) drawTune(c *gfx.Canvas) {
	i := a.padSel()
	s := a.pslot(i)
	btnLegend(c, [3]string{map[bool]string{true: "REVERSE", false: "FORWARD"}[s.Rev], "PLAY", ""}, map[bool]int{true: 0, false: -1}[s.Rev])
	a.padTitle(c, i)
	glass(c, 8, 58, 304, 116, 10, 0x9AA0A6)
	wave := a.padWave(i, 288)
	for k, v := range wave {
		x := 16 + float64(k)
		c.Line(x, 116-v*30, x, 116+v*27, 1, cvCol(x), 0.35)
	}
	st := fmt.Sprintf("%+d", int(s.Pitch))
	if s.Pitch == 0 {
		st = "0"
	}
	c.Text(160, 100, st+" ST", gfx.Title, uText, gfx.Center|gfx.Mid)
	c.Text(160, 122, fmt.Sprintf("FINE %+d  ·  LEVEL %d  ·  PAN %s", int(s.Fine), int(s.Level*99/1.5+0.5), panText(s.Pan)), gfx.Small, uDim, gfx.Center|gfx.Mid)
	labels := [3]string{"PITCH", "FINE", "LEVEL"}
	vals := [3]string{st, fmt.Sprintf("%+d", int(s.Fine)), fmt.Sprint(int(s.Level*99/1.5 + 0.5))}
	norms := [3]float64{(s.Pitch + 24) / 48, (s.Fine + 100) / 200, s.Level / 1.5}
	if a.shift {
		labels[0], vals[0], norms[0] = "PAN", panText(s.Pan), s.Pan
	}
	mockKnobs(c, labels, vals, norms, a.smpHot())
	a.uiBottom(c, "SHIFT + K1: PAN  ·  LEFT: REVERSE  ·  CHOP: DONE")
}

func panText(p float64) string {
	v := int(math.Round((p - 0.5) * 200))
	switch {
	case v == 0:
		return "C"
	case v < 0:
		return fmt.Sprintf("L%d", -v)
	}
	return fmt.Sprintf("R%d", v)
}

// an empty pad: where does its sound come from
func (a *app) drawPadEmpty(c *gfx.Canvas) {
	i := a.slotIdx(a.smp.ask)
	btnLegend(c, [3]string{"SAMPLE IN", "", "SAMPLE SELECT"}, -1)
	glass(c, 8, 58, 304, 116, 10, 0x9AA0A6)
	for gy := 64.0; gy < 170; gy += 6 {
		for gx := 14.0; gx < 308; gx += 6 {
			c.Circle(gx, gy, 0.5, uDim, 0.12)
		}
	}
	c.RoundRect(40, 84, 240, 64, 12, uBG, 0.92)
	c.RoundRectLine(40, 84, 240, 64, 12, 1, 0x5FE8FF, 0.7)
	c.Text(160, 102, slotName(i)+" IS EMPTY", gfx.Title, uText, gfx.Center|gfx.Mid)
	c.Text(160, 124, "LEFT: RECORD WHAT YOU HEAR  ·  RIGHT: FROM THE CARD", gfx.Small, uDim, gfx.Center|gfx.Mid)
	a.uiBottom(c, "A PAD WITH A SOUND: SHAPE IT  ·  PAD BANK: ALL BANKS")
}

// SAMPLE SELECT: folders on the left, the file's wave and the part to take
func (a *app) drawBrowser(c *gfx.Canvas) {
	if time.Since(a.pui.bscan) > 4*time.Second || a.pui.bents == nil {
		a.browseScan()
	}
	e, ok := a.browseSel()
	right := "TO " + slotName(a.padSel())
	mid := "LISTEN"
	if a.mt.playing {
		mid = "STOP"
	}
	if ok && e.dir {
		right, mid = "OPEN", ""
	} else if ok && e.kit {
		right, mid = "LOAD KIT", ""
	}
	btnLegend(c, [3]string{"UP", mid, right}, -1)
	c.Text(10, 46, "SAMPLE SELECT", gfx.Big, uText, gfx.Left|gfx.Mid)
	rel, _ := filepath.Rel(a.cardRoot(), a.pui.bdir)
	rel = "/ " + strings.ToUpper(strings.ReplaceAll(rel, string(filepath.Separator), " / "))
	if rel == "/ ." {
		rel = "/ CARD"
	}
	if len(rel) > 24 {
		rel = "..." + rel[len(rel)-21:]
	}
	c.Text(150, 47, rel, gfx.Small, uDim, gfx.Left|gfx.Mid)
	glass(c, 8, 58, 130, 128, 8, 0x9AA0A6)
	n := len(a.pui.bents)
	first := max(0, min(n-7, a.pui.bidx-3))
	for k := first; k < min(n, first+7); k++ {
		be := a.pui.bents[k]
		y := 70 + float64(k-first)*16
		sel := k == a.pui.bidx
		if sel {
			c.RoundRect(12, y-7, 122, 14, 4, shLime, 0.9)
		}
		col := uint32(uText)
		name := strings.ToUpper(be.name)
		if be.dir {
			name = "/ " + name
			col = 0x8FB8C8
		}
		if be.kit {
			col = pYellow
		}
		if sel {
			col = uBG
		}
		if len(name) > 20 {
			name = name[:19] + "~"
		}
		c.Text(18, y, name, gfx.Small, col, gfx.Left|gfx.Mid)
	}
	if n == 0 {
		c.Text(73, 120, "NO WAV FILES HERE", gfx.Small, uDim, gfx.Center|gfx.Mid)
	}
	glass(c, 144, 58, 168, 128, 8, 0x9AA0A6)
	if ok && !e.dir && !e.kit {
		total := a.browseFile(e)
		ov, _ := wavPeaks(e.path, 0, 0, 156)
		for k, v := range ov {
			t := float64(k) / 155
			x := 150 + float64(k)
			col := uint32(uDim)
			in := t*total >= a.pui.bstart && t*total <= a.pui.bstart+a.pui.bwin
			if in {
				col = cvCol(16 + t*288)
			}
			c.Line(x, 104-v*30, x, 104+v*27, 1, col, map[bool]float64{true: 0.75, false: 0.35}[in])
		}
		if total > 0 {
			xa := 150 + a.pui.bstart/total*156
			xb := 150 + math.Min(1, (a.pui.bstart+a.pui.bwin)/total)*156
			c.RoundRectLine(xa, 70, math.Max(2, xb-xa), 68, 3, 1, shLime, 0.85)
		}
		info := fmt.Sprintf("%.2f S", total)
		if total >= 60 {
			info = clock(total)
		}
		c.Text(152, 152, strings.ToUpper(info)+" FILE", gfx.Small, uDim, gfx.Left|gfx.Mid)
		c.Text(152, 166, fmt.Sprintf("TAKE %.2f S FROM %s", a.pui.bwin, clockFine(a.pui.bstart)), gfx.Small, uText, gfx.Left|gfx.Mid)
		c.Text(152, 178, "SHIFT + PUSH: CHOP OVER BANK "+bankName(a.pads.bank), gfx.Small, uDim, gfx.Left|gfx.Mid)
	} else if ok && e.kit {
		c.Text(228, 120, "A KIT: 128 PADS", gfx.Small, uDim, gfx.Center|gfx.Mid)
	}
	mockKnobs(c, [3]string{"START", "", "TAKE"}, [3]string{clockFine(a.pui.bstart), "", fmt.Sprintf("%.2f S", a.pui.bwin)},
		[3]float64{0, 0, a.pui.bwin / padMaxSecs}, a.smpHot())
	a.uiBottom(c, "DATA: PICK  ·  PUSH: OPEN / ONTO THE PAD  ·  PADS: WHICH PAD")
}

// PAD BANK: 8 banks x 16 pads
func (a *app) drawBanks(c *gfx.Canvas) {
	btnLegend(c, [3]string{"", "SHAPE", ""}, -1)
	full := 0
	for i := range a.pads.s {
		if !a.pads.s[i].empty() {
			full++
		}
	}
	c.Text(10, 46, "PAD BANKS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(112, 47, fmt.Sprintf("%d SOUNDS  ·  BANK %s", full, bankName(a.pads.bank)), gfx.Small, uDim, gfx.Left|gfx.Mid)
	for b := 0; b < 8; b++ {
		bx := 8 + float64(b%4)*77
		by := 58 + float64(b/4)*68
		tint := uint32(0x9AA0A6)
		if b == a.pads.bank {
			tint = shLime
		}
		glass(c, bx, by, 73, 64, 6, tint)
		n := 0
		for p := 0; p < 16; p++ {
			if !a.pads.s[b*16+p].empty() {
				n++
			}
		}
		c.Text(bx+6, by+9, bankName(b), gfx.Label, uText, gfx.Left|gfx.Mid)
		c.Text(bx+68, by+9, fmt.Sprint(n), gfx.Small, uDim, gfx.Right|gfx.Mid)
		for p := 0; p < 16; p++ {
			px := bx + 6 + float64(p%4)*16.5
			py := by + 54 - float64(p/4)*10.5
			s := &a.pads.s[b*16+p]
			if s.empty() {
				c.Circle(px+6, py, 1, uDim, 0.4)
				continue
			}
			col := tcol(b % 8)
			if b == a.pads.bank && p == a.smp.sel {
				c.RoundRect(px-1, py-5, 14, 10, 2, uText, 0.25)
			}
			if pk := a.padPeaks(b*16 + p); len(pk) > 0 {
				for k := 0; k < 12; k++ {
					v := pk[k*len(pk)/12]
					c.Line(px+float64(k), py-v*4, px+float64(k), py+v*4, 1, col, 0.85)
				}
			} else {
				c.RoundRect(px, py-1, 12, 2, 1, col, 0.6)
			}
		}
	}
	a.uiBottom(c, "PAD BANK: NEXT BANK  ·  PADS: PLAY / PICK  ·  PUSH: SHAPE  ·  ERASE + PAD: EMPTY")
}

// SAMPLE IN: record what you hear onto the pad
func (a *app) drawRecIn(c *gfx.Canvas) {
	i := a.padSel()
	mid := "RECORD"
	if a.pads.recOn {
		mid = "STOP"
	}
	btnLegend(c, [3]string{"BACK", mid, ""}, map[bool]int{true: 1, false: -1}[a.pads.recOn])
	c.Text(10, 46, "SAMPLE IN", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(112, 47, "ONTO "+slotName(i)+"  ·  WHAT YOU HEAR", gfx.Small, uDim, gfx.Left|gfx.Mid)
	glass(c, 8, 58, 304, 116, 10, uRed)
	a.dsp.pads.mu.Lock()
	n := a.dsp.pads.recLen
	a.dsp.pads.mu.Unlock()
	secs := float64(n) / 48000
	if a.pads.recOn {
		secs = math.Max(secs, time.Since(a.pads.recStart).Seconds())
		if secs >= padMaxSecs+0.2 {
			a.recInToggle()
		}
	}
	c.Text(160, 104, fmt.Sprintf("%.1f S", math.Min(secs, padMaxSecs)), gfx.Title, uText, gfx.Center|gfx.Mid)
	c.RoundRect(28, 130, 264, 6, 3, uPanel2, 1)
	c.RoundRect(28, 130, 264*math.Min(1, secs/padMaxSecs), 6, 3, uRed, 0.9)
	if a.pads.recOn && int(time.Since(a.pads.recStart).Seconds()*2)%2 == 0 {
		c.Circle(28, 104, 5, uRed, 1)
	}
	c.Text(160, 152, "UP TO 20 S  ·  PLAY THE PADS, THE SYNTHS, THE TAPE: IT ALL GOES IN", gfx.Small, uDim, gfx.Center|gfx.Mid)
	a.uiBottom(c, "MIDDLE / PUSH: RECORD · STOP  ·  LEFT: BACK")
}
