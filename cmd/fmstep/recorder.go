package main

// drawRecorder (0.9.23): the MIXTAPE screen in FM-STEP's own UI language,
// built from the existing views (Darren: "defined, elegant, translucent
// layering"; no reels, no cassette). A title row like the sound pages, a
// rounded card holding the wave as translucent layers around the head, the
// whole take as a slim strip like the LIBRARY window, the dot clock, meters.

import (
	"math"
	"time"

	"fmstep/internal/gfx"
)

const (
	recX0, recX1   = 8.0, 312.0
	recCardY0      = 66.0
	recCardY1      = 150.0
	recStripY      = 163.0
	recWindowSecs  = 8.0 // the card shows this much of the take around the head
	recHeadRecFrac = 0.78
)

// recColours: the state's colour (chip, head, strip).
func recColour(s *supa) uint32 {
	switch s.state {
	case supaRec, supaRecPaused:
		return uRed
	case supaListen, supaListenPaused:
		return uLime
	}
	return uDim
}

// recChip: the state as a translucent chip on the right of the title row,
// like the fader-effect chip on the sound pages.
func recChip(c *gfx.Canvas, s *supa, blink bool) {
	col := recColour(s)
	var lab string
	ic := icNone
	switch s.state {
	case supaRec:
		lab = "REC"
		if blink {
			ic = icRec
		}
	case supaRecPaused:
		lab, ic = "PAUSED", icPause
	case supaListen:
		lab, ic = "LISTEN", icPlay
	case supaListenPaused:
		lab, ic = "PAUSED", icPause
	default:
		lab = "STOPPED"
	}
	w := gfx.Small.Width(lab) + 22
	x := 312 - w
	c.RoundRect(x, 49.5, w, 14, 7, col, 0.22)
	c.RoundRectLine(x, 49.5, w, 14, 7, 0.8, col, 0.55)
	switch ic {
	case icRec:
		c.Circle(x+9, 56.5, 3, uRed, 1)
	case icPause:
		drawIcon(c, icPause, x+5.5, 56.5, col)
	case icPlay:
		drawIcon(c, icPlay, x+6, 56.5, col)
	}
	tx := x + 17
	if ic == icNone && s.state != supaRec {
		tx = x + 11
	}
	c.Text(tx, 56.5, lab, gfx.Small, uText, gfx.Left|gfx.Mid)
}

// recLayer draws one translucent layer of the wave in the card: height
// scale k of the level, in col.
func recLayer(c *gfx.Canvas, xs []float64, vs []float64, mid, h, k float64, col uint32, alpha []float64) {
	for i, x := range xs {
		v := vs[i]
		if v <= 0 || alpha[i] <= 0 {
			continue
		}
		c.Line(x, mid-v*h*k, x, mid+v*h*k, 1, col, alpha[i])
	}
}

func drawRecorder(c *gfx.Canvas, s *supa, blink bool, track uint32) {
	col := recColour(s)
	listen := s.state == supaListen || s.state == supaListenPaused || (s.state == supaIdle && s.total > 0)
	recording := s.state == supaRec || s.state == supaRecPaused
	paused := s.state == supaRecPaused || s.state == supaListenPaused

	// title row, like the sound pages: big word, the take beside it, the state chip
	w := c.Text(10, 57, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(10+w+7, 58.5, s.name, gfx.Small, uDim, gfx.Left|gfx.Mid)
	recChip(c, s, blink)

	// the card: dot grid, then the wave as translucent layers around the head
	c.RoundRect(recX0, recCardY0, recX1-recX0, recCardY1-recCardY0, 10, uPanel, 1)
	for y := recCardY0 + 8; y < recCardY1-4; y += 8 {
		for x := recX0 + 8; x < recX1-4; x += 8 {
			c.Circle(x, y, 0.55, uFaint, 0.55)
		}
	}
	mid := (recCardY0 + recCardY1) / 2
	h := (recCardY1 - recCardY0)/2 - 8
	x0, x1 := recX0+4, recX1-4
	head := x0 + (x1-x0)*recHeadRecFrac
	if listen {
		head = (x0 + x1) / 2
	}
	var xs, vs, past, future []float64
	for x := x0; x < x1; x++ {
		var v float64
		ap, af := 0.0, 0.0
		switch {
		case listen:
			t := s.secs + (x-head)/(x1-x0)*recWindowSecs
			if s.total <= 0 || t < 0 || t > s.total {
				continue
			}
			v = s.envAt(t / s.total)
			if x <= head {
				ap = 1
			} else {
				af = 1
			}
		case recording && x <= head:
			v = s.recentAt((x - x0) / (head - x0))
			ap = 1
		default:
			continue
		}
		xs = append(xs, x+0.5)
		vs = append(vs, v)
		past = append(past, ap)
		future = append(future, af)
	}
	scale := func(a []float64, k float64) []float64 {
		o := make([]float64, len(a))
		for i, x := range a {
			o[i] = x * k
		}
		return o
	}
	pa := 1.0
	if paused {
		pa = 0.55
	}
	// played: three overlapping translucent layers (wide magenta, track colour, a bright core)
	recLayer(c, xs, vs, mid, h, 1.0, knobCols[2], scale(past, 0.30*pa))
	recLayer(c, xs, vs, mid, h, 0.72, track, scale(past, 0.45*pa))
	recLayer(c, xs, vs, mid, h, 0.38, uText, scale(past, 0.55*pa))
	// still to come (listening): the same shape, faint
	recLayer(c, xs, vs, mid, h, 1.0, knobCols[2], scale(future, 0.10))
	recLayer(c, xs, vs, mid, h, 0.72, track, scale(future, 0.16))
	if recording { // the empty tape ahead of the head
		for x := head + 6; x < x1-2; x += 6 {
			c.Circle(x, mid, 0.9, uDim, 0.6)
		}
	}
	// the head: a line in the state colour with a time chip on top, like a knob flag
	c.Glow(head, mid, 18, col, 0.12)
	c.Line(head, recCardY0+18, head, recCardY1-4, 1.3, col, 0.95)
	c.Circle(head, recCardY1-4, 2.4, col, 1)
	lab := clock(s.secs)
	cw := gfx.Small.Width(lab) + 10
	cx := math.Max(recX0+4, math.Min(recX1-4-cw, head-cw/2))
	c.RoundRect(cx, recCardY0+5, cw, 12, 6, col, 1)
	c.Poly([]float64{head - 3, head + 3, head}, []float64{recCardY0 + 16.5, recCardY0 + 16.5, recCardY0 + 20}, col, 1)
	c.Text(cx+cw/2, recCardY0+11, lab, gfx.Small, uBG, gfx.Center|gfx.Mid)
	if paused { // a quiet veil and the pause bars
		c.RoundRect(recX0, recCardY0, recX1-recX0, recCardY1-recCardY0, 10, uBG, 0.32)
		c.RoundRect(150, mid-13, 7, 26, 2, uText, 0.8)
		c.RoundRect(163, mid-13, 7, 26, 2, uText, 0.8)
	}
	if !listen && !recording {
		c.Text(160, mid, "SAMPLE RECORD  STARTS A TAKE", gfx.Label, uDim, gfx.Center|gfx.Mid)
	}

	// the whole take: a slim track like the LIBRARY window, filled as it runs
	frac := s.tapeFrac()
	const sx0, sx1 = recX0 + 2, recX1 - 2
	c.RoundRect(sx0, recStripY-7, sx1-sx0, 14, 7, uPanel2, 0.8)
	for x := sx0 + 4; x < sx1-4; x++ {
		f := (x - sx0 - 4) / (sx1 - sx0 - 8)
		var v, a float64
		switch {
		case listen:
			v = s.envAt(f)
			a = 0.28
			if f <= frac {
				a = 0.9
			}
		case recording && frac > 0 && f <= frac:
			v, a = s.envAt(f/frac), 0.9
		default:
			continue
		}
		c.Line(x+0.5, recStripY-1-v*5, x+0.5, recStripY+1+v*5, 1, track, a)
	}
	px := sx0 + 4 + (sx1-sx0-8)*frac
	c.Circle(px, recStripY, 4, col, 1)
	c.Circle(px, recStripY, 1.6, uBG, 1)

	// the time, large in dots, and what is left / the length on the right
	c.Dots(12, 176, clock(s.secs), dotGlyph, 2.4, uText, 0x26271F)
	supaRight(c, s, 182)
}

// recTick keeps the recorder screen moving while something runs.
func (a *app) recTick(now time.Time) {
	if a.mt.rec || a.mt.playing || a.mt.stopping {
		a.animate(now.Add(120 * time.Millisecond))
	}
}

