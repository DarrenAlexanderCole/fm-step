package main

// FM-STEP 0.9.10: the second visualizer face (16 LEVELS twice): FLOW.
// Every note that plays drops a bubble on a beat grid — across: where in the
// loop, up: pitch, size: velocity, colour: track — and a line flows from it
// to that track's lane on the right, like a river of the groove. Bubbles and
// lines fade over a few seconds, so the screen draws the last bars.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/seq"
)

type hit struct {
	track, note int
	vel         float64
	pos         float64 // 0..1 through the loop
	at          time.Time
}

const hitLife = 3 * time.Second

func (a *app) addHit(track, note int, vel float64, step, length int) {
	h := hit{track, note, vel, (float64(step) + 0.5) / float64(max(length, 1)), time.Now()}
	a.ui.hits = append(a.ui.hits, h)
	if len(a.ui.hits) > 120 {
		a.ui.hits = a.ui.hits[len(a.ui.hits)-120:]
	}
	if a.ui.scope == 2 {
		a.animate(time.Now().Add(100 * time.Millisecond))
	}
}

func (a *app) drawFlowV2(c *gfx.Canvas) {
	now := time.Now()
	c.Text(10, 31, "FLOW", gfx.Big, uText, gfx.Left|gfx.Mid)
	info := fmt.Sprintf("%d BPM", int(math.Round(a.proj.G["tempo"])))
	if a.sq.playing && a.sq.posPat >= 0 {
		info = fmt.Sprintf("P%d  ·  BAR %d  ·  %s", a.sq.posPat+1, a.sq.posStep/16+1, info)
	}
	c.Text(310, 32, info, gfx.Label, uDim, gfx.Right|gfx.Mid)

	const gx0, gx1, gy0, gy1 = 12.0, 196.0, 46.0, 184.0 // the beat grid
	const lx = 306.0                                    // the lanes
	a.updateSpec()
	a.flowWash(c, 8, 42, 312, 188)
	// beat lines: one per beat of the playing loop, bar lines brighter
	n := 16
	if a.sq.playing && a.sq.posPat >= 0 {
		n = a.proj.Pat[a.sq.posPat].Len
	}
	for b := 0; b <= n; b += 4 {
		x := gx0 + (gx1-gx0)*float64(b)/float64(n)
		al := 0.07
		if b%16 == 0 {
			al = 0.16
		}
		c.Line(x, gy0, x, gy1, 0.7, 0xFFFFFF, al)
	}
	for y := gy0; y <= gy1; y += 15 { // faint dots between beats
		for x := gx0; x <= gx1; x += 11.5 {
			c.Circle(x, y, 0.5, 0xFFFFFF, 0.06)
		}
	}
	// the lanes: one per track, where the lines arrive
	laneY := func(t int) float64 { return gy0 + 8 + float64(t)*(gy1-gy0-16)/7 }
	for t := 0; t < seq.Tracks; t++ {
		col := tcol(t)
		al := 0.35
		if a.proj.Tracks[t].Muted {
			col, al = uFaint, 0.6
		}
		g := now.Sub(a.sq.flash[t])
		if g < 300*time.Millisecond && !a.proj.Tracks[t].Muted {
			c.Glow(lx, laneY(t), 11, col, 0.6*(1-float64(g)/float64(300*time.Millisecond)))
			al = 1
		}
		c.Circle(lx, laneY(t), 3.2, col, al)
	}
	// bubbles and their lines, oldest first so the newest sit on top
	px := func(h hit) float64 { return gx0 + (gx1-gx0)*h.pos }
	py := func(h hit) float64 {
		v := (float64(h.note) - 24) / 72
		return gy1 - 4 - math.Max(0, math.Min(1, v))*(gy1-gy0-8)
	}
	live := 0
	for _, h := range a.ui.hits {
		age := now.Sub(h.at)
		if age > hitLife {
			continue
		}
		live++
		f := 1 - float64(age)/float64(hitLife) // 1 new .. 0 gone
		col := tcol(h.track)
		x, y := px(h), py(h)
		// the line: a smooth S from the bubble to the track's lane
		ly := laneY(h.track)
		xs, ys := make([]float64, 0, 17), make([]float64, 0, 17)
		for i := 0; i <= 16; i++ {
			u := float64(i) / 16
			mu := 1 - u
			bx := mu*mu*mu*x + 3*mu*mu*u*(x+70) + 3*mu*u*u*(lx-70) + u*u*u*lx
			by := mu*mu*mu*y + 3*mu*mu*u*y + 3*mu*u*u*ly + u*u*u*ly
			xs, ys = append(xs, bx), append(ys, by)
		}
		c.Polyline(xs, ys, 0.9, col, 0.55*f*f)
		r := 1.8 + 5.5*h.vel*(0.55+0.45*f)
		if age < 250*time.Millisecond {
			c.Glow(x, y, r*2.6, col, 0.5*(1-float64(age)/float64(250*time.Millisecond)))
		}
		c.Circle(x, y, r, col, 0.25+0.7*f)
	}
	// the playhead
	if a.sq.playing && a.sq.posPat >= 0 && n > 0 {
		x := gx0 + (gx1-gx0)*(float64(a.sq.posStep)+0.5)/float64(n)
		c.Line(x, gy0, x, gy1, 1, uText, 0.6)
	}
	if live > 0 || a.sq.playing {
		a.animate(now.Add(80 * time.Millisecond))
	}
	if live == 0 && !a.sq.playing {
		c.Text((gx0+gx1)/2, (gy0+gy1)/2, "PLAY A PAD OR PRESS PLAY", gfx.Label, uFaint, gfx.Center|gfx.Mid)
	}
	a.knobFlag(c, gx0, gy0-4, lx-8, gy1)
	a.knobBars(c, 194)
	a.uiBottom(c, "ACROSS: THE LOOP  ·  UP: PITCH  ·  SIZE: VELOCITY  ·  16 LEVELS: BACK")
}

// flowWash: the watercolour layer under FLOW. Three soft hills of colour —
// lows (cyan) on the left, mids (orange) in the middle, highs (magenta) on
// the right — swell with the energy in their part of the spectrum and drift
// slowly, overlapping and blending where they meet. Flat translucent fills,
// so they stay smooth on the panel. The knob being turned deepens its hill.
func (a *app) flowWash(c *gfx.Canvas, x0, y0, x1, y1 float64) {
	sp := &a.ui.spec
	band := func(lo, hi int) float64 {
		sum := 0.0
		for b := lo; b <= hi; b++ {
			sum += math.Max(0, math.Min(1, (sp.show[b]+66)/60))
		}
		return sum / float64(hi-lo+1)
	}
	e := [3]float64{band(0, 12), band(13, 30), band(31, 47)}
	t := time.Since(appStart).Seconds()
	kk, env := a.turning()
	for k := 0; k < 3; k++ {
		cx := x0 + (x1-x0)*(0.2+0.3*float64(k)) + 18*math.Sin(t*0.3+float64(k)*2.1)
		width := 70 + 50*e[k]
		height := (y1 - y0) * (0.12 + 0.75*e[k])
		xs, ys := make([]float64, 0, 80), make([]float64, 0, 80)
		for i := 0; i <= 79; i++ {
			x := x0 + (x1-x0)*float64(i)/79
			d := (x - cx) / width
			hump := math.Exp(-d * d)
			ripple := 0.1 * math.Sin(x*0.05+t*(1.2+0.5*float64(k))+float64(k))
			ys = append(ys, y1-height*math.Max(0, hump+ripple*hump))
			xs = append(xs, x)
		}
		al := 0.16
		if k == kk {
			al += 0.18 * env
		}
		c.FillBetween(xs, ys, y1, knobCols[k], al)
		c.Polyline(xs, ys, 1.1, knobCols[k], 0.25+al)
	}
	a.animate(time.Now().Add(80 * time.Millisecond))
}
