package main

// Sequencer screens and controls for FM-STEP 0.7: step edit (SEQ), track
// select/mute (SAMPLE), patterns and chain (LOOP), project slots
// (SHIFT+SAMPLE SELECT), live recording (SEQ RECORD) and the playhead.
// The clock and the pattern data that plays live in fmstep-dsp; the UI keeps
// the same data in a.proj and mirrors every edit to the engine.

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

type preview struct {
	track, note int
	at          time.Time
}

type seqUI struct {
	solo [seq.Tracks]bool // 0.9.24: SHIFT + pad 9-16 in TRACKS
	pad      [16][3]byte
	padKnown [16]bool

	playing         bool
	posPat, posStep int
	recArm          bool
	recVal          map[string]float64 // knob values being recorded as locks
	recKnob         bool

	held       int // step+1 being held in step mode, 0 = none
	pressWasOn bool
	heldEdited bool
	stepPage   int
	viewPat    int  // pattern the step/tracks views show: the playing one while following, else the selected one
	noFollow   bool // step view pads stay on a page you picked (PAD BANK) instead of following the playhead
	knobAcc    [3]float64

	eraseHeld, muteHeld, chopHeld bool
	eraseArmAt, eraseUndoAt       time.Time   // SHIFT+ERASE: armed / just cleared
	eraseUndo                     *seq.Pattern // what the last erase-all removed
	eraseUndoPat                  int
	fxHeld, fxUsed                bool // KNOB FX held / a pad was used while held
	loopHeld, loopUsed            bool // LOOP held / a length pad was used while held
	loopAt                        time.Time
	loopPainted                   bool

	projDirty bool
	lastSave  time.Time
	projSlot  int
	projNames [16]string
	previews  []preview
	flash     [seq.Tracks]time.Time
	lastPaint time.Time

	// tour progress
	tPlayed, tMuted, tTrackSel, tStepToggled, tStepEdit, tLocked, tPatSwitched, tRecorded bool
	fxToggled, didReset                                                                   bool
	resetKey                                                                              string
	resetAt, lastResetPress                                                               time.Time
	fxPicked, lenJumped                                                                   bool
}

// One colour per track (7-bit RGB for the pads; rgb7 converts for the screen).
var trackColours = [seq.Tracks][3]byte{
	{127, 20, 10}, {127, 70, 0}, {120, 110, 0}, {50, 120, 10},
	{0, 60, 127}, {80, 20, 127}, {0, 110, 110}, {127, 20, 80},
}

func rgb7(c [3]byte) uint32 {
	return uint32(c[0])*2<<16 | uint32(c[1])*2<<8 | uint32(c[2])*2
}

func scale7(c [3]byte, f float64) (byte, byte, byte) {
	return byte(float64(c[0]) * f), byte(float64(c[1]) * f), byte(float64(c[2]) * f)
}

var gateChoices = []float64{0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6, 8, 12, 16}

func gateName(g float64) string {
	switch {
	case g < 0.2:
		return "TICK"
	case g < 0.3:
		return "1/4 STEP"
	case g < 0.6:
		return "1/2 STEP"
	case g < 0.9:
		return "3/4 STEP"
	case g < 1.2:
		return "1 STEP"
	}
	return strconv.FormatFloat(math.Round(g*10)/10, 'f', -1, 64) + " STEPS"
}

var noteNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func noteName(n int) string { return fmt.Sprintf("%s%d", noteNames[n%12], n/12-1) }

func (a *app) projectsDir() string { return filepath.Join(a.user, "projects") }

func (a *app) pat() *seq.Pattern { return &a.proj.Pat[a.sq.viewPat] }

// setProject loads a project into the UI and the engine (sequencer stopped).
func (a *app) setProject(p *seq.Project) {
	a.proj = p
	a.sq.solo = [seq.Tracks]bool{}
	a.dsp.lines(p.Commands())
	a.sq.playing, a.sq.posPat, a.sq.held, a.sq.recVal = false, -1, 0, nil
	a.sq.viewPat = p.Sel
	a.selectTrack(p.Track)
	a.sq.projDirty = false
	a.sq.lastSave = time.Now()
}

func (a *app) releaseHeld() {
	for idx, n := range a.held {
		a.dsp.send("O %d", n)
		delete(a.held, idx)
	}
}

func (a *app) selectTrack(t int) {
	a.releaseHeld()
	a.proj.Track = t
	a.preset = &a.proj.Tracks[t].Sound
	a.slot = a.proj.Tracks[t].Slot
	if a.slot < 1 || a.slot > 64 {
		a.slot = 1
	}
	a.modified = false
	a.sq.recVal = nil
	a.accum = [3]float64{}
	a.dsp.send("U %d", t)
	a.dirty = true
	a.paintPads()
}

// sendMutes (0.9.24): what the engine hears: muted tracks, and while any
// track is soloed every track that isn't.
func (a *app) sendMutes() {
	any := false
	for _, s := range a.sq.solo {
		any = any || s
	}
	for t := 0; t < seq.Tracks; t++ {
		m := a.proj.Tracks[t].Muted || (any && !a.sq.solo[t])
		a.dsp.send("M %d %d", t, b2i(m))
	}
}

// toggleSolo: SHIFT + pad 9-16 in the TRACKS view (not saved with the project).
func (a *app) toggleSolo(t int) {
	a.sq.solo[t] = !a.sq.solo[t]
	a.sendMutes()
	if a.sq.solo[t] {
		a.say(fmt.Sprintf("TRACK %d SOLO  ·  SHIFT + PAD AGAIN: OFF", t+1))
	} else {
		a.say(fmt.Sprintf("TRACK %d SOLO OFF", t+1))
	}
	a.paintPads()
	a.dirty = true
}

func (a *app) toggleMute(t int) {
	tr := &a.proj.Tracks[t]
	tr.Muted = !tr.Muted
	a.sendMutes()
	a.sq.tMuted = true
	a.sq.projDirty = true
	if tr.Muted {
		a.say(fmt.Sprintf("TRACK %d %s MUTED", t+1, tr.Sound.Name))
	} else {
		a.say(fmt.Sprintf("TRACK %d %s ON", t+1, tr.Sound.Name))
	}
	a.paintPads()
}

// noteTails says, for each step of track t, how much of it is covered by a
// note that started on an earlier step (0..1), so long notes can be drawn
// across the steps they ring through. Notes past the end wrap around.
func noteTails(p *seq.Pattern, t int) []float64 {
	cov := make([]float64, p.Len)
	for s := 0; s < p.Len; s++ {
		st := &p.S[t][s]
		if !st.On {
			continue
		}
		end := float64(s) + st.Micro + st.Gate
		for k := s + 1; float64(k) < end && k < s+p.Len; k++ {
			if c := math.Min(1, end-float64(k)); c > cov[k%p.Len] {
				cov[k%p.Len] = c
			}
		}
	}
	return cov
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// fxAmount shows how strongly effect i is acting: OFF at its neutral point,
// 100% at the far end of the fader (PAN shows its side).
func fxAmount(i int, v float64) string {
	ft := faderTargets[i]
	if ft.key == "fx_pan" {
		d := int(math.Round((v - 0.5) * 200))
		switch {
		case d < -1:
			return fmt.Sprintf("LEFT %d%%", -d)
		case d > 1:
			return fmt.Sprintf("RIGHT %d%%", d)
		}
		return "CENTRE"
	}
	span := math.Max(ft.neutral, 1-ft.neutral)
	p := int(math.Round(math.Abs(v-ft.neutral) / span * 100))
	if p < 1 {
		return "OFF"
	}
	return fmt.Sprintf("%d%%", p)
}

func dim(c [3]byte, f float64) uint32 {
	r, g, b := scale7(c, f)
	return rgb7([3]byte{r, g, b})
}

// pages is how many 16-step bars a pattern of length n has.
func bars(n int) int { return max(1, (n+15)/16) }

// followPage keeps the step view's pads on the bar that is playing (the
// default), unless a step is held or a bar was picked with PAD BANK.
func (a *app) followPage() {
	if a.sq.held != 0 {
		return // never move the pads under a held step
	}
	switch {
	case a.sq.playing && a.sq.posPat >= 0 && !a.sq.noFollow:
		a.sq.viewPat = a.sq.posPat // show what is playing (chains, recording)
	case !a.sq.playing:
		a.sq.viewPat = a.proj.Sel
	}
	if a.mode != modeStep || a.sq.noFollow || !a.sq.playing || a.sq.posPat != a.sq.viewPat {
		return
	}
	if pg := a.sq.posStep / 16; pg != a.sq.stepPage && pg < bars(a.pat().Len) {
		a.sq.stepPage = pg
	}
}

func (a *app) setMode(m mode) {
	if m != a.mode {
		a.sq.noFollow = false // every visit to the step view starts out following
	}
	if m != modePlay {
		a.ui.scope = 0
	}
	a.mode = m
	a.sq.held = 0
	if m == modeStep {
		a.sq.stepPage = min(a.sq.stepPage, bars(a.pat().Len)-1)
		a.followPage()
	}
	if m == modeProject {
		for i := range a.sq.projNames {
			a.sq.projNames[i] = ""
			if p, err := seq.Load(seq.SlotPath(a.projectsDir(), i+1)); err == nil {
				a.sq.projNames[i] = p.Name
			}
		}
	}
	a.dirty = true
	a.paintPads()
}

func (a *app) sendStep(s int) {
	a.dsp.lines(seq.StepCmds(a.sq.viewPat, a.proj.Track, s, a.pat().S[a.proj.Track][s]))
	a.sq.projDirty = true
	a.dirty = true
}

func (a *app) previewNote(t, note int, vel float64) {
	if a.sq.playing {
		return
	}
	a.dsp.send("N %d %.3f %d", note, vel, t)
	a.sq.previews = append(a.sq.previews, preview{t, note, time.Now().Add(220 * time.Millisecond)})
}

// ------------------------------------------------------------------ pads

func (a *app) paintSeqPads() {
	a.sq.lastPaint = time.Now()
	t := a.proj.Track
	c := trackColours[t]
	if a.loopShown() {
		a.sq.loopPainted = true
		a.paintLengthPads()
		return
	}
	if a.sq.fxHeld || a.mode == modeFX {
		a.paintFXPads()
		return
	}
	switch a.mode {
	case modeStep:
		p := a.pat()
		tails := noteTails(p, t)
		for i := 0; i < 16; i++ {
			s := a.sq.stepPage*16 + i
			st := &p.S[t][s]
			switch {
			case s >= p.Len:
				a.padColour(i, 0, 0, 0)
			case a.sq.held == s+1:
				a.padColour(i, 127, 60, 0)
			case a.sq.playing && a.sq.posPat == a.sq.viewPat && a.sq.posStep == s:
				a.padColour(i, 127, 127, 127)
			case st.On:
				r, g, b := scale7(c, 0.3+0.7*st.Vel)
				a.padColour(i, r, g, b)
			case tails[s] > 0: // a long note is still ringing here
				r, g, b := scale7(c, 0.12)
				a.padColour(i, r, g, b)
			case len(st.Locks) > 0:
				a.padColour(i, 25, 0, 35)
			case s%4 == 0:
				a.padColour(i, 7, 7, 12)
			default:
				a.padColour(i, 2, 2, 4)
			}
		}
	case modeTracks:
		for i := 0; i < seq.Tracks; i++ {
			switch {
			case time.Since(a.sq.flash[i]) < 90*time.Millisecond && !a.proj.Tracks[i].Muted:
				a.padColour(i, 127, 127, 127)
			case i == t:
				r, g, b := scale7(trackColours[i], 1)
				a.padColour(i, r, g, b)
			default:
				r, g, b := scale7(trackColours[i], 0.18)
				a.padColour(i, r, g, b)
			}
			if a.proj.Tracks[i].Muted {
				a.padColour(8+i, 100, 0, 0)
			} else {
				a.padColour(8+i, 0, 45, 0)
			}
		}
	case modePatterns:
		blink := time.Now().UnixMilli()/250%2 == 0
		for i := 0; i < seq.Patterns; i++ {
			inChain := false
			for _, ch := range a.proj.Chain {
				inChain = inChain || ch == i
			}
			switch {
			case a.sq.playing && a.sq.posPat == i:
				a.padColour(i, 0, 127, 0)
			case i == a.proj.Sel && a.sq.playing && blink:
				a.padColour(i, 127, 70, 0) // queued: starts at the end of this pattern
			case i == a.proj.Sel:
				a.padColour(i, 110, 110, 110)
			case inChain:
				a.padColour(i, 40, 0, 60)
			case a.proj.Pat[i].HasNotes():
				a.padColour(i, 0, 20, 70)
			default:
				a.padColour(i, 3, 3, 3)
			}
		}
	case modeProject:
		for i := 0; i < 16; i++ {
			switch {
			case i+1 == a.sq.projSlot:
				a.padColour(i, 110, 110, 110)
			case a.sq.projNames[i] != "":
				a.padColour(i, 0, 70, 10)
			default:
				a.padColour(i, 5, 5, 5)
			}
		}
	}
}

func (a *app) onSeqPad(idx int, vel byte, on bool) {
	switch a.mode {
	case modeStep:
		a.onStepPad(idx, vel, on)
	case modeTracks:
		if !on {
			return
		}
		if idx < seq.Tracks {
			if idx != a.proj.Track {
				a.sq.tTrackSel = true
			}
			a.selectTrack(idx)
			a.say(fmt.Sprintf("TRACK %d: %s", idx+1, a.preset.Name))
		} else if a.shift {
			a.toggleSolo(idx - 8)
		} else {
			a.toggleMute(idx - 8)
		}
	case modePatterns:
		if on {
			a.onPatternPad(idx)
		}
	case modeProject:
		if on {
			a.onProjectPad(idx)
		}
	case modeFX:
		if !on {
			return
		}
		a.fxPad(idx)
	}
}

// fxPad: pads 1-8 pick the fader effect (tapping the picked, active one
// switches it off / on again); pads 9-16 switch effects 1-8 on/off directly.
// SHIFT + any pad = all effects off.
func (a *app) fxPad(idx int) {
	switch {
	case a.shift:
		a.resetFX()
	case idx >= 8:
		a.toggleFX(idx - 8)
	case idx == a.faderIdx && (a.fxOn(idx) || math.Abs(a.fxLast[idx]-faderTargets[idx].neutral) > 0.02):
		a.toggleFX(idx)
	default:
		a.selectFader(idx)
	}
}

func (a *app) onStepPad(idx int, vel byte, on bool) {
	p := a.pat()
	t := a.proj.Track
	s := a.sq.stepPage*16 + idx
	if s >= p.Len {
		if on {
			a.say(fmt.Sprintf("STEP %d IS PAST THE LENGTH (%d): TURN DATA", s+1, p.Len))
		}
		return
	}
	st := &p.S[t][s]
	if on {
		if a.sq.eraseHeld {
			*st = seq.NewStep(a.proj.Tracks[t].Note)
			a.sendStep(s)
			a.say(fmt.Sprintf("STEP %d CLEARED", s+1))
			a.paintPads()
			return
		}
		if a.sq.held != 0 {
			return // one step at a time
		}
		a.sq.held, a.sq.pressWasOn, a.sq.heldEdited = s+1, st.On, false
		a.sq.knobAcc = [3]float64{}
		if !st.On {
			st.On = true
			if len(st.Locks) == 0 {
				st.Note = a.proj.Tracks[t].Note
			}
			st.Vel = math.Max(0.3, float64(vel)/127)
			if st.Gate <= 0 {
				st.Gate = 0.5
			}
			a.sendStep(s)
			a.sq.tStepToggled = true
		}
		a.previewNote(t, st.Note, st.Vel)
	} else if a.sq.held == s+1 {
		if a.sq.pressWasOn && !a.sq.heldEdited {
			st.On = false
			a.sendStep(s)
			a.sq.tStepToggled = true
		}
		a.sq.held = 0
	}
	a.dirty = true
	a.paintPads()
}

func (a *app) onPatternPad(i int) {
	switch {
	case a.sq.eraseHeld:
		l := a.proj.Pat[i].Len
		a.proj.Pat[i].Clear()
		a.proj.Pat[i].Len = l
		a.dsp.lines(a.proj.PatternCmds(i))
		a.say(fmt.Sprintf("PATTERN %d CLEARED", i+1))
	case a.sq.chopHeld:
		a.proj.CopyPattern(a.proj.Sel, i)
		a.dsp.lines(a.proj.PatternCmds(i))
		a.say(fmt.Sprintf("PATTERN %d COPIED TO %d", a.proj.Sel+1, i+1))
	case a.shift:
		if len(a.proj.Chain) >= 8 {
			a.say("CHAIN IS FULL (8). ERASE + LOOP CLEARS IT")
			return
		}
		a.proj.Chain = append(a.proj.Chain, i)
		a.dsp.send(seq.ChainCmd(a.proj.Chain))
		a.say("CHAIN: " + chainText(a.proj.Chain))
	default:
		a.proj.Chain = nil
		a.proj.Sel = i
		a.followPage()
		a.dsp.lines([]string{"CHAIN", fmt.Sprintf("PAT %d", i)})
		a.sq.tPatSwitched = true
		if a.sq.playing {
			a.say(fmt.Sprintf("PATTERN %d NEXT (AT THE END OF THIS ONE)", i+1))
		} else {
			a.say(fmt.Sprintf("PATTERN %d", i+1))
		}
	}
	a.sq.projDirty = true
	a.dirty = true
	a.paintPads()
}

func chainText(c []int) string {
	var s []string
	for _, p := range c {
		s = append(s, strconv.Itoa(p+1))
	}
	return strings.Join(s, " > ")
}

func (a *app) onProjectPad(i int) {
	n := i + 1
	path := seq.SlotPath(a.projectsDir(), n)
	switch {
	case a.recHeld:
		if a.proj.Name == "" || a.proj.Name == "NEW PROJECT" || a.proj.Name == "STARTER GROOVES" {
			a.proj.Name = fmt.Sprintf("PROJECT %d", n)
		}
		if err := a.proj.Save(path); err != nil {
			a.say("SAVE FAILED")
			a.log("PROJECT_SAVE_FAILED slot=%d err=%v", n, err)
			return
		}
		a.sq.projSlot, a.sq.projNames[i] = n, a.proj.Name
		a.saveSession()
		a.say(fmt.Sprintf("PROJECT SAVED TO SLOT %d", n))
		a.log("PROJECT_SAVED slot=%d", n)
	case a.sq.eraseHeld:
		// new project: keep the 8 sounds and globals, clear every pattern
		a.proj.Save(filepath.Join(a.projectsDir(), "before_new.json"))
		np := seq.New()
		np.Tracks, np.G = a.proj.Tracks, a.proj.G
		a.setProject(np)
		a.sq.projSlot = 0
		a.say("NEW PROJECT (SAME SOUNDS). OLD ONE: PROJECTS/BEFORE_NEW")
	default:
		p, err := seq.Load(path)
		if err != nil {
			a.say(fmt.Sprintf("SLOT %d IS EMPTY", n))
			return
		}
		a.proj.Save(filepath.Join(a.projectsDir(), "before_load.json")) // safety copy
		a.setProject(p)
		a.sq.projSlot = n
		a.say(fmt.Sprintf("LOADED PROJECT %d: %s", n, p.Name))
		a.log("PROJECT_LOADED slot=%d", n)
	}
	a.sq.projDirty = true
	a.paintPads()
}

// ------------------------------------------------------------------ buttons

// seqHeldButton tracks the hold-modifier buttons (both edges).
func (a *app) seqHeldButton(note byte, down bool) bool {
	switch note {
	case controls.BtnErase:
		a.sq.eraseHeld = down
		if down && a.shift {
			a.eraseAllPress()
		}
	case controls.BtnMute:
		a.sq.muteHeld = down
		if down {
			a.say("MUTE: PADS 1-8 MUTE / UNMUTE TRACKS")
		}
	case controls.BtnChop:
		a.sq.chopHeld = down
	case controls.BtnKnobFX:
		// hold + pad 1-8 = pick a fader effect, + pad 9-16 = switch one on/off;
		// a plain tap opens the FX view; SHIFT + KNOB FX = all effects off
		if down && a.shift {
			a.resetFX()
			a.sq.fxUsed, a.sq.fxHeld = true, true
			return true
		}
		if down {
			a.sq.fxHeld, a.sq.fxUsed = true, false
		} else {
			a.sq.fxHeld = false
			if !a.sq.fxUsed && a.mode != modeConfirmExit {
				a.toggleMode(modeFX)
			}
		}
		a.paintPads()
	case controls.BtnLoop:
		// hold + pads = live length (the sheet appears after a short hold, so
		// nothing flashes); SHIFT + LOOP = patterns view; ERASE + LOOP clears
		// the chain. A plain tap only shows how it works.
		switch {
		case down && a.sq.eraseHeld:
			a.proj.Chain = nil
			a.dsp.send("CHAIN")
			a.say("CHAIN CLEARED")
		case down && a.shift:
			if a.mode != modeConfirmExit {
				a.toggleMode(modePatterns)
			}
		case down:
			a.sq.loopHeld, a.sq.loopUsed, a.sq.loopAt, a.sq.loopPainted = true, false, time.Now(), false
			a.animate(a.sq.loopAt.Add(loopDelay + 20*time.Millisecond))
		case a.sq.loopHeld:
			if !a.sq.loopUsed && time.Since(a.sq.loopAt) < loopDelay*2 {
				a.say("HOLD LOOP + PAD: LOOP LENGTH  ·  SHIFT + LOOP: PATTERNS")
			}
			a.sq.loopHeld = false
		}
		a.paintPads()
	default:
		return false
	}
	a.dirty = true
	return true
}

func (a *app) toggleMode(m mode) {
	if a.mode == m {
		a.setMode(modePlay)
	} else {
		a.setMode(m)
	}
}

func (a *app) onSeqButton(note byte) bool {
	switch note {
	case controls.BtnPlay:
		a.dsp.send("PLAY")
		a.sq.playing, a.sq.posPat = true, -1
		a.sq.tPlayed = true
		if a.sq.recArm {
			a.say("PLAY + RECORD: PADS AND KNOBS GET RECORDED")
		} else {
			a.say("PLAY")
		}
	case controls.BtnStop:
		if a.shift {
			return false // SHIFT + STOP = Exit to Stock
		}
		if a.sq.playing {
			// first STOP: the sequencer stops, echoes and decays ring out
			a.dsp.send("STOP")
			a.sq.playing, a.sq.posPat, a.sq.recVal = false, -1, nil
			a.followPage()
			a.say("STOPPED (STOP AGAIN: ALL SOUND OFF)")
			a.paintPads()
		} else {
			a.hardStop() // second STOP: silence now
		}
	case controls.BtnSeqRecord:
		a.sq.recArm = !a.sq.recArm
		a.sq.recVal = nil
		switch {
		case a.sq.recArm && a.sq.playing:
			a.say("RECORDING INTO TRACK " + strconv.Itoa(a.proj.Track+1))
		case a.sq.recArm:
			a.say("RECORD ARMED: PRESS PLAY")
		default:
			a.say("RECORD OFF")
		}
	case controls.BtnSeq:
		a.toggleMode(modeStep)
	case controls.BtnSample:
		a.toggleMode(modeTracks)
	case controls.Btn16Levels: // the visualizer: the play view's other face
		if a.mode == modePlay {
			a.ui.scope = (a.ui.scope + 1) % 4 // pages -> spectrum -> flow -> dots -> pages
		} else {
			a.setMode(modePlay)
			a.ui.scope = 1
		}
		a.paintPads()
		switch a.ui.scope {
		case 1:
			a.say("SPECTRUM  ·  16 LEVELS: FLOW  ·  KNOBS STILL WORK")
		case 2:
			a.say("FLOW  ·  16 LEVELS: DOTS")
		case 3:
			a.say("DOTS  ·  16 LEVELS: BACK TO THE PAGES")
		}
	case controls.BtnSampleSelect:
		switch {
		case a.shift:
			a.toggleMode(modeProject)
		case a.mode != modePlay && a.mode != modePreset:
			a.setMode(modePreset)
			a.say("SOUNDS FOR TRACK " + strconv.Itoa(a.proj.Track+1))
		default:
			return false
		}
	case controls.BtnPadBank:
		if a.mode != modeStep {
			return false
		}
		a.sq.held = 0
		if a.shift {
			a.sq.noFollow = !a.sq.noFollow
			if a.sq.noFollow {
				a.say("PADS STAY ON THIS BAR")
			} else {
				a.followPage()
				a.say("PADS FOLLOW THE PLAYHEAD")
			}
		} else {
			a.sq.stepPage = (a.sq.stepPage + 1) % bars(a.pat().Len)
			a.sq.noFollow = true
			a.say(fmt.Sprintf("BAR %d (STEPS %d-%d). SHIFT+PAD BANK: FOLLOW AGAIN", a.sq.stepPage+1, a.sq.stepPage*16+1, a.sq.stepPage*16+16))
		}
		a.paintPads()
	case controls.BtnMinus, controls.BtnPlus:
		if a.mode != modeStep || a.sq.held == 0 {
			return false
		}
		st := &a.pat().S[a.proj.Track][a.sq.held-1]
		d := 12
		if note == controls.BtnMinus {
			d = -12
		}
		st.Note = max(0, min(127, st.Note+d))
		a.sq.heldEdited = true
		a.sendStep(a.sq.held - 1)
		a.previewNote(a.proj.Track, st.Note, st.Vel)
	default:
		return false
	}
	a.dirty = true
	return true
}

// hardStop: STOP means silence. The sequencer stops and the engine fades
// everything (notes, echoes, reverb, stutter) out in 30 ms, without a click.
func (a *app) hardStop() {
	a.dsp.lines([]string{"STOP", "KILL"})
	for idx := range a.held {
		delete(a.held, idx)
	}
	a.sq.playing, a.sq.posPat, a.sq.recVal = false, -1, nil
	a.followPage()
	a.say("STOP: ALL SOUND OFF")
	a.paintPads()
}

// seqData handles the DATA encoder in step mode: length, or micro timing of
// the held step. Returns false when DATA should do its normal job.
func (a *app) seqData(d2 byte) bool {
	if a.mode == modeTracks { // DATA flips between the notes and the mixer
		a.turnPage(1)
		return true
	}
	if a.mode != modeStep {
		return false
	}
	dir := 0
	if d2 == 1 {
		dir = 1
	} else if d2 == 127 {
		dir = -1
	}
	p := a.pat()
	if a.sq.held > 0 {
		st := &p.S[a.proj.Track][a.sq.held-1]
		st.Micro = math.Max(-0.45, math.Min(0.45, math.Round((st.Micro+float64(dir)*0.05)*100)/100))
		a.sq.heldEdited, a.sq.tStepEdit = true, true
		a.sendStep(a.sq.held - 1)
		a.say(fmt.Sprintf("STEP %d TIMING %+d%%", a.sq.held, int(math.Round(st.Micro*100))))
		return true
	}
	p.Len = max(1, min(seq.Steps, p.Len+dir))
	a.sq.stepPage = min(a.sq.stepPage, bars(p.Len)-1)
	a.dsp.send("LEN %d %d", a.sq.viewPat, p.Len)
	a.sq.projDirty = true
	a.say(fmt.Sprintf("PATTERN %d LENGTH %d STEPS", a.sq.viewPat+1, p.Len))
	a.paintPads()
	return true
}

// stepKnob: K1-K3 while holding a step = note / velocity / length;
// with SHIFT = lock the current page's K1-K3 parameter on that step.
func (a *app) stepKnob(k, delta, prev int) {
	s := a.sq.held - 1
	st := &a.pat().S[a.proj.Track][s]
	a.sq.heldEdited = true
	if a.shift {
		pr := &pages[a.pageIdx].p[k]
		if seq.IsGlobal(pr.key) || isChar(pr.key) {
			a.say(pr.label + " IS SHARED BY ALL TRACKS: NO LOCK")
			return
		}
		cur, ok := st.LockValue(pr.key)
		if !ok {
			cur = a.preset.P[pr.key]
		}
		n := pr.norm(cur)
		if delta > 0 {
			n += (1 - n) * float64(delta) / float64(max(1, 127-prev))
		} else {
			n -= n * float64(-delta) / float64(max(1, prev))
		}
		v := pr.denorm(n)
		st.SetLock(pr.key, v)
		a.sq.tLocked = true
		a.sendStep(s)
		a.say(fmt.Sprintf("STEP %d LOCK %s %s", s+1, pr.label, pr.show(v)))
		return
	}
	switch k {
	case 0:
		a.sq.knobAcc[0] += float64(delta) * 0.5
		if d := int(a.sq.knobAcc[0]); d != 0 {
			a.sq.knobAcc[0] -= float64(d)
			st.Note = max(0, min(127, st.Note+d))
			a.previewNote(a.proj.Track, st.Note, st.Vel)
		}
	case 1:
		st.Vel = math.Max(0.05, math.Min(1, st.Vel+float64(delta)/127))
	case 2:
		a.sq.knobAcc[2] += float64(delta) / 6
		if d := int(a.sq.knobAcc[2]); d != 0 {
			a.sq.knobAcc[2] -= float64(d)
			gi := 0
			for i, g := range gateChoices {
				if math.Abs(g-st.Gate) < math.Abs(gateChoices[gi]-st.Gate) {
					gi = i
				}
			}
			st.Gate = gateChoices[max(0, min(len(gateChoices)-1, gi+d))]
		}
	}
	a.sq.tStepEdit = true
	a.sendStep(s)
}

// ------------------------------------------------------------------ tick

// seqTick runs every UI loop: engine events, previews, pad animation and
// the project autosave.
func (a *app) seqTick() {
	if a.dsp != nil && a.dsp.ev != nil {
	drain:
		for {
			select {
			case l := <-a.dsp.ev:
				a.onEngineLine(l)
			default:
				break drain
			}
		}
	}
	now := time.Now()
	kept := a.sq.previews[:0]
	for _, p := range a.sq.previews {
		if now.After(p.at) {
			a.dsp.send("O %d %d", p.note, p.track)
		} else {
			kept = append(kept, p)
		}
	}
	a.sq.previews = kept
	if (a.mode == modePatterns || a.mode == modeTracks || a.mode == modeFX) && now.Sub(a.sq.lastPaint) > 100*time.Millisecond {
		a.paintPads()
	}
	if a.loopShown() && !a.sq.loopPainted {
		a.paintPads()
		a.dirty = true
	}
	if a.sq.projDirty && now.Sub(a.sq.lastSave) > 30*time.Second {
		a.sq.lastSave = now
		a.saveSession()
	}
}

func (a *app) onEngineLine(l string) {
	if strings.HasPrefix(l, "POS ") {
		var p, s int
		if _, err := fmt.Sscanf(l, "POS %d %d", &p, &s); err != nil {
			return
		}
		if p < 0 {
			a.sq.playing, a.sq.posPat = false, -1
			a.followPage()
		} else {
			a.sq.playing, a.sq.posPat, a.sq.posStep = true, p, s
			a.followPage()
			now := time.Now()
			for t := 0; t < seq.Tracks; t++ {
				if st := &a.proj.Pat[p].S[t][s]; st.On {
					a.sq.flash[t] = now
					if !a.proj.Tracks[t].Muted {
						a.addHit(t, st.Note, st.Vel, s, a.proj.Pat[p].Len)
					}
				}
			}
		}
		a.dirty = true
		if a.mode == modeStep || a.mode == modeTracks || a.mode == modePatterns {
			a.paintPads()
		}
		return
	}
	if _, _, _, ok := a.proj.ApplyReply(l); ok {
		a.sq.projDirty = true
		if strings.HasPrefix(l, "REC ") {
			a.sq.tRecorded = true
		}
		a.dirty = true
	}
}

// ------------------------------------------------------------------ screen

func (a *app) drawSeqStatus(y int) {
	s := a.scr
	x := 8
	put := func(c uint32, t string) {
		s.Text(x, y, 1, c, t)
		x += 6 * (len(t) + 2)
	}
	put(cWhite, fmt.Sprintf("PAT %02d", a.proj.Sel+1))
	if a.sq.playing && a.sq.posPat >= 0 {
		put(cGreen, fmt.Sprintf("> P%d BAR %d.%d", a.sq.posPat+1, a.sq.posStep/16+1, a.sq.posStep%16/4+1))
	} else {
		put(cGrey, "STOPPED")
	}
	put(cGrey, fmt.Sprintf("%d BPM", int(math.Round(a.proj.G["tempo"]))))
	if len(a.proj.Chain) > 0 {
		put(cAmber, "CHAIN")
	}
	if a.sq.recArm {
		put(0x00FF3030, "REC")
	}
	put(cGrey, fmt.Sprintf("OCT %d", a.octave))
}

// drawStrip: the selected track's steps in the selected pattern, one row.
func (a *app) drawStrip(y int) {
	s := a.scr
	p := a.pat()
	t := a.proj.Track
	col := rgb7(trackColours[t])
	tails := noteTails(p, t)
	w := min(9, 300/p.Len) // cell width: 32 steps fill the line, 64 halve
	for i := 0; i < p.Len; i++ {
		x := 6 + i*w + i/16*2 // small gap between bars
		c := uint32(0x00302820)
		if tails[i] > 0 {
			c = dim(trackColours[t], 0.4)
		}
		if p.S[t][i].On {
			c = col
		}
		if a.sq.playing && a.sq.posPat == a.sq.viewPat && a.sq.posStep == i {
			c = cWhite
		}
		s.Rect(x, y, max(2, w-1), 7, c)
	}
}

func (a *app) drawSeq() {
	s := a.scr
	tourOn := !a.tourDone && a.tourIdx < len(a.tour)
	switch a.mode {
	case modeStep:
		p := a.pat()
		t := a.proj.Track
		follow := "FOLLOW"
		if a.sq.noFollow {
			follow = "HOLD"
		}
		s.Text(6, 38, 2, cGreen, fmt.Sprintf("STEPS P%d LEN %d", a.sq.viewPat+1, p.Len))
		s.Text(250, 38, 1, cAmber, fmt.Sprintf("BAR %d/%d", a.sq.stepPage+1, bars(p.Len)))
		s.Text(250, 48, 1, cGrey, follow)
		// one row per bar (16 steps); the row the pads show is framed
		col := rgb7(trackColours[t])
		tailCol := dim(trackColours[t], 0.55)
		tails := noteTails(p, t)
		rowY := func(r int) int { return 60 + r*15 }
		for r := 0; r < bars(p.Len); r++ {
			if r == a.sq.stepPage {
				s.Rect(3, rowY(r)-2, 309, 17, 0x00604020)
			}
		}
		for i := 0; i < p.Len; i++ {
			row, c := i/16, i%16
			x, y := 6+c*19, rowY(row)
			st := &p.S[t][i]
			bg := uint32(0x00282018)
			if i%4 == 0 {
				bg = 0x003A3024
			}
			s.Rect(x, y, 17, 13, bg)
			if tails[i] > 0 { // long note ringing through: a bar joined to the note
				s.Rect(x-2, y+5, 2+int(17*tails[i]), 4, tailCol)
			}
			if st.On {
				// width = note length (a full block = 1 step), height = velocity,
				// shifted by its timing nudge
				h := int(3 + 10*st.Vel)
				w := max(3, int(17*math.Min(1, st.Gate)))
				off := int(st.Micro * 19)
				s.Rect(x+off, y+13-h, w, h, col)
				if st.Gate > 1 {
					s.Rect(x+off+17, y+5, 2, 4, tailCol)
				}
			}
			if len(st.Locks) > 0 {
				s.Rect(x+6, y+1, 5, 3, 0x00C060FF)
			}
			if a.sq.held == i+1 {
				s.Rect(x, y+11, 17, 2, cOrange)
			}
			if a.sq.playing && a.sq.posPat == a.sq.viewPat && a.sq.posStep == i {
				s.Rect(x, y, 17, 3, cWhite)
			}
		}
		if a.sq.held > 0 {
			st := &p.S[t][a.sq.held-1]
			s.Text(6, 124, 1, cWhite, fmt.Sprintf("STEP %d  NOTE %s  VEL %d  %s  TIMING %+d%%",
				a.sq.held, noteName(st.Note), int(st.Vel*99+0.5), gateName(st.Gate), int(math.Round(st.Micro*100))))
			var ls []string
			for _, l := range st.Locks {
				ls = append(ls, fmt.Sprintf("%s %s", strings.ToUpper(strings.ReplaceAll(l.Key, "_", " ")), lockShow(l)))
			}
			if len(ls) == 0 {
				ls = []string{"NONE"}
			}
			s.Text(6, 136, 1, cOrange, "LOCKS: "+strings.Join(ls, ", "))
			if !tourOn {
				s.Text(6, 148, 1, cGrey, fmt.Sprintf("K1 NOTE K2 VEL K3 LENGTH  DATA TIMING  +/- OCTAVE"))
				pg := &pages[a.pageIdx]
				s.Text(6, 160, 1, cAmber, fmt.Sprintf("SHIFT+K: LOCK %s: %s / %s / %s", pg.name, pg.p[0].label, pg.p[1].label, pg.p[2].label))
				s.Text(6, 172, 1, cGrey, "SCREEN BUTTONS: OTHER PAGES TO LOCK")
			}
		} else if !tourOn {
			s.Text(6, 124, 1, cGrey, "TAP PAD: STEP ON/OFF   HOLD PAD + K1-K3: EDIT")
			s.Text(6, 136, 1, cGrey, "HOLD PAD + SHIFT + K: LOCK A SOUND SETTING")
			s.Text(6, 148, 1, cGrey, "DATA: LENGTH  PAD BANK: NEXT BAR (SHIFT: FOLLOW)")
			s.Text(6, 160, 1, cGrey, "ERASE + PAD: CLEAR STEP   SEQ: BACK")
		}
	case modeTracks:
		s.Text(6, 40, 1, cGreen, fmt.Sprintf("TRACKS  PATTERN %d   PADS 1-8 SELECT, 9-16 MUTE", a.sq.viewPat+1))
		p := a.pat()
		for t := 0; t < seq.Tracks; t++ {
			y := 52 + t*12
			tr := &a.proj.Tracks[t]
			if t == a.proj.Track {
				s.Rect(0, y-1, 320, 11, 0x00302418)
			}
			name := tr.Sound.Name
			if len(name) > 7 {
				name = name[:7]
			}
			lc := rgb7(trackColours[t])
			if tr.Muted {
				lc = 0x00605048
			}
			s.Text(4, y+1, 1, lc, fmt.Sprintf("%d %-7s", t+1, name))
			if tr.Muted {
				s.Text(62, y+1, 1, 0x00FF3030, "M")
			}
			w := min(7, 244/p.Len)
			tails := noteTails(p, t)
			for i := 0; i < p.Len; i++ {
				x := 72 + i*w + i/16
				c := uint32(0x00282018)
				if tails[i] > 0 && !tr.Muted {
					c = dim(trackColours[t], 0.35)
				}
				if p.S[t][i].On {
					c = rgb7(trackColours[t])
					if tr.Muted {
						c = 0x00504038
					}
				}
				if a.sq.playing && a.sq.posPat == a.sq.viewPat && a.sq.posStep == i {
					c = cWhite
				}
				s.Rect(x, y, max(2, w-1), 9, c)
			}
		}
	case modePatterns:
		s.Text(6, 34, 1, cGreen, "PATTERNS  TAP: PLAY IT  SHIFT+PADS: CHAIN")
		a.drawBoxes(func(i int) (string, string, uint32) {
			p := &a.proj.Pat[i]
			c := uint32(0x00201810)
			switch {
			case a.sq.playing && a.sq.posPat == i:
				c = 0x00207020
			case i == a.proj.Sel:
				c = 0x00605850
			case p.HasNotes():
				c = 0x00183050
			}
			return fmt.Sprintf("%d", i+1), fmt.Sprintf("%d ST", p.Len), c
		})
		if len(a.proj.Chain) > 0 {
			s.Text(6, 152, 1, cAmber, "CHAIN: "+chainText(a.proj.Chain)+"  (ERASE+LOOP CLEARS)")
		} else if !tourOn {
			s.Text(6, 152, 1, cGrey, "ERASE+PAD: CLEAR  CHOP+PAD: COPY HERE")
		}
	case modeFX:
		s.Text(6, 34, 1, cGreen, "PADS 1-8 PICK   9-16 ON/OFF   SHIFT+PAD ALL OFF")
		fader := -1.0 // where the physical fader is (shown on the picked effect)
		if a.ccKnown[controls.CCFader] {
			fader = float64(a.lastCC[controls.CCFader]) / 127
		}
		for i := range faderTargets {
			col, row := i%4, 1-i/4 // laid out like pads 1-8 (bottom two rows)
			x, y := 6+col*78, 46+row*52
			ft := faderTargets[i]
			bg := uint32(0x00201810)
			if i == a.faderIdx {
				bg = 0x00504438
			}
			s.Rect(x, y, 74, 49, bg)
			s.Rect(x, y, 74, 10, rgb7(ft.col))
			s.Text(x+3, y+2, 1, 0x00100C08, fmt.Sprintf("PAD %d + %d", i+1, i+9))
			s.Text(x+4, y+13, 1, cWhite, ft.label)
			// the bar is the fader's travel (bottom = left, top = right); the
			// white tick is where the effect is OFF, the colour runs from there
			// to where the effect is now
			bx, bw := x+4, 66
			v := a.fxNow[i]
			s.Rect(bx, y+24, bw, 7, 0x00100C08)
			n := int(float64(bw) * ft.neutral)
			c := int(float64(bw) * v)
			s.Rect(bx+min(n, c), y+24, max(1, abs(c-n)), 7, rgb7(ft.col))
			s.Rect(bx+min(n, bw-1), y+22, 1, 11, cWhite)
			if i == a.faderIdx && fader >= 0 && a.faderVal < 0 {
				s.Rect(bx+int(float64(bw-1)*fader), y+33, 1, 3, cGrey) // fader, not taken over yet
			}
			s.Text(x+4, y+34, 2-boolInt(ft.key == "fx_pan"), cWhite, fxAmount(i, v))
			if a.fxOn(i) {
				s.Rect(x+66, y+13, 5, 5, cGreen) // switched on
			}
		}
		if !tourOn {
			s.Text(6, 152, 1, cGrey, "ANY VIEW: HOLD KNOB FX + PAD. SHIFT+KNOB FX: ALL OFF")
		}
	case modeProject:
		s.Text(6, 34, 1, cGreen, "PROJECTS  PAD: LOAD  HOLD SAMPLE RECORD+PAD: SAVE")
		a.drawBoxes(func(i int) (string, string, uint32) {
			c := uint32(0x00201810)
			if a.sq.projNames[i] != "" {
				c = 0x00184020
			}
			if i+1 == a.sq.projSlot {
				c = 0x00605850
			}
			name := a.sq.projNames[i]
			if len(name) > 10 {
				name = name[:10]
			}
			return fmt.Sprintf("%d", i+1), name, c
		})
		if a.recHeld {
			s.Text(6, 152, 2, cOrange, "SAVE: PICK A PAD")
		} else if !tourOn {
			s.Text(6, 152, 1, cGrey, "ERASE+PAD: NEW PROJECT WITH THESE SOUNDS")
		}
	}
}

func lockShow(l seq.Lock) string {
	for i := range pages {
		for k := 0; k < 3; k++ {
			if pages[i].p[k].key == l.Key {
				return pages[i].p[k].show(l.V)
			}
		}
	}
	return pct(l.V)
}

// drawBoxes draws 16 boxes laid out like the pads (pad 1 bottom-left).
func (a *app) drawBoxes(f func(i int) (string, string, uint32)) {
	s := a.scr
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		x, y := 6+col*78, 44+row*26
		big, small, c := f(i)
		s.Rect(x, y, 74, 24, c)
		s.Text(x+4, y+5, 2, cWhite, big)
		s.Text(x+34, y+9, 1, cGrey, small)
	}
}

// ------------------------------------------------------------------ tour

// lengthPads: hold LOOP + pad 1-7 = these lengths (16 steps = 1 bar);
// pad 15 / 16 = one step less / more.
var lengthPads = []int{1, 2, 4, 8, 16, 32, 64}

// livePattern is the pattern a live length change applies to: the one
// playing, or the selected one when stopped.
func (a *app) livePattern() int {
	if a.sq.playing && a.sq.posPat >= 0 {
		return a.sq.posPat
	}
	return a.proj.Sel
}

func (a *app) setLength(p, n int) {
	n = max(1, min(seq.Steps, n))
	a.proj.Pat[p].Len = n
	if p == a.sq.viewPat {
		a.sq.stepPage = min(a.sq.stepPage, bars(n)-1)
	}
	a.dsp.send("LEN %d %d", p, n)
	a.sq.projDirty, a.sq.lenJumped = true, true
	a.say(fmt.Sprintf("PATTERN %d LENGTH %d", p+1, n))
}

// quickPad handles the hold-a-button pad shortcuts that work in every view.
func (a *app) quickPad(idx int, on bool) bool {
	switch {
	case a.sq.loopHeld:
		if on {
			a.sq.loopUsed = true
			p := a.livePattern()
			switch {
			case idx < len(lengthPads):
				a.setLength(p, lengthPads[idx])
			case idx == 14:
				a.setLength(p, a.proj.Pat[p].Len-1)
			case idx == 15:
				a.setLength(p, a.proj.Pat[p].Len+1)
			}
			a.paintPads()
		}
		return true
	case a.sq.fxHeld:
		if on {
			a.sq.fxUsed = true
			a.fxPad(idx)
		}
		return true
	}
	return false
}

func (a *app) paintLengthPads() {
	l := a.proj.Pat[a.livePattern()].Len
	for i := 0; i < 16; i++ {
		switch {
		case i < len(lengthPads) && lengthPads[i] == l:
			a.padColour(i, 127, 127, 127)
		case i < len(lengthPads):
			a.padColour(i, 0, byte(20+i*16), 40)
		case i == 14 || i == 15:
			a.padColour(i, 60, 30, 0)
		default:
			a.padColour(i, 0, 0, 0)
		}
	}
}

func (a *app) paintFXPads() {
	for i := 0; i < 16; i++ {
		switch {
		case i < 8: // pick
			c := faderTargets[i].col
			f := 0.18
			if a.fxOn(i) {
				f = 0.6 // effect is active
			}
			if i == a.faderIdx {
				f = 1
			}
			r, g, b := scale7(c, f)
			a.padColour(i, r, g, b)
		default: // on/off switch for effect i-8: lit = on
			c := faderTargets[i-8].col
			f := 0.06
			if a.fxOn(i - 8) {
				f = 1
			}
			r, g, b := scale7(c, f)
			a.padColour(i, r, g, b)
		}
	}
}

func (a *app) drawLengthOverlay() {
	s := a.scr
	p := a.livePattern()
	s.Rect(0, 40, 320, 160, cBg)
	s.Text(6, 44, 2, cGreen, fmt.Sprintf("LENGTH  PATTERN %d", p+1))
	s.Text(110, 70, 6, cWhite, fmt.Sprintf("%d", a.proj.Pat[p].Len))
	s.Text(6, 124, 1, cAmber, "HOLD LOOP + PAD:")
	s.Text(6, 138, 1, cWhite, "1=1 2=2 3=4 4=8 STEPS  5=1 BAR 6=2 7=4 BARS")
	s.Text(6, 152, 1, cWhite, "PAD 15 = ONE STEP LESS   PAD 16 = ONE MORE")
	s.Text(6, 168, 1, cGrey, "STAYS ON THE BEAT.")
}

var seqTour = []tourStep{
	{"PRESS PLAY: THREE STARTER\nGROOVES PLAY IN A CHAIN.", func(a *app) bool { return a.sq.tPlayed }},
	{"PRESS SAMPLE: ALL 8 TRACKS.\nPADS 9-16 MUTE. TRY ONE.", func(a *app) bool { return a.sq.tMuted }},
	{"PADS 1-8 PICK A TRACK.\nPICK TRACK 5, THE BASS.", func(a *app) bool { return a.sq.tTrackSel }},
	{"PRESS SEQ: PADS ARE STEPS.\nTAP PADS: STEP ON / OFF.", func(a *app) bool { return a.sq.tStepToggled }},
	{"HOLD A LIT PAD + TURN K1:\nNOTE. K2 VEL, K3 LENGTH.", func(a *app) bool { return a.sq.tStepEdit }},
	{"HOLD PAD + SHIFT + TURN K:\nLOCK A SOUND ON THAT STEP.", func(a *app) bool { return a.sq.tLocked }},
	{"SHIFT + LOOP: PATTERNS.\nTAP A PAD TO SWITCH.", func(a *app) bool { return a.sq.tPatSwitched }},
	{"BACK TO PLAY VIEW, SEQ REC\nON, PLAY PADS: IT RECORDS.", func(a *app) bool { return a.sq.tRecorded }},
}

// resetPress: SHIFT + DATA push. In the step view with a step held, it
// clears that step's locks. Otherwise it resets the knob you touched last to
// its neutral value (EQ flat, sends off, pan centre...) or, for sound
// settings without one, to the value saved in the sound. Pressed twice
// quickly, it resets all three lines of the page.
func (a *app) resetPress() {
	if a.mode == modeStep && a.sq.held > 0 {
		st := &a.pat().S[a.proj.Track][a.sq.held-1]
		st.Locks = nil
		a.sq.heldEdited = true
		a.sendStep(a.sq.held - 1)
		a.say(fmt.Sprintf("STEP %d: LOCKS CLEARED", a.sq.held))
		return
	}
	if a.mode != modePlay && a.mode != modeStep {
		return
	}
	now := time.Now()
	pg := &pages[a.pageIdx]
	if now.Sub(a.sq.lastResetPress) < 600*time.Millisecond {
		for k := 0; k < 3; k++ {
			a.resetKeyTo(pg.p[k].key)
		}
		a.sq.resetKey = ""
		a.say(pg.name + ": ALL THREE RESET")
		a.sq.lastResetPress = time.Time{}
		return
	}
	a.sq.lastResetPress = now
	key := a.lastKey
	onPage := false
	for k := 0; k < 3; k++ {
		onPage = onPage || pg.p[k].key == key || (pg.alt[k] != nil && pg.alt[k].key == key)
	}
	if !onPage {
		key = pg.p[0].key // nothing touched on this page yet: K1
	}
	v := a.resetKeyTo(key)
	a.sq.resetKey, a.sq.resetAt, a.sq.didReset = key, now, true
	a.touched(key)
	a.say(fmt.Sprintf("RESET %s: %s (TWICE: WHOLE PAGE)", labelOf(key), showOf(key, v)))
}

// neutralValues: where a parameter does nothing / sits flat.
var neutralValues = map[string]float64{
	"eq_low": 0.5, "eq_mid": 0.5, "eq_high": 0.5, "eq_mid_freq": 0.5, "pan": 0.5,
	"eq_low_freq": 0.574, "eq_high_freq": 0.5283,
	"delay_send": 0, "reverb_send": 0, "sub_level": 0, "wobble_depth": 0, "drive": 0,
	"resonance": 0, "cutoff": 1, "swing": 0, "rec_quant": 1,
}

func (a *app) resetKeyTo(key string) float64 {
	var v float64
	switch {
	case isChar(key):
		v = charDefaults[key]
	case neutralValues[key] != 0 || hasKey(neutralValues, key):
		v = neutralValues[key]
	case seq.IsGlobal(key):
		v = seq.GlobalDefaults[key]
	default: // back to the sound as saved
		p, _ := sound.LoadSlot(a.soundsDir(), a.slot)
		v = p.P[key]
	}
	if key == "engine" {
		a.allNotesOff()
	}
	a.setVal(key, v)
	return v
}

func hasKey(m map[string]float64, k string) bool { _, ok := m[k]; return ok }

func labelOf(key string) string {
	for i := range pages {
		for k := 0; k < 3; k++ {
			if pages[i].p[k].key == key {
				return pages[i].p[k].label
			}
		}
	}
	return strings.ToUpper(key)
}

func showOf(key string, v float64) string {
	for i := range pages {
		for k := 0; k < 3; k++ {
			if pages[i].p[k].key == key {
				return pages[i].p[k].show(v)
			}
		}
	}
	return pct(v)
}

// liveTour: the 0.7.3 additions, for players who finished seqTour.
var liveTour = []tourStep{
	{"HOLD KNOB FX + PAD 1-8:\nPICK A FADER EFFECT.", func(a *app) bool { return a.sq.fxPicked && a.faderMoved }},
	{"PLAY, THEN HOLD LOOP +\nPADS 1-6: LENGTH 1 TO 32.", func(a *app) bool { return a.sq.lenJumped && a.sq.playing }},
}

// eraseAllPress: SHIFT + ERASE clears in two presses, and a third press
// soon after brings it back. In the step view it clears the selected track
// in the pattern on screen; everywhere else, every track of the pattern
// playing (or selected).
func (a *app) eraseAllPress() {
	now := time.Now()
	if a.sq.eraseUndo != nil && now.Sub(a.sq.eraseUndoAt) < 5*time.Second {
		i := a.sq.eraseUndoPat
		a.proj.Pat[i] = *a.sq.eraseUndo
		a.dsp.lines(a.proj.PatternCmds(i))
		a.sq.eraseUndo, a.sq.projDirty = nil, true
		a.say(fmt.Sprintf("UNDONE: PATTERN %d IS BACK", i+1))
		a.log("ERASE_UNDO pat=%d", i)
		a.paintPads()
		return
	}
	trackOnly := a.mode == modeStep
	i := a.livePattern()
	if trackOnly {
		i = a.sq.viewPat
	}
	t := a.proj.Track
	what := fmt.Sprintf("ALL TRACKS IN PATTERN %d", i+1)
	if trackOnly {
		what = fmt.Sprintf("TRACK %d IN PATTERN %d", t+1, i+1)
	}
	if now.Sub(a.sq.eraseArmAt) > 2*time.Second {
		a.sq.eraseArmAt = now
		a.say("SHIFT + ERASE AGAIN: CLEAR " + what)
		return
	}
	a.sq.eraseArmAt = time.Time{}
	backup := a.proj.Pat[i]
	p := &a.proj.Pat[i]
	for tr := 0; tr < seq.Tracks; tr++ {
		if trackOnly && tr != t {
			continue
		}
		for st := range p.S[tr] {
			p.S[tr][st] = seq.NewStep(a.proj.Tracks[tr].Note)
		}
	}
	a.dsp.lines(a.proj.PatternCmds(i))
	a.sq.eraseUndo, a.sq.eraseUndoAt, a.sq.eraseUndoPat = &backup, now, i
	a.sq.projDirty = true
	a.say("CLEARED " + what + "  ·  SHIFT + ERASE: UNDO")
	a.log("ERASE_ALL pat=%d track_only=%t track=%d", i, trackOnly, t)
	a.paintPads()
}

const loopDelay = 180 * time.Millisecond

// loopShown: the length sheet and length pads appear once LOOP has been held
// a moment (or a length pad was already used), so a tap never flashes them.
func (a *app) loopShown() bool {
	return a.sq.loopHeld && (a.sq.loopUsed || time.Since(a.sq.loopAt) >= loopDelay)
}

// flipFace: the middle screen button shows the other face of the view:
// a page and its visualizer, the tracks' notes and the mixer, a followed or
// held bar in the step view; elsewhere it goes back to the play view.
func (a *app) flipFace() {
	switch a.mode {
	case modePlay:
		if a.ui.scope == 0 {
			a.ui.scope = a.ui.lastScope
			if a.ui.scope == 0 {
				a.ui.scope = 1
			}
		} else {
			a.ui.lastScope, a.ui.scope = a.ui.scope, 0
		}
	case modeTracks:
		a.ui.mixer = !a.ui.mixer
	case modeStep:
		a.sq.noFollow = !a.sq.noFollow
		if a.sq.noFollow {
			a.say("HOLDING THIS BAR  ·  MIDDLE BUTTON: FOLLOW")
		} else {
			a.say("FOLLOWING THE PLAYHEAD")
			a.followPage()
		}
	default:
		a.setMode(modePlay)
	}
	a.paintPads()
	a.dirty = true
}
