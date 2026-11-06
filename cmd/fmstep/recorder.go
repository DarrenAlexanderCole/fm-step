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
	h := (recCardY1-recCardY0)/2 - 8
	x0, x1 := recX0+4, recX1-4
	head := x0 + (x1-x0)*recHeadRecFrac
	win, t0 := recWindowSecs, s.secs-recWindowSecs/2
	if s.window > 0 {
		win = s.window
		if win >= s.total {
			win, t0 = s.total, 0
		} else {
			t0 = math.Max(0, math.Min(s.total-win, s.focusT-win/2))
		}
	}
	tx := func(t float64) float64 { return x0 + (t-t0)/win*(x1-x0) }
	if listen {
		head = tx(s.secs)
	}
	var xs, vs, past, future []float64
	for x := x0; x < x1; x++ {
		var v float64
		ap, af := 0.0, 0.0
		switch {
		case listen:
			t := t0 + (x-x0)/(x1-x0)*win
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
	_ = scale
	hh := h * math.Max(1, s.height)
	clip := func(y float64) float64 { return math.Max(recCardY0+3, math.Min(recCardY1-3, y)) }
	if len(s.lineMin) > 0 && listen { // 0.9.25: the real wave as a line (min / max per column)
		n := len(s.lineMin)
		for i := 0; i < n; i++ {
			x := x0 + (float64(i)+0.5)*(x1-x0)/float64(n)
			lo, hi := s.lineMin[i], s.lineMax[i]
			al, cl := 0.9*pa, uint32(0xE8E4D8)
			if x > head {
				al, cl = 0.32, 0xB0AEA6
			}
			c.Line(x, clip(mid-hi*hh), x, clip(mid-lo*hh)+0.6, 0.9, cl, al)
		}
		c.Line(x0, mid, x1, mid, 0.5, uFaint, 0.6)
	} else { // the level as a thin mirrored line (recording, or wide views)
		for i, x := range xs {
			v := vs[i]
			al, cl := 0.9*pa, uint32(0xE8E4D8)
			if future[i] > 0 {
				al, cl = 0.3, 0xB0AEA6
			}
			c.Line(x, clip(mid-v*hh), x, clip(mid+v*hh), 0.8, cl, al)
		}
	}
	_ = track
	_ = past
	if recording { // the empty tape ahead of the head
		for x := head + 6; x < x1-2; x += 6 {
			c.Circle(x, mid, 0.9, uDim, 0.6)
		}
	}
	if s.trimming { // IN / OUT: what's left out is veiled; the loop is a bracket in K2's colour
		ix, ox := tx(s.trimIn), tx(s.trimOut)
		if ix > x0 {
			c.Rect(x0, recCardY0+2, math.Min(ix, x1)-x0, recCardY1-recCardY0-4, uBG, 0.6)
		}
		if ox < x1 {
			c.Rect(math.Max(ox, x0), recCardY0+2, x1-math.Max(ox, x0), recCardY1-recCardY0-4, uBG, 0.6)
		}
		mark := func(x float64, lab string, kc uint32, left bool) {
			if x < x0-1 || x > x1+1 {
				return
			}
			c.Line(x, recCardY0+4, x, recCardY1-4, 1.4, kc, 1)
			w := gfx.Small.Width(lab) + 8
			bx := x
			if left {
				bx = x - w
			}
			c.RoundRect(bx, recCardY1-17, w, 12, 3, kc, 1)
			c.Text(bx+w/2, recCardY1-11, lab, gfx.Small, uBG, gfx.Center|gfx.Mid)
		}
		mark(ix, "IN", knobCols[0], false)
		mark(ox, "OUT", knobCols[2], true)
		if s.loop {
			la, lb := math.Max(x0, tx(s.loopA)), math.Min(x1, tx(s.loopB))
			if lb > la {
				c.RoundRect(la, recCardY0+2, lb-la, 3, 1.5, knobCols[1], 0.95)
			}
		}
	}
	// the head: a line in the state colour with a time chip on top, like a knob flag
	if head < x0-2 || head > x1+2 {
		head = -100 // out of the zoomed view
	}
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
	if s.trimming && s.total > 0 {
		sx := func(t float64) float64 { return sx0 + 4 + (sx1-sx0-8)*t/s.total }
		c.Line(sx(s.trimIn), recStripY-8, sx(s.trimIn), recStripY+8, 1.3, knobCols[0], 1)
		c.Line(sx(s.trimOut), recStripY-8, sx(s.trimOut), recStripY+8, 1.3, knobCols[2], 1)
		if s.loop {
			c.Line(sx(s.loopA), recStripY-8, sx(s.loopB), recStripY-8, 2, knobCols[1], 1)
		}
		if s.window > 0 && s.window < s.total { // where the zoomed card is
			w0 := math.Max(0, math.Min(s.total-s.window, s.focusT-s.window/2))
			c.RoundRectLine(sx(w0), recStripY-9, sx(w0+s.window)-sx(w0), 18, 3, 0.8, uText, 0.5)
		}
	}
	px := sx0 + 4 + (sx1-sx0-8)*frac
	c.Circle(px, recStripY, 4, col, 1)
	c.Circle(px, recStripY, 1.6, uBG, 1)

	// the time, large in dots, and what is left / the length on the right
	c.Dots(12, 172, clock(s.secs), dotGlyph, 2.2, uText, 0x26271F)
	supaRight(c, s, 177)
	// what K1-K3 do here, in the knob colours
	for k := 0; k < 3; k++ {
		x := 12 + float64(k)*100
		kc := knobCols[k]
		al := 0.75
		if k == s.knobHot {
			al = 1
			c.RoundRect(x-3, 193.5, 96, 13, 6.5, kc, 0.18)
		}
		c.RoundRect(x, 198.5, 6, 3, 1.5, kc, al)
		lw := c.Text(x+10, 200, s.knobs[k][0], gfx.Small, uDim, gfx.Left|gfx.Mid)
		c.Text(x+14+lw, 200, s.knobs[k][1], gfx.Small, uText, gfx.Left|gfx.Mid)
	}
}

// recTick keeps the recorder screen moving while something runs.
func (a *app) recTick(now time.Time) {
	if a.mt.rec || a.mt.playing || a.mt.stopping {
		a.animate(now.Add(120 * time.Millisecond))
	}
}
