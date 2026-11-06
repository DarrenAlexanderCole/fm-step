package main

// Screens of 0.9.22 as built (not mockups): FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestShots022

import (
	"image"
	"image/color"
	"image/png"
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
	// MIXTAPE recording
	a.mt = mtState{view: true, rec: true, name: "TAKE 007", frames: 252 * 48000, limit: 20 * 60 * 48000, free: 12 << 30,
		started: time.Date(2026, 11, 5, 21, 0, 0, 0, time.Local), pk: [2]float64{0.42, 0.38}, angle: 0.7}
	shot("1 MIXTAPE recording")
	a.mt.rec, a.mt.ask = false, true
	shot("2 MIXTAPE keep the take")
	a.mt.ask, a.mt.playing, a.mt.playPos, a.mt.playTotal = false, true, 96*48000, 252*48000
	a.mt.path = "x"
	shot("3 MIXTAPE listening")
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
