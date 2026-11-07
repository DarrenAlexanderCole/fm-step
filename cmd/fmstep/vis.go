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
	"math"
	"time"

	"fmstep/internal/gfx"
)

type meltBlob struct{ x, y, vx, vy, r float64 }

type collagePiece struct {
	x, y, w, h float64
	col        uint32
	at         time.Time
	torn       float64
}

type visState struct {
	blobs   [6]meltBlob
	pieces  []collagePiece
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

func (a *app) drawMeltV2(c *gfx.Canvas) {
	v := &a.vis
	now := time.Now()
	dt := now.Sub(v.at).Seconds()
	if v.at.IsZero() || dt > 0.2 {
		dt = 0.05
		if v.blobs[0].r == 0 {
			for i := range v.blobs {
				ang := float64(i) / 6 * 2 * math.Pi
				v.blobs[i] = meltBlob{x: 160 + 70*math.Cos(ang), y: 132 + 40*math.Sin(ang), r: 16}
			}
		}
	}
	v.at = now
	bands := a.visBands()
	kk, env := a.turning()
	// notes kick the blob of their track
	newest := v.lastHit
	for _, h := range a.ui.hits {
		if h.at.After(v.lastHit) {
			b := &v.blobs[h.track%6]
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
	for i := range v.blobs {
		b := &v.blobs[i]
		ang := float64(i)/6*2*math.Pi + t*0.15
		hx, hy := 160+42*math.Cos(ang), 134+24*math.Sin(ang*1.3)
		b.vx += (hx - b.x) * 2.2 * dt
		b.vy += (hy - b.y) * 2.2 * dt
		for j := range v.blobs { // a little apart, so they melt rather than stack
			if j == i {
				continue
			}
			o := &v.blobs[j]
			dx, dy := b.x-o.x, b.y-o.y
			d2 := dx*dx + dy*dy + 30
			b.vx += dx / d2 * 120 * dt
			b.vy += dy / d2 * 120 * dt
		}
		b.vx *= math.Pow(0.25, dt)
		b.vy *= math.Pow(0.25, dt)
		b.x = math.Max(30, math.Min(290, b.x+b.vx*dt))
		b.y = math.Max(64, math.Min(206, b.y+b.vy*dt))
		want := 9 + 16*bands[i]
		if kk >= 0 && i%3 == kk {
			want += 10 * env
		}
		b.r += (want - b.r) * math.Min(1, dt*6)
	}
	c.Text(10, 33, "MELT", gfx.Big, uText, gfx.Left|gfx.Mid)
	const step = 3.0
	for y := 48.0; y < 220; y += step {
		for x := 4.0; x < 316; x += step {
			f := 0.0
			for i := range v.blobs {
				b := &v.blobs[i]
				dx, dy := x-b.x, y-b.y
				f += b.r * b.r / (dx*dx + dy*dy + 1)
			}
			if f < 0.6 {
				continue
			}
			if f < 0.75 {
				c.Rect(x, y, step, step, 0xFFFFFF, 0.18)
				continue
			}
			c.Rect(x, y, step, step, cvCol(x), math.Min(0.95, 0.35+(f-0.75)*0.8))
		}
	}
	a.animate(now.Add(60 * time.Millisecond))
}

func (a *app) drawCollageV2(c *gfx.Canvas) {
	v := &a.vis
	now := time.Now()
	if v.seed == 0 {
		v.seed = 0x9E3779B9
	}
	for _, h := range a.ui.hits {
		if !h.at.After(v.lastHit) {
			continue
		}
		w := 40 + 90*h.vel*v.rnd()
		hh := 26 + 60*v.rnd()
		v.pieces = append(v.pieces, collagePiece{
			x: 6 + v.rnd()*(308-w), y: 50 + v.rnd()*(166-hh), w: w, h: hh,
			col: tcol(h.track % 8), at: h.at, torn: v.rnd() * 6,
		})
	}
	for _, h := range a.ui.hits {
		if h.at.After(v.lastHit) {
			v.lastHit = h.at
		}
	}
	// keep the newest 14 and the ones still showing
	keep := v.pieces[:0]
	for _, p := range v.pieces {
		if now.Sub(p.at) < 5*time.Second {
			keep = append(keep, p)
		}
	}
	if len(keep) > 14 {
		keep = keep[len(keep)-14:]
	}
	v.pieces = keep
	c.Text(10, 33, "COLLAGE", gfx.Big, uText, gfx.Left|gfx.Mid)
	for _, p := range v.pieces {
		age := now.Sub(p.at).Seconds()
		al := math.Max(0, 1-age/5)
		c.Rect(p.x, p.y, p.w, p.h, p.col, 0.55*al)
		for x := p.x; x < p.x+p.w; x += 3 { // a torn top edge
			c.Circle(x, p.y+math.Sin(x*0.7+p.torn)*1.2, 1, p.col, 0.8*al)
		}
	}
	bands := a.visBands()
	for y := 52.0; y < 214; y += 6 {
		for x := 6.0; x < 314; x += 6 {
			b := bands[min(5, int(x/320*6))]
			v := 0.5 + 0.5*math.Sin(x/23+y/17+float64(now.UnixMilli()%100000)/900)
			c.Circle(x, y, 0.3+(0.8+1.2*b)*v, uBG, 0.35)
		}
	}
	if len(v.pieces) == 0 {
		c.Text(160, 132, "PLAY SOMETHING", gfx.Small, uDim, gfx.Center|gfx.Mid)
	}
	a.animate(now.Add(80 * time.Millisecond))
}
