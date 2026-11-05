package main

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"fmstep/internal/panel"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

// TestPreviewScreens renders the main screens to PNG (set FMSTEP_PREVIEW=dir).
func TestPreviewScreens(t *testing.T) {
	dir := os.Getenv("FMSTEP_PREVIEW")
	if dir == "" {
		t.Skip("set FMSTEP_PREVIEW to write PNGs")
	}
	a := &app{scr: panel.NewMemScreen(320, 240), held: map[int]int{}, user: t.TempDir(), dsp: &dsp{in: nopWC{io.Discard}}}
	a.proj = seq.Default()
	a.proj.Tracks[0].Sound = sound.Factory()[1]
	a.preset = &a.proj.Tracks[0].Sound
	a.tour = []tourStep{{"SAMPLE SELECT, PAD 2,\nSAMPLE SELECT. HOLD A PAD.", nil}}
	a.slot = 2
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	shot := func(name string) {
		a.draw()
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				c := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255})
			}
		}
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, img)
		f.Close()
	}
	for i := range pages {
		a.pageIdx = i
		if i == 1 {
			a.tourDone = true
		}
		if pages[i].name == "DUB DELAY" {
			a.preset.P["delay_feedback"] = 0.95
		}
		shot(filepath.Base(string(rune('a'+i))) + "-page")
	}
	a.shift, a.pageIdx = true, pageIndex("TAPE")
	shot("i-tape-shift")
	a.shift, a.mode, a.recHeld = false, modePreset, true
	shot("j-sounds")
	a.recHeld, a.tourDone = false, true
	a.sq.playing, a.sq.posPat, a.sq.posStep, a.sq.recArm = true, 0, 6, true
	a.setMode(modePlay)
	shot("k-play-strip")
	a.setMode(modeStep)
	shot("l-steps")
	a.sq.held = 11
	shot("m-step-held")
	a.sq.held = 0
	a.setMode(modeTracks)
	a.proj.Tracks[3].Muted = true
	shot("n-tracks")
	a.proj.Chain = []int{0, 0, 1, 2}
	a.setMode(modePatterns)
	shot("o-patterns")
	a.setMode(modeProject)
	shot("p-projects")
	a.tourDone, a.tourIdx, a.tour = false, 3, seqTour
	a.setMode(modeStep)
	shot("q-step-tour")
	a.tourDone = true
	for i, ft := range faderTargets {
		a.fxNow[i] = ft.neutral
	}
	a.fxNow[0], a.fxNow[3], a.fxNow[6], a.fxNow[7], a.faderIdx = 0.4, 0.7, 0.3, 0.8, 2
	a.faderVal, a.ccKnown[20], a.lastCC[20] = -1, true, 100
	a.setMode(modeFX)
	shot("r-fx")
	a.setMode(modePlay)
	a.sq.loopHeld = true
	shot("s-length")
	a.sq.loopHeld = false
	a.proj.Pat[0].Len = 64
	a.proj.CopyPattern(0, 1)
	for tr := 0; tr < 8; tr++ {
		for i := 32; i < 64; i++ {
			a.proj.Pat[0].S[tr][i] = a.proj.Pat[1].S[tr][i-32]
		}
	}
	a.sq.posStep = 41
	st := &a.proj.Pat[0].S[0]
	st[4] = seq.Step{On: true, Note: 40, Vel: 0.9, Gate: 3.5}
	st[12] = seq.Step{On: true, Note: 40, Vel: 0.6, Gate: 0.1}
	st[13] = seq.Step{On: true, Note: 40, Vel: 0.7, Gate: 1}
	st[14] = seq.Step{On: true, Note: 40, Vel: 1, Gate: 0.25, Micro: 0.3}
	st[15] = seq.Step{On: true, Note: 40, Vel: 1, Gate: 6}
	a.setMode(modeStep)
	shot("t-steps64")
	a.setMode(modeTracks)
	shot("u-tracks64")
	a.setMode(modePlay)
	shot("v-play64")
}
