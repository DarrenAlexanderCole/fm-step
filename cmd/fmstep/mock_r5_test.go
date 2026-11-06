package main

// Design round 5: SHAPER with the pad's waveform and an ADSR. FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR5

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

const shLimeY = 0xD8FF4A

// shWave: a fake 8-hit break, min/max per column, -1..1
func shWave(n int) (mn, mx []float64) {
	mn, mx = make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		ph := math.Mod(t*8, 1)
		hit := math.Exp(-ph*4.5) * (0.75 + 0.25*math.Sin(float64(int(t*8))*2.1))
		body := 0.22 * math.Exp(-ph*1.5)
		n1 := 0.5 + 0.5*math.Sin(float64(i)*1.7+math.Sin(float64(i)*0.31)*3)
		n2 := 0.5 + 0.5*math.Sin(float64(i)*2.3+1)
		v := 1.25 * (hit*(0.7+0.3*n1) + body*n2)
		if t > 0.97 {
			v *= (1 - t) / 0.03
		}
		mx[i], mn[i] = math.Min(1, v+0.02), -math.Min(1, v*(0.85+0.15*n2)+0.02)
	}
	return
}

type shEnv struct {
	a, d, s, r float64 // a, d, r as a fraction of the sample; s level 0..1
	off        float64 // where the pad lets go (one shot: the end)
}

func (e shEnv) at(t float64) float64 {
	var v float64
	switch {
	case t < e.a:
		v = t / e.a
	case t < e.a+e.d:
		v = 1 - (1-e.s)*(t-e.a)/e.d
	default:
		v = e.s
	}
	if t > e.off {
		v *= math.Max(0, 1-(t-e.off)/e.r)
	}
	return v
}

func r5Shaper(a *app, c *gfx.Canvas, mode string, e shEnv, loop bool, labels, vals [3]string, norms [3]float64, active int, hint string) {
	btnLegend(c, [3]string{mode, "PLAY", "TRIM"}, 0)
	c.Text(10, 46, "SHAPER", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 47, "PAD 05  ·  BREAK 09", gfx.Small, uDim, gfx.Left|gfx.Mid)

	x0, y0, w, h := 8.0, 58.0, 304.0, 116.0
	glass(c, x0, y0, w, h, 10, shBlue)
	// a faint dot grid, as on every card
	for gy := y0 + 6; gy < y0+h-2; gy += 6 {
		for gx := x0 + 6; gx < x0+w-2; gx += 6 {
			c.Circle(gx, gy, 0.5, uDim, 0.12)
		}
	}
	n := 288
	mn, mx := shWave(n)
	mid, amp := y0+h/2+6, h/2-12
	xs := make([]float64, n)
	top, bot, ftop, fbot := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n-1)
		xs[i] = x0 + 8 + t*(w-16)
		g := e.at(t)
		ftop[i], fbot[i] = mid-mx[i]*amp, mid-mn[i]*amp
		top[i], bot[i] = mid-mx[i]*amp*g, mid-mn[i]*amp*g
	}
	// the untouched sound, a ghost
	for i := range xs {
		c.Line(xs[i], ftop[i], xs[i], fbot[i], 1, shBlue, 0.10)
	}
	// the shaped sound: filled, brighter toward the middle line
	for i := range xs {
		hh := bot[i] - top[i]
		if hh < 0.5 {
			continue
		}
		c.Line(xs[i], top[i], xs[i], mid, 1, shBlue, 0.55)
		c.Line(xs[i], mid, xs[i], bot[i], 1, 0x5A3CFF, 0.45)
	}
	c.Polyline(xs, top, 1, shLine, 0.9)
	c.Polyline(xs, bot, 1, shLine, 0.7)
	// beat lines
	for b := 1; b < 8; b++ {
		x := x0 + 8 + float64(b)/8*(w-16)
		c.Line(x, y0+16, x, y0+h-12, 1, shLimeY, 0.18)
	}
	// loop bracket (pink, like the tape)
	if loop {
		la, lb := 0.25, 0.75
		xa, xb := x0+8+la*(w-16), x0+8+lb*(w-16)
		c.Line(xa, y0+10, xb, y0+10, 1.2, shPink, 0.9)
		c.Line(xa, y0+7, xa, y0+h-10, 1, shPink, 0.8)
		c.Line(xb, y0+7, xb, y0+h-10, 1, shPink, 0.8)
	}
	// the envelope: a faint line and its nodes, riding the top
	ey := func(v float64) float64 { return mid - amp - 4 + (1-v)*(amp*0.9) }
	ex := func(t float64) float64 { return x0 + 8 + t*(w-16) }
	var exs, eys []float64
	for i := 0; i <= 200; i++ {
		t := float64(i) / 200
		exs, eys = append(exs, ex(t)), append(eys, ey(e.at(t)))
	}
	c.Polyline(exs, eys, 1, 0xFFFFFF, 0.35)
	nodes := []struct{ t, v float64 }{{e.a, 1}, {e.a + e.d, e.s}, {e.off, e.s}, {math.Min(1, e.off+e.r), 0}}
	for k, nd := range nodes {
		col := uint32(0xFFFFFF)
		if k == active || (active == 1 && k == 1 && labels[1] == "SUSTAIN") {
			col = knobCols[max(0, min(2, active))]
		}
		c.Circle(ex(nd.t), ey(nd.v), 2.4, col, 0.95)
	}
	// play dot + length
	px := ex(0.31)
	c.Line(px, y0+14, px, y0+h-10, 1.2, shLimeY, 0.9)
	c.Circle(px, y0+14, 2.2, shLimeY, 1)
	c.Text(x0+10, y0+h-7, "0:00", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(x0+w-10, y0+h-7, "2.41 s  ·  2 BARS", gfx.Small, uDim, gfx.Right|gfx.Mid)

	mockKnobs(c, labels, vals, norms, active)
	a.uiBottom(c, hint)
}

func TestMockR5(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	adr := [3]string{"ATTACK", "DECAY", "RELEASE"}
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 SHAPER one shot K1 attack", func(c *gfx.Canvas) {
			e := shEnv{a: 0.08, d: 0.3, s: 0.55, r: 0.12, off: 0.88}
			r5Shaper(a, c, "ONE SHOT", e, false, adr, [3]string{"190 ms", "720 ms", "290 ms"}, [3]float64{0.35, 0.55, 0.4}, 0,
				"LEFT: ONE SHOT / GATE / LOOP  ·  SHIFT + K2: SUSTAIN")
		}},
		{"2 SHAPER gate SHIFT sustain", func(c *gfx.Canvas) {
			e := shEnv{a: 0.01, d: 0.12, s: 0.35, r: 0.18, off: 0.6}
			r5Shaper(a, c, "GATE", e, false, [3]string{"ATTACK", "SUSTAIN", "RELEASE"}, [3]string{"2 ms", "35", "430 ms"}, [3]float64{0.02, 0.35, 0.5}, 1,
				"GATE: HOLD THE PAD  ·  LET GO: RELEASE")
		}},
		{"3 SHAPER loop K3 release", func(c *gfx.Canvas) {
			e := shEnv{a: 0.02, d: 0.2, s: 0.8, r: 0.1, off: 0.9}
			r5Shaper(a, c, "LOOP", e, true, adr, [3]string{"5 ms", "480 ms", "240 ms"}, [3]float64{0.05, 0.45, 0.35}, 2,
				"LOOP: PINK REGION REPEATS WHILE HELD  ·  RIGHT: TRIM")
		}},
	}
	for _, s := range shots {
		c := a.scr.C
		c.Clear(uBG)
		a.statusAt = time.Time{}
		a.uiHeader(c)
		s.f(c)
		savePNG(a, filepath.Join(dir, s.name+".png"))
	}
	_ = fmt.Sprintf
}
