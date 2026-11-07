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
	prevB   [6]float64
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

type shapeBit struct {
	x, y, vx, vy, s float64
	kind            int
}

// COLLAGE: small shapes on a paper card. Gravity pulls them down; each kind
// rises with its own frequency band (triangles the lows ... dashes the highs)
// and bounces when it lands.
func (a *app) drawCollageV2(c *gfx.Canvas) {
	v := &a.vis
	now := time.Now()
	dt := now.Sub(v.at).Seconds()
	if v.at.IsZero() || dt > 0.2 {
		dt = 0.05
	}
	v.at = now
	if v.seed == 0 {
		v.seed = 0x9E3779B9
	}
	if len(v.bits) == 0 {
		for i := 0; i < 66; i++ {
			v.bits = append(v.bits, shapeBit{x: 16 + v.rnd()*288, y: 60 + v.rnd()*120, s: 2.5 + v.rnd()*3, kind: i % 6})
		}
	}
	bands := a.visBands()
	const floor, top = 184.0, 50.0
	for i := range v.bits {
		b := &v.bits[i]
		e := bands[b.kind]
		rise := e - v.prevB[b.kind]
		if rise > 0.04 && b.y > floor-30 { // an onset in its band: a kick upward
			b.vy -= (120 + 260*rise) * (0.6 + 0.4*v.rnd())
			b.vx += (v.rnd() - 0.5) * 60
		}
		b.vy += (380 - 300*e) * dt // gravity, lighter while its band is loud
		b.x += b.vx * dt
		b.y += b.vy * dt
		b.vx *= math.Pow(0.6, dt)
		if b.y > floor-b.s {
			b.y, b.vy = floor-b.s, -b.vy*0.45
			if math.Abs(b.vy) < 12 {
				b.vy = 0
			}
		}
		if b.y < top+b.s {
			b.y, b.vy = top+b.s, math.Abs(b.vy)*0.5
		}
		if b.x < 14 || b.x > 306 {
			b.vx = -b.vx
			b.x = math.Max(14, math.Min(306, b.x))
		}
	}
	v.prevB = bands
	c.Text(10, 33, "COLLAGE", gfx.Big, uText, gfx.Left|gfx.Mid)
	names := []string{"TRI", "SQR", "CIRC", "DIAM", "DOT", "DASH"}
	for i, nm := range names {
		col := uint32(uDim)
		if bands[i] > 0.5 {
			col = uText
		}
		c.Text(122+float64(i)*32, 34, nm, gfx.Small, col, gfx.Left|gfx.Mid)
	}
	c.RoundRect(8, 44, 304, 146, 6, 0xEFE9DF, 0.96)
	for y := 48.0; y < 188; y += 5 {
		for x := 12.0; x < 310; x += 5 {
			hv := 0.5 + 0.5*math.Sin(x/31+y/19)
			c.Circle(x, y, 0.3+0.5*hv, 0x1A1A1A, 0.1)
		}
	}
	for _, b := range v.bits {
		col := uint32(0x161616)
		if b.kind%2 == 0 {
			col = visOrange
		}
		x, y, s := b.x, b.y, b.s
		switch b.kind {
		case 0:
			c.Poly([]float64{x, x + s, x - s}, []float64{y - s, y + s, y + s}, col, 1)
		case 1:
			c.Rect(x-s, y-s, 2*s, 2*s, col, 1)
		case 2:
			c.Circle(x, y, s, col, 1)
		case 3:
			c.Poly([]float64{x, x + s, x, x - s}, []float64{y - s, y, y + s, y}, col, 1)
		case 4:
			c.Circle(x, y, 1.4, col, 1)
		case 5:
			c.Line(x-s, y, x+s, y, 1.6, col, 1)
		}
	}
	a.uiBottom(c, "LOWS LIFT THE TRIANGLES ... HIGHS THE DASHES  ·  16 LEVELS: NEXT")
	a.animate(now.Add(50 * time.Millisecond))
}
