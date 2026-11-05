package main

import (
	"fmt"
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
)

// TestGifFrames renders short frame sequences of the interface in motion for
// the README and the press page (set FMSTEP_GIF=dir; tools/make_gifs.py turns
// them into GIFs). The spectrum is synthetic; the drawing code is the app's.
func TestGifFrames(t *testing.T) {
	dir := os.Getenv("FMSTEP_GIF")
	if dir == "" {
		t.Skip("set FMSTEP_GIF")
	}
	a := &app{scr: panel.NewMemScreen(320, 240), held: map[int]int{}, user: t.TempDir(), dsp: &dsp{in: nopWC{io.Discard}}}
	a.proj = seq.Default()
	a.proj.Track = 4
	a.preset = &a.proj.Tracks[4].Sound
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	a.tourDone = true
	for i, ft := range faderTargets {
		a.fxNow[i], a.fxGoal[i] = ft.neutral, ft.neutral
	}
	p := &a.proj.Pat[0]
	// a moving synthetic spectrum: kick pulses on the beat, mids breathe
	spec := func(k int, lowCut float64) {
		ph := float64(k%8) / 8
		kick := math.Exp(-ph * 5)
		for b := 0; b < 48; b++ {
			f := specFreq(float64(b))
			v := -34 - 9*math.Log2(f/80)*0.55 + 5*math.Sin(float64(b)*1.7+float64(k)*0.4) + 3*math.Cos(float64(b)*0.6-float64(k)*0.25)
			if f < 140 {
				v = -30 + 22*kick + 3*math.Sin(float64(b))
			}
			if f < 300 {
				v -= lowCut * (1 - f/300) * 40
			}
			a.dsp.spec[b] = math.Max(-88, v)
		}
		a.dsp.peakL, a.dsp.peakR, a.dsp.specAt = 0.3+0.4*kick, 0.28+0.4*kick, time.Now()
	}
	shot := func(seqName string, i int) {
		a.draw()
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				c := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255})
			}
		}
		os.MkdirAll(filepath.Join(dir, seqName), 0755)
		f, _ := os.Create(filepath.Join(dir, seqName, fmt.Sprintf("%03d.png", i)))
		png.Encode(f, img)
		f.Close()
	}
	step := func(k int) { // one 16th per frame
		s := k % p.Len
		a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, s
		for tr := 0; tr < seq.Tracks; tr++ {
			if st := p.S[tr][s]; st.On {
				a.sq.flash[tr] = time.Now()
				a.addHit(tr, st.Note, st.Vel, s, p.Len)
			}
		}
	}
	const frameDur = 60 * time.Millisecond

	// 1. the knob flag riding along on the dub delay page
	a.setMode(modePlay)
	a.pageIdx = pageIndex("DUB DELAY")
	for i := 0; i < 36; i++ {
		v := 0.25 + 0.6*(0.5-0.5*math.Cos(float64(i)/35*2*math.Pi))
		a.setVal("delay_feedback", v)
		a.lastKey = "delay_feedback"
		a.touched("delay_feedback")
		spec(i, 0)
		step(i)
		a.updateSpec()
		shot("flag", i)
		time.Sleep(frameDur)
	}
	// 2. EQ: the low band cut to a kill and back, over the live spectrum
	a.pageIdx = pageIndex("EQ")
	for i := 0; i < 40; i++ {
		u := 0.5 - 0.5*math.Cos(float64(i)/39*2*math.Pi) // 0..1..0
		a.setVal("eq_low", 0.5-0.5*u)
		a.lastKey = "eq_low"
		a.touched("eq_low")
		spec(i, u)
		step(i)
		a.updateSpec()
		shot("eq", i)
		time.Sleep(frameDur)
	}
	// 3. FLOW: two bars of the starter groove
	a.ui.hits = nil
	a.ui.touchKey = ""
	a.ui.scope = 2
	for i := 0; i < 48; i++ {
		spec(i, 0)
		step(i)
		a.updateSpec()
		shot("flow", i)
		time.Sleep(frameDur)
	}
	// 4. the spectrum face while CUTOFF sweeps
	a.ui.scope = 1
	a.pageIdx = pageIndex("SUB + FILTER")
	for i := 0; i < 36; i++ {
		a.setVal("cutoff", 0.3+0.45*(0.5-0.5*math.Cos(float64(i)/35*2*math.Pi)))
		a.lastKey = "cutoff"
		a.touched("cutoff")
		spec(i, 0)
		step(i)
		a.updateSpec()
		shot("spectrum", i)
		time.Sleep(frameDur)
	}
	// 5. all tracks' notes with the playhead
	a.ui.scope = 0
	a.ui.touchKey = ""
	a.setMode(modeTracks)
	for i := 0; i < 32; i++ {
		step(i)
		shot("tracks", i)
		time.Sleep(frameDur)
	}
}
