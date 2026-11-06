package main

// DOTS: the third visualizer face. The mix as a dot matrix: one column per
// slice of the spectrum, low (left) to high (right), lit outward from the
// middle line, up and down by the same amount, to its level, in the knob colours by frequency zone (lows cyan, mids
// orange, highs magenta, blended where they meet). The top dot of each
// column fades in as the level climbs into it, so the columns rise and fall
// smoothly; a brighter dot holds each column's peak for a moment, and the
// knob being turned washes the lit dots in its colour.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
)

const (
	dotsCols  = 36
	dotsRows  = 16
	dotsX0    = 12.0
	dotsX1    = 308.0
	dotsY0    = 46.0
	dotsY1    = 178.0
	dotsDbMin = -72.0
	dotsDbMax = -6.0
)

// dotsLevel: the smoothed spectrum (or peak) sampled at column i, 0..1.
func dotsLevel(vals *[specBands]float64, i int) float64 {
	b := (float64(i) + 0.5) / dotsCols * specBands
	j := int(b - 0.5)
	t := b - 0.5 - float64(j)
	j = max(0, min(specBands-2, j))
	db := vals[j] + (vals[j+1]-vals[j])*math.Max(0, math.Min(1, t))
	return math.Max(0, math.Min(1, (db-dotsDbMin)/(dotsDbMax-dotsDbMin)))
}

// dotsColour: the zone colour of column i (by its centre frequency).
func dotsColour(i int) uint32 {
	f := specFreq((float64(i) + 0.5) / dotsCols * specBands)
	return gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], zoneW(f, 250)), knobCols[2], zoneW(f, 3500))
}

func (a *app) drawDotsV2(c *gfx.Canvas) {
	a.updateSpec()
	sp := &a.ui.spec
	c.Text(10, 31, "DOTS", gfx.Big, uText, gfx.Left|gfx.Mid)
	info := fmt.Sprintf("%d BPM", int(math.Round(a.proj.G["tempo"])))
	if a.sq.playing && a.sq.posPat >= 0 {
		info = fmt.Sprintf("P%d  ·  BAR %d  ·  %s", a.sq.posPat+1, a.sq.posStep/16+1, info)
	}
	c.Text(310, 32, info, gfx.Label, uDim, gfx.Right|gfx.Mid)

	kk, env := a.turning()
	// mirrored around the middle line: each column grows up and down from
	// the centre by the same amount, so it bounces symmetrically
	const half = dotsRows / 2
	pitchX := (dotsX1 - dotsX0) / dotsCols
	pitchY := (dotsY1 - dotsY0) / dotsRows
	mid := (dotsY0 + dotsY1) / 2
	r := math.Min(pitchX, pitchY) * 0.36
	c.Line(dotsX0, mid, dotsX1, mid, 1, uFaint, 0.5)
	for i := 0; i < dotsCols; i++ {
		x := dotsX0 + pitchX*(float64(i)+0.5)
		lvl := dotsLevel(&sp.show, i) * half
		pk := int(dotsLevel(&sp.peak, i)*half + 0.5)
		col := dotsColour(i)
		if kk >= 0 {
			col = gfx.Mix(col, knobCols[kk], 0.3*env)
		}
		for row := 0; row < half; row++ {
			on := math.Max(0, math.Min(1, lvl-float64(row))) // 0..1: the outer dot fades in
			off := pitchY * (float64(row) + 0.5)
			for _, y := range [2]float64{mid - off, mid + off} {
				switch {
				case on > 0:
					// brighter toward the outer edge of the lit column
					hot := 0.55 + 0.45*float64(row)/half
					c.Circle(x, y, r*(0.75+0.25*on), col, 0.25+0.75*on*hot)
				case row == pk-1 && pk > 0:
					c.Circle(x, y, r*0.85, col, 0.7) // peak hold
				default:
					c.Circle(x, y, r*0.55, uPanel2, 1) // unlit matrix
				}
			}
		}
	}
	// the knob being turned: a dot rides along the bottom, no chip
	a.knobDot(c, dotsX0, dotsX1, dotsY1+5)
	if a.sq.playing {
		a.animate(time.Now().Add(100 * time.Millisecond))
	}
	a.knobBars(c, 194)
	a.uiBottom(c, "LOW TO HIGH  ·  SIZE: LEVEL  ·  16 LEVELS: BACK")
}

// knobPos: where the knob being turned sits on [x0,x1] (frequency
// parameters on a log axis, the rest from minimum to maximum).
func (a *app) knobPos(kk int, x0, x1 float64) float64 {
	pr := a.knobParam(kk)
	if f, ok := a.paramFreq(pr.key); ok {
		return logX(math.Max(20, math.Min(20000, f)), x0, x1)
	}
	return x0 + (x1-x0)*pr.norm(a.val(pr.key))
}

// knobDot: a quiet version of the knob flag. A dot in the knob's colour
// slides along a faint line at y, with no chip, so it never covers what is
// above it (the big numbers on GLOBAL, the matrix on DOTS).
func (a *app) knobDot(c *gfx.Canvas, x0, x1, y float64) {
	kk, env := a.turning()
	if kk < 0 || env <= 0 {
		return
	}
	col := knobCols[kk]
	fx := a.knobPos(kk, x0, x1)
	c.Line(x0, y, x1, y, 1, col, 0.25*env)
	c.Line(x0, y, fx, y, 1.4, col, 0.7*env)
	c.Glow(fx, y, 7, col, 0.45*env)
	c.Circle(fx, y, 3, col, env)
}
