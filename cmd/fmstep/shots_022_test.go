package main

// Screens of 0.9.22 as built (not mockups): FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestShots022

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func savePNG(a *app, path string) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			v := a.scr.RGB(x, y)
			img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
		}
	}
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
}

func TestShots022(t *testing.T) {
	dir := os.Getenv("FMSTEP_MOCK")
	if dir == "" {
		t.Skip("set FMSTEP_MOCK")
	}
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	shot := func(name string) {
		a.statusAt = time.Time{}
		a.drawV2()
		savePNG(a, filepath.Join(dir, name+".png"))
	}
	// SUPA TAPE
	rw := waveCols(breakWave(48000, 5, 3), 150)
	env := waveCols(breakWave(48000*5, 252, 7), 2520)
	a.dsp.mt.recent, a.dsp.mt.recEnv = rw, env
	a.dsp.mt.lv[8] = 200
	tk := filepath.Join(t.TempDir(), "take 007.wav")
	os.MkdirAll(filepath.Dir(peaksPath(tk)), 0755)
	pkb := make([]byte, len(env))
	for i, v := range env {
		pkb[i] = byte(math.Min(255, 60+v*200))
	}
	os.WriteFile(peaksPath(tk), pkb, 0644)
	a.mt = mtState{view: true, rec: true, name: "TAKE 007", frames: 252 * 48000, limit: 20 * 60 * 48000, free: 102 << 30,
		started: time.Now(), pk: [2]float64{0.42, 0.38}, angle: 0.7, path: tk}
	shot("1 SUPA TAPE recording")
	a.mt.paused = true
	shot("1b SUPA TAPE recording paused")
	a.mt.paused, a.mt.rec, a.mt.ask = false, false, true
	shot("2 SUPA TAPE keep the take")
	a.mt.playing, a.mt.playPos, a.mt.playTotal, a.mt.playPath = true, 96*48000, 252*48000, tk
	shot("3 SUPA TAPE listening")
	a.mt.lpaused = true
	shot("3b SUPA TAPE listening paused")
	// 0.9.24: trimming before SAVE (IN, OUT, a 1-bar loop), whole take and FINE
	a.mt.lpaused = false
	a.mt.trimIn, a.mt.trimOut, a.mt.loopIdx = 80, 200, 5
	a.mt.playPos = 82 * 48000
	a.mt.knobK, a.mt.knobAt = 1, time.Now()
	shot("3c TRIM loop")
	a.mt.fine, a.mt.loopIdx = true, 0
	a.mt.knobK, a.mt.trimFocus = 0, 0
	shot("3d TRIM fine")
	a.mt = mtState{view: true, rec: true, name: "TAKE 008", frames: 31 * 48000, limit: 20 * 60 * 48000, free: 102 << 30,
		started: time.Now(), pk: [2]float64{0.92, 0.88}, path: tk, knobK: 2, knobAt: time.Now()}
	a.char["mt_pad"], a.char["mt_comp"] = 0.25, 1.0/3
	shot("3e REC knobs")
	a.mt = mtState{}
	// the LIBRARY with a take
	md := a.mixtapeDir()
	os.MkdirAll(filepath.Join(md, ".peaks"), 0755)
	p := filepath.Join(md, "take 007.wav")
	writeTestWav(t, p, 48000*252)
	pk := make([]byte, 2520)
	for i := range pk {
		f := float64(i) / 2520
		v := 0.35 + 0.4*f + 0.2*float64((i/4)%2)
		if i > 1900 {
			v *= 0.6
		}
		pk[i] = byte(min(255, int(v*255)))
	}
	os.WriteFile(peaksPath(p), pk, 0644)
	writeTestWav(t, filepath.Join(md, "take 006.wav"), 48000*75)
	sdir := a.samplesDir()
	os.MkdirAll(sdir, 0755)
	writeTestWav(t, filepath.Join(sdir, "amen break.wav"), 48000*4)
	writeTestWav(t, filepath.Join(sdir, "rhodes loop 92.wav"), 48000*10)
	a.smpEnter(smpTape)
	a.smp.takeFor, a.smp.takeFrom = p, 131
	shot("4 LIBRARY a take")
	a.smp.fileIdx = 2
	shot("5 LIBRARY samples")
	a.smpLeave()
	// the mixer with level dots
	a.mode, a.ui.mixer = modeTracks, true
	a.dsp.mt.lv = [9]uint8{235, 200, 160, 245, 0, 120, 190, 210, 240}
	a.dsp.mt.lvAt = time.Now()
	a.proj.Tracks[5].Muted = true
	a.mt.lvPeak = [9]float64{0.95, 0.8, 0.7, 1, 0, 0.6, 0.75, 0.85}
	a.mt.lvPeakAt = [9]time.Time{time.Now(), time.Now(), time.Now(), time.Now(), time.Now(), time.Now(), time.Now(), time.Now()}
	shot("6 MIXER level dots")
	// MODEL page with keepers
	a.mode = modePlay
	a.pageIdx = pageIndex("MODEL")
	a.preset.P["engine"] = 0
	a.keep = map[int]bool{0: true, 10: true, 14: true, 21: true}
	shot("7 MODEL keep")
	a.preset.P["engine"] = 11
	shot("8 MODEL not in the 4")
}
