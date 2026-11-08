package main

// Two more visualizers on 16 LEVELS (0.9.27).
//
//   MELT     six soft shapes that melt into each other: the sound's bands
//            swell them, notes kick them, a knob being turned grows the shape
//            in its colour. Simple physics: each one is pulled home and
//            pushed by the others.
//   COLLAGE  cut-out shapes in the track colours land with the notes and
//            fade, under a halftone that breathes with the spectrum.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/seq"
)

type meltBlob struct{ x, y, vx, vy, r float64 }

type collagePiece struct {
	x, y, w, h float64
	col        uint32
	at         time.Time
	torn       float64
}

type visState struct {
	blobs   [8]meltBlob
	pieces  []collagePiece
	bits    []shapeBit
	lastHit time.Time
	at      time.Time
	seed    uint32
}

func (v *visState) rnd() float64 {
	v.seed = v.seed*1664525 + 1013904223
	return float64(v.seed>>8) / float64(1<<24)
}

// band groups 0..1 from the spectrum (6 groups, low to high)
func (a *app) visBands() [6]float64 {
	a.updateSpec()
	var g [6]float64
	sp := &a.ui.spec
	for i := 0; i < 6; i++ {
		m := -90.0
		for b := i * specBands / 6; b < (i+1)*specBands/6; b++ {
			m = math.Max(m, sp.show[b])
		}
		g[i] = math.Max(0, math.Min(1, (m+60)/54))
	}
	return g
}

// ---------------------------------------------------------------- 0.9.28

const visOrange = 0xE8582A

// the page's K1-K3 as 0..1 (the selected track's sound page, or its mixer knobs)
func (a *app) visKnobs() [3]float64 {
	var n [3]float64
	for k := 0; k < 3; k++ {
		var pr *param
		if seq.IsSample(a.proj.Track) {
			pr = []*param{&mixParams[0], &pSendALvl, &pSendBLvl}[k]
		} else {
			pr = a.cur(k)
		}
		if pr != nil && pr.key != "" {
			n[k] = clamp01(pr.norm(a.val(pr.key)))
		}
	}
	return n
}

// MELT: the knobs reshape it. K1 how the shapes flow together (and how far
// apart they orbit), K2 how many and how big, K3 the texture inside (dots,
// lines, checks). The sound swells them, notes kick them.
func (a *app) drawMeltV2(c *gfx.Canvas) {
	v := &a.vis
	now := time.Now()
	dt := now.Sub(v.at).Seconds()
	if v.at.IsZero() || dt > 0.2 {
		dt = 0.05
	}
	v.at = now
	kn := a.visKnobs()
	bands := a.visBands()
	n := 3 + int(math.Round(kn[1]*5))
	newest := v.lastHit
	for _, h := range a.ui.hits {
		if h.at.After(v.lastHit) {
			b := &v.blobs[h.track%n]
			ang := float64(h.note) * 0.7
			b.vx += math.Cos(ang) * 50 * h.vel
			b.vy += math.Sin(ang) * 35 * h.vel
			if h.at.After(newest) {
				newest = h.at
			}
		}
	}
	v.lastHit = newest
	t := float64(now.UnixMilli()%600000) / 1000
	orbit := 22 + 46*(1-kn[0])
	for i := 0; i < n; i++ {
		b := &v.blobs[i]
		if b.r == 0 {
			b.x, b.y, b.r = 160, 118, 10
		}
		ang := float64(i)/float64(n)*2*math.Pi + t*0.15
		hx, hy := 160+orbit*1.3*math.Cos(ang), 118+orbit*0.7*math.Sin(ang*1.2)
		b.vx += (hx - b.x) * 2.2 * dt
		b.vy += (hy - b.y) * 2.2 * dt
		b.vx *= math.Pow(0.25, dt)
		b.vy *= math.Pow(0.25, dt)
		b.x = math.Max(24, math.Min(296, b.x+b.vx*dt))
		b.y = math.Max(56, math.Min(180, b.y+b.vy*dt))
		want := (10 + 12*kn[1]) * (0.75 + 0.5*bands[i%6])
		b.r += (want - b.r) * math.Min(1, dt*6)
	}
	c.Text(10, 33, "MELT", gfx.Big, uText, gfx.Left|gfx.Mid)
	thr := 0.4 + 0.8*(1-kn[0])
	const step = 2.0
	for y := 46.0; y < 188; y += step {
		for x := 4.0; x < 316; x += step {
			f := 0.0
			for i := 0; i < n; i++ {
				b := &v.blobs[i]
				dx, dy := x-b.x, y-b.y
				f += b.r * b.r / (dx*dx + dy*dy + 1)
			}
			if f < thr {
				continue
			}
			show := true
			switch {
			case kn[2] < 0.33:
				show = int(x/2+y/2)%2 == 0 || f > thr*2.5
			case kn[2] < 0.66:
				show = int(y/2)%3 != 0
			default:
				show = (int(x/6)+int(y/6))%2 == 0
			}
			if !show {
				continue
			}
			col := uint32(visOrange)
			if f > thr*2.2 {
				col = 0x161616
			}
			c.Rect(x, y, step, step, gfx.Mix(col, cvCol(x), 0.25), 0.9)
		}
	}
	labels, vals := [3]string{"FLOW", "HOW MANY", "TEXTURE"}, [3]string{fmt.Sprint(int(kn[0] * 99)), fmt.Sprint(n), []string{"DOTS", "LINES", "CHECKS"}[min(2, int(kn[2]*3))]}
	mockKnobs(c, labels, vals, kn, -1)
	a.uiBottom(c, "K1-K3 TURN THE SOUND AND THE SHAPE  ·  16 LEVELS: NEXT")
	a.animate(now.Add(60 * time.Millisecond))
}

// 0.9.29 SHAPES (was COLLAGE): five shapes, one per frequency band, low to
// high: circle (sub), triangle (lows), square (low mids), diamond (high
// mids), bar (highs). Each lives in its own lane: its band lifts it off the
// floor and swells it, an onset in its band kicks it up, gravity brings it
// back down with a bounce. The angular ones turn while their band is loud.
type shapeBit struct {
	y, vy, s, rot, prev float64
}

// visBands5: the spectrum in five groups, low to high
func (a *app) visBands5() [5]float64 {
	a.updateSpec()
	var g [5]float64
	sp := &a.ui.spec
	for i := 0; i < 5; i++ {
		m := -90.0
		for b := i * specBands / 5; b < (i+1)*specBands/5; b++ {
			m = math.Max(m, sp.show[b])
		}
		g[i] = math.Max(0, math.Min(1, (m+60)/54))
	}
	return g
}

const shapesFloor, shapesTop = 164.0, 52.0 // where a shape's middle rests / can reach

func (a *app) drawCollageV2(c *gfx.Canvas) {
	v := &a.vis
	now := time.Now()
	dt := now.Sub(v.at).Seconds()
	if v.at.IsZero() || dt > 0.2 {
		dt = 0.05
	}
	v.at = now
	if len(v.bits) != 5 {
		v.bits = make([]shapeBit, 5)
		for i := range v.bits {
			v.bits[i] = shapeBit{y: shapesFloor, s: 14}
		}
	}
	bands := a.visBands5()
	for i := range v.bits {
		shapesStep(&v.bits[i], bands[i], dt)
	}
	drawShapes(c, v.bits)
	a.uiBottom(c, "ONE SHAPE PER BAND  ·  LOWS LEFT, HIGHS RIGHT  ·  16 LEVELS: NEXT")
	a.animate(now.Add(50 * time.Millisecond))
}

// shapesStep: one shape's physics for dt seconds with its band at e (0..1).
// A spring pulls it to its band's height (silence = the floor); an onset
// kicks it higher and it falls back with a bounce on the floor.
func shapesStep(b *shapeBit, e, dt float64) {
	rise := e - b.prev
	b.prev = e
	if rise > 0.05 { // an onset in its band: a kick up
		b.vy -= 120 + 500*rise
	}
	home := shapesFloor - e*80
	b.vy += 90 * (home - b.y) * dt // spring to its band's height
	b.vy *= math.Exp(-5 * dt)      // air
	b.y += b.vy * dt
	size := 14 + 10*e
	b.s += (size - b.s) * math.Min(1, dt*10)
	if b.y > shapesFloor {
		b.y, b.vy = shapesFloor, -b.vy*0.45
		if math.Abs(b.vy) < 15 {
			b.vy = 0
		}
	}
	if top := shapesTop + 2*b.s - 22; b.y < top {
		b.y, b.vy = top, math.Abs(b.vy)*0.3
	}
	b.rot += e * e * 4 * dt
}

func drawShapes(c *gfx.Canvas, bits []shapeBit) {
	c.Text(10, 33, "SHAPES", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.RoundRect(8, 44, 304, 146, 6, 0xEFE9DF, 0.96)
	for y := 48.0; y < 188; y += 5 { // the halftone paper
		for x := 12.0; x < 310; x += 5 {
			hv := 0.5 + 0.5*math.Sin(x/31+y/19)
			c.Circle(x, y, 0.3+0.5*hv, 0x1A1A1A, 0.1)
		}
	}
	const fy = 186.0
	c.Line(16, fy, 304, fy, 1, 0x1A1A1A, 0.25) // the floor
	for i, b := range bits {
		x := 8 + 304*(float64(i)+0.5)/5
		col := uint32(0x161616)
		if i%2 == 0 {
			col = visOrange
		}
		lift := math.Max(0, math.Min(1, (shapesFloor-b.y)/(shapesFloor-shapesTop)))
		c.Rect(x-b.s*(1-0.5*lift), fy-1, 2*b.s*(1-0.5*lift), 2, 0x1A1A1A, 0.25*(1-lift)+0.05) // its shadow
		c.Circle(x, fy, 1.8, 0x1A1A1A, 0.6)                                                   // its node
		drawShape(c, i, x, b.y+22-b.s, b.s, b.rot, col)                                       // resting, it sits on the floor
	}
}

func drawShape(c *gfx.Canvas, kind int, x, y, s, rot float64, col uint32) {
	poly := func(n int, r, a0 float64) {
		xs, ys := make([]float64, n), make([]float64, n)
		for k := 0; k < n; k++ {
			a := a0 + rot + 2*math.Pi*float64(k)/float64(n)
			xs[k], ys[k] = x+r*math.Sin(a), y-r*math.Cos(a)
		}
		c.Poly(xs, ys, col, 1)
	}
	switch kind {
	case 0:
		c.Circle(x, y, s, col, 1)
	case 1:
		poly(3, s*1.2, 0)
	case 2:
		poly(4, s*1.15, math.Pi/4)
	case 3:
		poly(4, s*1.2, 0)
	default: // the bar: wide and thin, it tilts
		ca, sa := math.Cos(rot), math.Sin(rot)
		w, h := s*1.3, s*0.28
		xs := []float64{x + (-w*ca - -h*sa), x + (w*ca - -h*sa), x + (w*ca - h*sa), x + (-w*ca - h*sa)}
		ys := []float64{y + (-w*sa + -h*ca), y + (w*sa + -h*ca), y + (w*sa + h*ca), y + (-w*sa + h*ca)}
		c.Poly(xs, ys, col, 1)
	}
}
