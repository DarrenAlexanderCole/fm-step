package main

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"fmstep/internal/controls"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

type nopWC struct{ io.Writer }

func (nopWC) Close() error { return nil }

func testApp() *app {
	a := &app{held: map[int]int{}, dsp: &dsp{in: nopWC{io.Discard}}}
	a.proj = seq.Default()
	a.proj.Tracks[0].Sound = sound.Factory()[0]
	a.preset = &a.proj.Tracks[0].Sound
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	return a
}

func TestKnobScalingCoversFullRange(t *testing.T) {
	a := testApp()
	a.pageIdx = pageIndex("SOUND") // K1 = harmonics
	a.preset.P["harmonics"] = 0.5
	a.onKnob(0, 64) // first report only sets the position
	a.onKnob(0, 127)
	if v := a.preset.P["harmonics"]; v < 0.999 {
		t.Fatalf("full turn up reached %v, want 1", v)
	}
	a.onKnob(0, 0)
	if v := a.preset.P["harmonics"]; v > 0.001 {
		t.Fatalf("full turn down reached %v, want 0", v)
	}
	// small turn back up moves proportionally to the remaining travel
	a.onKnob(0, 64)
	if v := a.preset.P["harmonics"]; v < 0.45 || v > 0.55 {
		t.Fatalf("half turn gave %v", v)
	}
}

func TestModelSkipsChiptuneAndReachesEnds(t *testing.T) {
	a := testApp()
	a.pageIdx = pageIndex("MODEL")
	a.preset.P["engine"] = 8
	a.onKnob(0, 0)
	for v := 1; v <= 127; v++ {
		a.onKnob(0, v)
		if a.preset.P["engine"] == 7 {
			t.Fatal("chiptune offered")
		}
	}
	if a.preset.P["engine"] != modelChoices[len(modelChoices)-1] {
		t.Fatalf("end of turn = %v", a.preset.P["engine"])
	}
}

func TestFineTuneAndHarmonicsNames(t *testing.T) {
	a := testApp()
	a.pageIdx = pageIndex("SOUND")
	a.onKnob(1, 60)
	a.onKnob(1, 70) // touches timbre
	before := a.preset.P["timbre"]
	a.fineTune(1)
	if d := a.preset.P["timbre"] - before; d < 0.004 || d > 0.006 {
		t.Fatalf("fine step %v", d)
	}
	if n := harmonicsName(3, (2+0.5)/32/1.02); n != "WINTRHODES" {
		t.Fatalf("dx name %q", n)
	}
	if n := harmonicsName(14, (5+0.5)/11); n != "MINOR 9" {
		t.Fatalf("chord %q", n)
	}
}

func TestFaderSuitePickAndGlide(t *testing.T) {
	a := testApp()
	if len(faderTargets) != 8 {
		t.Fatalf("%d fader effects, want 8", len(faderTargets))
	}
	for _, ft := range faderTargets {
		if ft.key == "wobble_depth" {
			t.Fatal("wobble should be gone from the fader")
		}
		a.fxNow[0], a.fxGoal[0] = 1, 1
	}
	for i, ft := range faderTargets {
		a.fxNow[i], a.fxGoal[i] = ft.neutral, ft.neutral
	}
	// hold KNOB FX + pad 4 = STUTTER, from any view
	a.seqHeldButton(controls.BtnPadFX, true)
	a.onPad(3, 100, true)
	a.seqHeldButton(controls.BtnPadFX, false)
	if faderTargets[a.faderIdx].label != "STUTTER" || a.mode != modePlay {
		t.Fatalf("picked %s, mode %v", faderTargets[a.faderIdx].label, a.mode)
	}
	a.onFader(127, 0)
	a.glideFader()
	if a.fxNow[3] > 0.06 {
		t.Fatalf("effect jumped to %v instead of gliding", a.fxNow[3])
	}
	for i := 0; i < 40; i++ {
		a.glideFader()
	}
	if a.fxNow[3] != 1 {
		t.Fatalf("glide ended at %v", a.fxNow[3])
	}
	// picking another effect glides the old one back to neutral, smoothly
	a.selectFader(2)
	a.glideFader()
	if a.fxNow[3] < 0.9 || a.fxNow[3] == 1 {
		t.Fatalf("stutter should glide back, now %v", a.fxNow[3])
	}
	for i := 0; i < 40; i++ {
		a.glideFader()
	}
	if a.fxNow[3] != 0 || a.faderVal != -1 {
		t.Fatal("old effect not back at neutral / new one not waiting for the fader")
	}
	a.shift = true
	a.onFader(127, 0)
	a.onFader(63, 127) // with SHIFT: a quarter as far
	if g := a.fxGoal[2]; g < 0.86 || g > 0.89 {
		t.Fatalf("shift fine move gave %v, want about 0.874", g)
	}
	// a plain KNOB FX tap opens the FX view; pad 16 resets everything
	a.shift = false
	a.seqHeldButton(controls.BtnPadFX, true)
	a.seqHeldButton(controls.BtnPadFX, false)
	if a.mode != modeFX {
		t.Fatal("tap should open the FX view")
	}
	// pad 12 switches STUTTER (effect 4) straight off, then back on where it was
	a.fxGoal[3], a.fxNow[3] = 0.7, 0.7
	a.onPad(11, 100, true)
	if a.fxGoal[3] != 0 {
		t.Fatal("pad 12 should switch stutter off")
	}
	a.onPad(11, 100, true)
	if a.fxGoal[3] != 0.7 {
		t.Fatalf("pad 12 again should bring stutter back to 0.7, got %v", a.fxGoal[3])
	}
	// pad 16 toggles VOLUME: silent, then full
	a.onPad(15, 100, true)
	if a.fxGoal[7] != 0 {
		t.Fatal("volume toggle")
	}
	// 0.9.26: SHIFT + pad is the MPC's printed function (pad 1 FULL LEVEL), not FX off
	a.shift = true
	a.onPad(0, 100, true)
	a.onPad(0, 0, false)
	a.shift = false
	if !a.util.fullLevel {
		t.Fatal("SHIFT + pad 1 should switch FULL LEVEL on")
	}
	a.util.fullLevel = false
	a.resetFX()
	// from any view: SHIFT + KNOB FX = all off; KNOB FX + pad 9-16 = switch
	a.setMode(modePlay)
	a.seqHeldButton(controls.BtnPadFX, true)
	a.onPad(8, 100, true) // LOW PASS on
	a.seqHeldButton(controls.BtnPadFX, false)
	if !a.fxOn(0) || a.mode != modePlay {
		t.Fatal("PAD FX + pad 9 should switch LOW PASS on without opening the view")
	}
	a.shift = true
	a.seqHeldButton(controls.BtnKnobFX, true)
	a.seqHeldButton(controls.BtnKnobFX, false)
	a.shift = false
	if a.fxOn(0) || a.mode != modePlay {
		t.Fatal("SHIFT + KNOB FX should switch everything off")
	}
}

func TestLiveLengthPads(t *testing.T) {
	a := testApp()
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.sq.playing, a.sq.posPat = true, 1
	a.seqHeldButton(controls.BtnLoop, true)
	a.onPad(2, 100, true)  // pad 3 = 4 steps, applies to the playing pattern
	a.onPad(15, 100, true) // pad 16 = one more
	a.seqHeldButton(controls.BtnLoop, false)
	if a.proj.Pat[1].Len != 5 || a.mode != modePlay {
		t.Fatalf("len %d mode %v", a.proj.Pat[1].Len, a.mode)
	}
	sent := strings.Join(c.lines, "")
	if !strings.Contains(sent, "LEN 1 4") || !strings.Contains(sent, "LEN 1 5") {
		t.Fatalf("sent %q", sent)
	}
	// a plain LOOP tap changes nothing; SHIFT + LOOP opens the patterns view
	a.seqHeldButton(controls.BtnLoop, true)
	a.seqHeldButton(controls.BtnLoop, false)
	if a.mode != modePlay {
		t.Fatal("a plain tap must not switch views")
	}
	a.shift = true
	a.seqHeldButton(controls.BtnLoop, true)
	a.seqHeldButton(controls.BtnLoop, false)
	a.shift = false
	if a.mode != modePatterns {
		t.Fatal("SHIFT + LOOP should open patterns")
	}
	// ERASE + LOOP clears the chain
	a.proj.Chain = []int{0, 1}
	a.sq.eraseHeld = true
	a.seqHeldButton(controls.BtnLoop, true)
	a.seqHeldButton(controls.BtnLoop, false)
	a.sq.eraseHeld = false
	if len(a.proj.Chain) != 0 {
		t.Fatal("ERASE + LOOP should clear the chain")
	}
}

type capture struct{ lines []string }

func (c *capture) Write(b []byte) (int, error) {
	c.lines = append(c.lines, string(b))
	return len(b), nil
}
func (c *capture) Close() error { return nil }

func TestStepEditing(t *testing.T) {
	a := testApp()
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.proj.Sel, a.proj.Track = 3, 2 // empty pattern, hat track
	a.preset = &a.proj.Tracks[2].Sound
	a.setMode(modeStep)
	// tap = on, tap again = off
	a.onPad(4, 100, true)
	a.onPad(4, 0, false)
	st := &a.proj.Pat[3].S[2][4]
	if !st.On || st.Note != a.proj.Tracks[2].Note {
		t.Fatalf("step not on: %+v", st)
	}
	a.onPad(4, 100, true)
	a.onPad(4, 0, false)
	if st.On {
		t.Fatal("second tap should turn it off")
	}
	// hold + K2 edits velocity and keeps the step on
	a.onPad(5, 100, true)
	a.onKnob(1, 64)
	a.onKnob(1, 20)
	a.onPad(5, 0, false)
	st = &a.proj.Pat[3].S[2][5]
	if !st.On || st.Vel > 0.5 {
		t.Fatalf("hold+K2: %+v", st)
	}
	// hold + SHIFT + K1 on the SOUND page locks harmonics
	a.pageIdx = pageIndex("SOUND")
	a.onPad(5, 100, true)
	a.shift = true
	a.onKnob(0, 64)
	a.onKnob(0, 127)
	a.shift = false
	a.onPad(5, 0, false)
	if v, ok := st.LockValue("harmonics"); !ok || v < 0.99 {
		t.Fatalf("lock: %+v", st.Locks)
	}
	if !st.On {
		t.Fatal("editing a held step must not turn it off")
	}
	sent := strings.Join(c.lines, "")
	if !strings.Contains(sent, "L 3 2 5 harmonics 1") {
		t.Fatalf("lock not sent to engine:\n%s", sent)
	}
	// DATA = length
	a.seqData(127)
	if a.proj.Pat[3].Len != 15 || !strings.Contains(strings.Join(c.lines, ""), "LEN 3 15") {
		t.Fatal("length")
	}
}

func TestRecordingAndMute(t *testing.T) {
	a := testApp()
	c := &capture{}
	a.dsp = &dsp{in: c, ev: make(chan string, 8)}
	a.onSeqButton(controls.BtnSeqRecord)
	a.onSeqButton(controls.BtnPlay)
	a.onPad(0, 90, true)
	a.onPad(0, 0, false)
	a.setVal("cutoff", 0.3) // knob while recording = lock, not the sound
	sent := strings.Join(c.lines, "")
	for _, w := range []string{"PLAY", "R 36 0.709", "r 36", "TL cutoff 0.3"} {
		if !strings.Contains(sent, w) {
			t.Fatalf("missing %q in\n%s", w, sent)
		}
	}
	if a.preset.P["cutoff"] == 0.3 {
		t.Fatal("recorded knob changed the sound itself")
	}
	a.dsp.ev <- "POS 0 4"
	a.dsp.ev <- "REC 0 0 5 36 0.709 0.000"
	a.seqTick()
	if !a.proj.Pat[0].S[0][5].On || a.sq.posStep != 4 || !a.sq.tRecorded {
		t.Fatal("engine replies not applied")
	}
	a.sq.muteHeld = true
	a.onPad(2, 100, true)
	if !a.proj.Tracks[2].Muted || !strings.Contains(strings.Join(c.lines, ""), "M 2 1") {
		t.Fatal("MUTE+pad")
	}
}

func TestSixtyFourStepsAndFollow(t *testing.T) {
	a := testApp()
	a.dsp = &dsp{in: &capture{}, ev: make(chan string, 8)}
	a.sq.playing, a.sq.posPat = true, 0
	a.seqHeldButton(controls.BtnLoop, true)
	a.onPad(6, 100, true) // pad 7 = 64 steps (4 bars)
	a.seqHeldButton(controls.BtnLoop, false)
	if a.proj.Pat[0].Len != 64 {
		t.Fatalf("len %d", a.proj.Pat[0].Len)
	}
	a.setMode(modeStep)
	// the pads follow the playhead bar by bar
	for _, c := range []struct{ step, page int }{{5, 0}, {20, 1}, {40, 2}, {63, 3}, {0, 0}} {
		a.dsp.ev <- fmt.Sprintf("POS 0 %d", c.step)
		a.seqTick()
		if a.sq.stepPage != c.page {
			t.Fatalf("step %d: pads on bar %d, want %d", c.step, a.sq.stepPage+1, c.page+1)
		}
	}
	// a step on bar 3 is reachable and editable
	a.dsp.ev <- "POS 0 40"
	a.seqTick()
	a.onPad(9, 100, true) // bar 3, step 10 = step 42
	a.onPad(9, 0, false)
	if !a.proj.Pat[0].S[a.proj.Track][41].On {
		t.Fatal("step 42 not set")
	}
	// PAD BANK picks a bar and stops following; SHIFT+PAD BANK follows again
	a.onSeqButton(controls.BtnPadBank)
	a.dsp.ev <- "POS 0 5"
	a.seqTick()
	if a.sq.stepPage != 3 || !a.sq.noFollow {
		t.Fatalf("hold: page %d", a.sq.stepPage)
	}
	a.shift = true
	a.onSeqButton(controls.BtnPadBank)
	a.shift = false
	if a.sq.noFollow || a.sq.stepPage != 0 {
		t.Fatalf("follow again: page %d", a.sq.stepPage)
	}
}

func TestPageOrderAndEQ(t *testing.T) {
	want := []string{"MODEL", "EQ", "AMP", "SUB + FILTER", "SOUND", "WARMTH", "WOBBLE", "DUB DELAY", "REVERB", "TAPE", "GLOBAL", "GROOVE"}
	for i, n := range want {
		if pages[i].name != n {
			t.Fatalf("page %d = %s, want %s", i+1, pages[i].name, n)
		}
	}
	a := testApp()
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.pageIdx = pageIndex("EQ")
	a.onKnob(0, 64)
	a.onKnob(0, 127)
	if a.preset.P["eq_low"] < 0.99 || !strings.Contains(strings.Join(c.lines, ""), "T 0 eq_low 1") {
		t.Fatal("EQ low not set on the track")
	}
	if dbShow(1) != "+18.0 DB" || dbShow(0.5) != "FLAT" || dbShow(0) != "KILL" || dbShow(0.25) != "-18.0 DB" {
		t.Fatal(dbShow(1), dbShow(0), dbShow(0.25))
	}
	// SHIFT + DATA push resets the knob touched last; twice = whole page
	a.shift = true
	a.onButton(controls.BtnDataPush, true)
	if a.preset.P["eq_low"] != 0.5 {
		t.Fatalf("reset gave %v", a.preset.P["eq_low"])
	}
	a.preset.P["eq_mid"], a.preset.P["eq_high"] = 0.9, 0.1
	a.onButton(controls.BtnDataPush, true)
	a.shift = false
	if a.preset.P["eq_mid"] != 0.5 || a.preset.P["eq_high"] != 0.5 {
		t.Fatal("double press should reset the whole page")
	}
	// GROOVE: SHIFT + K3 snaps pan between left, centre and right
	a.pageIdx = pageIndex("GROOVE")
	a.shift = true
	a.preset.P["pan"] = 0.42
	a.onKnob(2, 64)
	a.onKnob(2, 127)
	if a.preset.P["pan"] != 1 {
		t.Fatalf("pan snap gave %v", a.preset.P["pan"])
	}
	a.shift = false
	// STOP while playing lets tails ring; STOP again = silence
	c.lines = nil
	a.sq.playing = true
	a.onSeqButton(controls.BtnStop)
	if strings.Contains(strings.Join(c.lines, ""), "KILL") || a.sq.playing {
		t.Fatal("first STOP should only stop the sequencer")
	}
	a.onSeqButton(controls.BtnStop)
	if !strings.Contains(strings.Join(c.lines, ""), "KILL") {
		t.Fatal("second STOP must hard-stop")
	}
}

func TestEraseAll(t *testing.T) {
	a := testApp()
	a.proj = seq.Default()
	a.preset = &a.proj.Tracks[0].Sound
	count := func(p int, tr int) int {
		n := 0
		for t := 0; t < seq.Tracks; t++ {
			if tr >= 0 && t != tr {
				continue
			}
			for s := 0; s < seq.Steps; s++ {
				if a.proj.Pat[p].S[t][s].On {
					n++
				}
			}
		}
		return n
	}
	a.proj.Sel, a.sq.viewPat, a.proj.Track = 0, 0, 0
	before, beforeT0 := count(0, -1), count(0, 0)
	if before == 0 || beforeT0 == 0 {
		t.Fatal("starter project has no notes in pattern 1")
	}
	// step view: track only, two presses
	a.mode, a.shift = modeStep, true
	a.eraseAllPress()
	if count(0, -1) != before {
		t.Fatal("first press must only arm")
	}
	a.eraseAllPress()
	if count(0, 0) != 0 || count(0, -1) != before-beforeT0 {
		t.Fatalf("track clear wrong: %d left on t0, %d total", count(0, 0), count(0, -1))
	}
	a.eraseAllPress() // undo
	if count(0, -1) != before {
		t.Fatal("undo did not restore")
	}
	// play view: the whole pattern
	a.mode = modePlay
	a.sq.eraseUndo = nil
	a.eraseAllPress()
	a.eraseAllPress()
	if count(0, -1) != 0 {
		t.Fatal("pattern clear left notes")
	}
}
