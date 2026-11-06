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

// TestDotsFrames renders the DOTS visualizer moving with a synthetic beat
// (set FMSTEP_GIF=dir); without it, just checks a frame is cheap.
func TestDotsFrames(t *testing.T) {
	dir := os.Getenv("FMSTEP_GIF")
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
	a.ui.scope = 3
	a.sq.playing, a.sq.posPat = true, 0
	if dir != "" {
		os.MkdirAll(filepath.Join(dir, "dots"), 0755)
	}
	var worst time.Duration
	for k := 0; k < 96; k++ {
		ph := float64(k%12) / 12
		kick := math.Exp(-ph * 5)
		hat := math.Exp(-math.Mod(ph*2, 1) * 9)
		for b := 0; b < 48; b++ {
			f := specFreq(float64(b))
			v := -40 - 9*math.Log2(f/80)*0.5 + 6*math.Sin(float64(b)*0.9+float64(k)*0.35) + 4*math.Cos(float64(b)*0.4-float64(k)*0.2)
			if f < 140 {
				v = -34 + 30*kick
			}
			if f > 5000 {
				v += 16 * hat
			}
			a.dsp.spec[b] = math.Max(-88, v)
		}
		a.dsp.peakL, a.dsp.peakR, a.dsp.specAt = 0.3+0.4*kick, 0.3+0.4*kick, time.Now()
		a.sq.posStep = (k / 3) % 16
		if k >= 60 && k < 84 {
			a.touched(a.cur(0).key)
		}
		time.Sleep(25 * time.Millisecond) // real time between frames, so levels move
		s := time.Now()
		a.draw()
		if d := time.Since(s); d > worst {
			worst = d
		}
		if dir == "" {
			continue
		}
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				v := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
			}
		}
		f, _ := os.Create(filepath.Join(dir, "dots", fmt.Sprintf("%03d.png", k)))
		png.Encode(f, img)
		f.Close()
	}
	t.Logf("worst DOTS frame %v", worst)
}
