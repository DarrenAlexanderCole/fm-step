package main

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/panel"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

// TestPreviewV2 renders the 0.9 screens to PNG (set FMSTEP_PREVIEW=dir) and
// times one full frame of each.
func TestPreviewV2(t *testing.T) {
	dir := os.Getenv("FMSTEP_PREVIEW")
	a := &app{scr: panel.NewMemScreen(320, 240), held: map[int]int{}, user: t.TempDir(), dsp: &dsp{in: nopWC{io.Discard}}}
	a.proj = seq.Default()
	a.proj.Track = 4
	a.preset = &a.proj.Tracks[4].Sound
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	a.tourDone = true
	// a plausible mix spectrum: kick and bass low, mids, falling highs
	for b := 0; b < 48; b++ {
		f := specFreq(float64(b))
		v := -30 - 9*math.Log2(f/80)*0.55 + 6*math.Sin(float64(b)*1.7) + 3*math.Cos(float64(b)*0.6)
		if f < 120 {
			v = -14 + 3*math.Sin(float64(b))
		}
		a.dsp.spec[b] = math.Max(-88, v)
	}
	a.dsp.peakL, a.dsp.peakR, a.dsp.specAt = 0.5, 0.42, time.Now()
	for i := 0; i < 8; i++ {
		a.updateSpec()
		time.Sleep(10 * time.Millisecond)
		a.dsp.specAt = time.Now()
	}
	for i, ft := range faderTargets {
		a.fxNow[i], a.fxGoal[i] = ft.neutral, ft.neutral
	}
	a.preset.P["sub_level"], a.preset.P["cutoff"], a.preset.P["resonance"] = 0.85, 0.42, 0.55
	a.preset.P["eq_low"], a.preset.P["eq_mid"], a.preset.P["eq_high"], a.preset.P["eq_mid_freq"] = 0.375, 0.68, 0, 0.6
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 41
	shot := func(name string) {
		t0 := time.Now()
		a.draw()
		t.Logf("%-16s %v", name, time.Since(t0))
		if dir == "" {
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				c := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255})
			}
		}
		f, _ := os.Create(filepath.Join(dir, "v2-"+name+".png"))
		png.Encode(f, img)
		f.Close()
	}
	for i := range pages {
		a.pageIdx = i
		a.lastKey = pages[i].p[1].key
		a.ui.touchKey, a.ui.touchAt = "", time.Time{}
		shot(pages[i].name)
		if i < 4 || pages[i].name == "DUB DELAY" {
			kk := 1
			if pages[i].name == "MODEL" || pages[i].name == "DUB DELAY" {
				kk = 0
			}
			a.touched(pages[i].p[kk].key)
			shot(pages[i].name + "-turn")
		}
	}
	// GLOBAL with a knob turning: a dot along the bottom, no chip over the BPM
	a.pageIdx = pageIndex("GLOBAL")
	a.touched(pages[a.pageIdx].p[0].key)
	shot("GLOBAL-turn")
	// DOTS visualizer, still and with K2 turning
	a.ui.touchKey = ""
	a.ui.scope = 3
	a.updateSpec()
	shot("dots")
	a.touched(a.cur(1).key)
	shot("dots-turn")
	a.ui.scope = 0
	a.ui.touchKey = ""
	a.pageIdx = pageIndex("EQ")
	a.preset.P["eq_low_freq"], a.preset.P["eq_high_freq"] = 0.35, 0.8
	a.touched("eq_low_freq")
	a.lastKey = "eq_low_freq"
	shot("EQ-lowfreq")
	a.ui.touchKey = ""
	a.preset.P["eq_low_freq"], a.preset.P["eq_high_freq"] = 0.574, 0.5283
	a.fxGoal[3], a.fxNow[3], a.faderIdx = 0.7, 0.7, 3
	a.status, a.statusAt = "RESET LOW: FLAT (TWICE: WHOLE PAGE)", time.Now()
	shot("EQ-status")
	a.status = ""
	// 64-step pattern with long notes and locks, one step held
	p := &a.proj.Pat[0]
	p.Len = 64
	for s := 32; s < 64; s++ {
		p.S[4][s] = p.S[4][s-32]
	}
	p.S[4][48] = seq.Step{On: true, Note: 38, Vel: 1, Gate: 6}
	p.S[4][41] = seq.Step{On: true, Note: 39, Vel: 0.9, Gate: 0.5, Micro: 0.1}
	p.S[4][41].SetLock("cutoff", 0.62)
	p.S[4][41].SetLock("delay_send", 0.7)
	a.setMode(modeStep)
	shot("steps")
	a.sq.held = 42
	shot("steps-held")
	a.sq.held = 0
	a.sq.loopHeld = true
	shot("length")
	a.sq.loopHeld = false
	a.setMode(modePlay)
	a.pageIdx = pageIndex("GLOBAL")
	a.tapHeld = true
	shot("tempo")
	a.tapHeld = false
	a.tourDone, a.tour, a.tourIdx = false, seqTour, 4
	a.pageIdx = pageIndex("SUB + FILTER")
	shot("tour")
	a.tourDone = true
	a.sq.flash[0], a.sq.flash[2] = time.Now(), time.Now().Add(-100*time.Millisecond)
	a.fxGoal[4], a.fxNow[4] = 0.5, 0.5
	a.dsp.specAt = time.Now()
	a.ui.scope = 1
	shot("scope")
	a.pageIdx = pageIndex("SUB + FILTER")
	a.touched("cutoff")
	shot("scope-turn")
	a.ui.touchKey = ""
	// FLOW: two bars of the starter pattern as recent hits
	now := time.Now()
	pp := &a.proj.Pat[0]
	for k := 0; k < 32; k++ {
		s := k % pp.Len
		for tr := 0; tr < seq.Tracks; tr++ {
			if st := pp.S[tr][s]; st.On {
				a.addHit(tr, st.Note, st.Vel, s, pp.Len)
				a.ui.hits[len(a.ui.hits)-1].at = now.Add(-time.Duration(32-k) * 80 * time.Millisecond)
			}
		}
	}
	a.ui.scope = 2
	shot("flow")
	a.pageIdx = pageIndex("AMP")
	a.touched("decay")
	shot("flow-turn")
	shot("AMP-flag")
	a.ui.scope = 0
	a.pageIdx = pageIndex("DUB DELAY")
	a.touched("delay_feedback")
	shot("DELAY-flag")
	a.ui.touchKey = ""
	a.ui.scope = 2
	a.ui.scope = 1
	a.ui.scope = 0
	// the other views
	a.proj.Tracks[3].Muted = true
	a.sq.flash[0], a.sq.flash[4] = time.Now(), time.Now().Add(-150*time.Millisecond)
	for _, pi := range []int{1, 2, 5} {
		a.proj.Pat[pi] = a.proj.Pat[0]
	}
	a.proj.Chain = []int{0, 1, 0, 2}
	a.setMode(modeTracks)
	shot("tracks")
	a.preset = &a.proj.Tracks[a.proj.Track].Sound
	a.onKnob(0, 64)
	a.onKnob(0, 40)
	shot("tracks-turn")
	a.setMode(modePatterns)
	shot("patterns")
	a.fxGoal[0], a.fxNow[0] = 0.4, 0.4
	a.fxGoal[2], a.fxNow[2] = 0.6, 0.6
	a.faderIdx = 2
	a.setMode(modeFX)
	shot("fx")
	a.sq.projNames[0], a.sq.projNames[1], a.sq.projNames[4] = "STARTER GROOVES", "NIGHT DUB", "HOUSE SKETCH 2"
	a.sq.projSlot = 2
	a.setMode(modeProject)
	shot("projects")
	a.slot = 3
	a.setMode(modePreset)
	shot("sounds")
	a.setMode(modeConfirmExit)
	shot("exit")
	_ = sound.Keys
}
