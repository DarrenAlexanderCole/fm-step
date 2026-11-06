package main

// SHIFT + PAD (0.9.26): the functions printed under the MPC Sample's pads.
// Hold SHIFT for a moment and the sheet shows what each pad does; SHIFT + pad
// does it. Pad 16 PROJECT opens a short menu (DATA: pick, DATA push: do).
// Anything that throws work away asks twice, and UNDO brings back the last
// thing the utilities changed.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/seq"
)

type utilPad struct {
	label string
	soon  bool // printed on the MPC, not in FM-STEP yet
}

// in pad order (pad 1 = index 0), as printed on the MPC Sample
var utilPads = [16]utilPad{
	{"FULL LEVEL", false}, {"HALF SEQ", false}, {"DOUBLE SEQ", false}, {"COUNT-IN", true},
	{"COMPRESSOR", false}, {"HALF SPEED", false}, {"DOUBLE SPEED", false}, {"MIDI CONFIG", true},
	{"FADER", false}, {"REC QUANTIZE", false}, {"RESAMPLE", false}, {"SONG", false},
	{"TRIM SAMPLE", false}, {"TIME CORRECT", false}, {"WARP", true}, {"PROJECT", false},
}

const utilShowAfter = 450 * time.Millisecond

type utilState struct {
	shiftAt   time.Time
	shiftUsed bool     // a knob, pad or button went with SHIFT: no sheet
	swallow   [16]bool // the release of a SHIFT + pad press
	fullLevel bool
	menu      bool
	menuIdx   int
	ask       string // a menu item / pad waiting for its second press
	askAt     time.Time
	undoName  string
	undo      func()
}

// utilShift: SHIFT went down / up.
func (a *app) utilShift(down bool) {
	if down {
		a.util.shiftAt, a.util.shiftUsed = time.Now(), false
		a.animate(time.Now().Add(utilShowAfter + 50*time.Millisecond))
	}
	a.dirty = true
}

func (a *app) utilShown() bool {
	return a.shift && !a.util.shiftUsed && !a.util.shiftAt.IsZero() && time.Since(a.util.shiftAt) >= utilShowAfter &&
		a.mode != modeConfirmExit
}

// utilPadPress: SHIFT + pad. true = handled (the pad does nothing else).
func (a *app) utilPadPress(idx int, on bool) bool {
	if !on {
		if a.util.swallow[idx] {
			a.util.swallow[idx] = false
			return true
		}
		return false
	}
	if !a.shift || a.mode == modePatterns || (a.mode == modeTracks && idx >= seq.Tracks) {
		return false // PATTERNS: SHIFT + pad chains · TRACKS: SHIFT + mute pad solos
	}
	a.util.swallow[idx] = true
	a.util.shiftUsed = true
	a.utilDo(idx)
	a.dirty = true
	return true
}

// ask: true on the second press within 3 s
func (a *app) utilAsk(name, what string) bool {
	if a.util.ask == name && time.Since(a.util.askAt) < 3*time.Second {
		a.util.ask = ""
		return true
	}
	a.util.ask, a.util.askAt = name, time.Now()
	a.say(what + "?  AGAIN TO DO IT")
	return false
}

// snapshot: what UNDO brings back (the whole project, as it is now)
func (a *app) utilSnapshot(name string) {
	b, err := a.proj.MarshalJSON()
	if err != nil {
		return
	}
	playing := a.sq.playing
	a.util.undoName = name
	a.util.undo = func() {
		var p seq.Project
		if p.UnmarshalJSON(b) != nil {
			return
		}
		if playing && a.sq.playing { // keep playing: rewrite the patterns only
			a.proj.Pat, a.proj.Chain = p.Pat, p.Chain
			for i := range a.proj.Pat {
				a.dsp.lines(a.proj.PatternCmds(i))
			}
			a.dsp.send(seq.ChainCmd(a.proj.Chain))
			a.sq.projDirty = true
			return
		}
		a.setProject(&p)
		a.sq.projDirty = true
	}
}

func (a *app) utilDo(idx int) {
	u := utilPads[idx]
	if u.soon {
		a.say(u.label + ": NOT IN FM-STEP YET")
		return
	}
	p := a.pat()
	pi := a.sq.viewPat
	switch idx {
	case 0: // FULL LEVEL
		a.util.fullLevel = !a.util.fullLevel
		a.say(map[bool]string{true: "FULL LEVEL ON  ·  EVERY HIT AT 127", false: "FULL LEVEL OFF"}[a.util.fullLevel])
	case 1: // HALF SEQ
		if p.Len <= 1 {
			a.say("PATTERN IS 1 STEP ALREADY")
			return
		}
		a.utilSnapshot("HALF SEQ")
		p.Len = max(1, p.Len/2)
		a.dsp.lines(a.proj.PatternCmds(pi))
		a.sq.projDirty = true
		a.say(fmt.Sprintf("PATTERN %d: %d STEPS", pi+1, p.Len))
	case 2: // DOUBLE SEQ: the pattern twice over (locks too)
		if p.Len*2 > seq.Steps {
			a.say(fmt.Sprintf("LONGEST IS %d STEPS", seq.Steps))
			return
		}
		a.utilSnapshot("DOUBLE SEQ")
		for t := range p.S {
			for s := 0; s < p.Len; s++ {
				st := p.S[t][s]
				st.Locks = append([]seq.Lock(nil), st.Locks...)
				p.S[t][s+p.Len] = st
			}
		}
		p.Len *= 2
		a.dsp.lines(a.proj.PatternCmds(pi))
		a.sq.projDirty = true
		a.say(fmt.Sprintf("PATTERN %d: %d STEPS", pi+1, p.Len))
	case 4: // COMPRESSOR: the recorder's COMP + FX page
		a.mtShow(true)
		a.mt.compPage = true
		a.say("COMP + FX  ·  RIGHT BUTTON: BACK TO THE TAPE")
	case 5, 6: // HALF / DOUBLE SPEED (tempo)
		bpm := a.proj.G["tempo"]
		if idx == 5 {
			bpm /= 2
		} else {
			bpm *= 2
		}
		bpm = math.Max(40, math.Min(200, math.Round(bpm)))
		a.setVal("tempo", bpm)
		a.say(fmt.Sprintf("TEMPO %d BPM", int(bpm)))
	case 8: // FADER: the fader effects
		a.mt.view = false
		if a.smp.view != smpOff {
			a.smpLeave()
		}
		a.setMode(modeFX)
		a.paintPads()
		a.say("FADER EFFECTS")
	case 9: // REC QUANTIZE
		v := 1.0
		if a.proj.G["rec_quant"] > 0.5 {
			v = 0
		}
		a.setVal("rec_quant", v)
		a.say(map[bool]string{true: "REC QUANTIZE ON  ·  RECORDED NOTES LAND ON THE STEPS", false: "REC QUANTIZE OFF  ·  RECORDED NOTES KEEP THEIR FEEL"}[v > 0.5])
	case 10: // RESAMPLE: MIXTAPE records what you hear
		a.mtShow(true)
		a.say("RESAMPLE: SAMPLE RECORD TAKES WHAT YOU HEAR")
	case 11: // SONG: the patterns and the chain
		a.mt.view = false
		if a.smp.view != smpOff {
			a.smpLeave()
		}
		a.setMode(modePatterns)
		a.paintPads()
		a.say("SONG  ·  SHIFT + PAD: CHAIN PATTERNS")
	case 12: // TRIM SAMPLE: the SHAPER
		a.shaperOpen()
	case 13: // TIME CORRECT: the track's notes onto the steps
		a.utilSnapshot("TIME CORRECT")
		n := 0
		for s := 0; s < p.Len; s++ {
			if st := &p.S[a.proj.Track][s]; st.On && st.Micro != 0 {
				st.Micro = 0
				n++
			}
		}
		a.dsp.lines(a.proj.PatternCmds(pi))
		a.sq.projDirty = true
		a.say(fmt.Sprintf("TIME CORRECT: %d NOTES OF TRACK %d ON THE STEPS", n, a.proj.Track+1))
	case 15: // PROJECT
		a.util.menu, a.util.menuIdx, a.util.ask = true, 0, ""
		a.say("PROJECT  ·  DATA: PICK  ·  PUSH: DO")
	}
}

// ---------------------------------------------------------------- PROJECT menu

type menuItem struct {
	label string
	ask   bool // asks twice
	do    func(a *app)
}

var projectMenu = []menuItem{
	{"SAVE", false, (*app).utilSave},
	{"PROJECT SLOTS", false, func(a *app) { a.setMode(modeProject); a.paintPads() }},
	{"NEW PROJECT", true, (*app).utilNew},
	{"CLEAR PATTERN", true, (*app).utilClearPattern},
	{"CLEAR AUTOMATION  ·  TRACK", true, func(a *app) { a.utilClearAuto(true) }},
	{"CLEAR AUTOMATION  ·  PATTERN", true, func(a *app) { a.utilClearAuto(false) }},
	{"UNDO", false, (*app).utilUndo},
	{"EXPORT WAV  ·  NOT YET", false, func(a *app) { a.say("EXPORT: NOT IN FM-STEP YET (MIXTAPE RECORDS THE MIX)") }},
}

// utilMenuData: DATA turn in the menu
func (a *app) utilMenuData(d int) {
	a.util.menuIdx = max(0, min(len(projectMenu)-1, a.util.menuIdx+d))
	a.util.ask = ""
	a.dirty = true
}

func (a *app) utilMenuPush() {
	it := projectMenu[a.util.menuIdx]
	if it.ask && !a.utilAsk(it.label, it.label) {
		a.dirty = true
		return
	}
	a.util.menu = false
	it.do(a)
	a.dirty = true
}

func (a *app) utilSave() {
	n := a.sq.projSlot
	if n < 1 {
		a.setMode(modeProject)
		a.paintPads()
		a.say("NOT SAVED YET  ·  HOLD SAMPLE RECORD + PAD: SAVE TO THAT SLOT")
		return
	}
	held := a.recHeld
	a.recHeld = true
	a.onProjectPad(n - 1)
	a.recHeld = held
}

func (a *app) utilNew() {
	a.utilSnapshot("NEW PROJECT")
	held := a.sq.eraseHeld
	a.sq.eraseHeld = true
	a.onProjectPad(0) // ERASE + slot = new project (same sounds)
	a.sq.eraseHeld = held
	a.sq.projSlot = 0
}

func (a *app) utilClearPattern() {
	a.utilSnapshot("CLEAR PATTERN")
	pi := a.sq.viewPat
	l := a.proj.Pat[pi].Len
	a.proj.Pat[pi].Clear()
	a.proj.Pat[pi].Len = l
	a.dsp.lines(a.proj.PatternCmds(pi))
	a.sq.projDirty = true
	a.say(fmt.Sprintf("PATTERN %d CLEARED", pi+1))
}

// utilClearAuto: drop the recorded knob moves (step locks)
func (a *app) utilClearAuto(track bool) {
	a.utilSnapshot("CLEAR AUTOMATION")
	pi := a.sq.viewPat
	p := &a.proj.Pat[pi]
	n := 0
	for t := range p.S {
		if track && t != a.proj.Track {
			continue
		}
		for s := range p.S[t] {
			n += len(p.S[t][s].Locks)
			p.S[t][s].Locks = nil
		}
	}
	a.dsp.lines(a.proj.PatternCmds(pi))
	a.sq.auto = nil
	a.sq.projDirty = true
	if track {
		a.say(fmt.Sprintf("AUTOMATION CLEARED: TRACK %d, PATTERN %d (%d MOVES)", a.proj.Track+1, pi+1, n))
	} else {
		a.say(fmt.Sprintf("AUTOMATION CLEARED: PATTERN %d (%d MOVES)", pi+1, n))
	}
}

func (a *app) utilUndo() {
	if a.util.undo == nil {
		a.say("NOTHING TO UNDO")
		return
	}
	f, name := a.util.undo, a.util.undoName
	a.util.undo = nil
	f()
	a.paintPads()
	a.say("UNDONE: " + name)
}

// ---------------------------------------------------------------- drawing

// drawUtil: the SHIFT sheet (the pads as they sit on the MPC: 1-4 bottom row)
func (a *app) drawUtil(c *gfx.Canvas) {
	c.RoundRect(0, 22, 320, 218, 0, uBG, 1)
	c.Text(10, 34, "SHIFT + PAD", gfx.Big, uText, gfx.Left|gfx.Mid)
	w, h := 74.0, 40.0
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4 // pads 1-4 are the bottom row
		x, y := 10+float64(col)*76, 50+float64(row)*42
		u := utilPads[i]
		tint := uint32(0x5FE8FF)
		switch {
		case u.soon:
			tint = uDim
		case i == 15:
			tint = 0xD8FF4A
		case i == 0 && a.util.fullLevel, i == 9 && a.proj.G["rec_quant"] > 0.5:
			tint = 0xFF9A2E
		}
		glass(c, x, y, w, h, 6, tint)
		c.Text(x+w-5, y+8, fmt.Sprint(i+1), gfx.Small, uDim, gfx.Right|gfx.Mid)
		alpha := uint32(uText)
		if u.soon {
			alpha = uDim
		}
		c.Text(x+6, y+h-11, u.label, gfx.Small, alpha, gfx.Left|gfx.Mid)
		if (i == 0 && a.util.fullLevel) || (i == 9 && a.proj.G["rec_quant"] > 0.5) {
			c.Circle(x+8, y+9, 2.5, 0xFF9A2E, 1)
		}
	}
	und := "UNDO: NOTHING YET"
	if a.util.undo != nil {
		und = "PROJECT > UNDO: " + a.util.undoName
	}
	c.Text(160, 230, und, gfx.Small, uDim, gfx.Center|gfx.Mid)
}

// drawProjectMenu: pad 16's list
func (a *app) drawProjectMenu(c *gfx.Canvas) {
	c.RoundRect(0, 22, 320, 218, 0, uBG, 1)
	c.Text(10, 34, "PROJECT", gfx.Big, uText, gfx.Left|gfx.Mid)
	name := a.proj.Name
	if a.sq.projSlot > 0 {
		name = fmt.Sprintf("SLOT %d  ·  %s", a.sq.projSlot, name)
	}
	c.Text(100, 35, name, gfx.Small, uDim, gfx.Left|gfx.Mid)
	glass(c, 8, 48, 304, 166, 8, 0xD8FF4A)
	for i, it := range projectMenu {
		y := 62 + float64(i)*19
		if i == a.util.menuIdx {
			c.RoundRect(14, y-8, 292, 16, 5, 0xD8FF4A, 0.9)
			lab := it.label
			if a.util.ask == it.label && time.Since(a.util.askAt) < 3*time.Second {
				lab += "  ·  PUSH AGAIN"
			}
			c.Text(22, y, lab, gfx.Label, uBG, gfx.Left|gfx.Mid)
			continue
		}
		col := uint32(uText)
		if it.label == "UNDO" && a.util.undo == nil {
			col = uDim
		}
		c.Text(22, y, it.label, gfx.Label, col, gfx.Left|gfx.Mid)
	}
	a.uiBottom(c, "DATA: PICK  ·  PUSH: DO  ·  ANY BUTTON: CLOSE")
}
