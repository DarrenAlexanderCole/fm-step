package main

// SLICE as a zoom trim (0.9.23), after Logic / DaVinci Resolve. The view
// follows the marker you turn: the wave scrolls under it, and K3 zooms from
// the whole buffer down to one sample per pixel. Turning slowly moves one
// pixel at a time, so zoomed in that is sample by sample; turning fast jumps.
//
//   K1 IN · K2 OUT · K3 ZOOM          SHIFT: PITCH · REVERSE
//   left: ▶ FROM IN (play the slice) · right: UP TO OUT ▶ (the last 0.6 s)
//   middle: back to CHOP · DATA / pads: pick the slice · fader: scrub
//
// The engine draws the window (GZM: min / max per column of the real
// samples), so what you see at high zoom is the actual wave.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
)

const trimCols = 296

// trimWindow: the stretch of the buffer on screen (start, length in frames).
func (a *app) trimWindow(n, s, e int) (int, int) {
	W := a.smp.zoomW
	m := s
	if a.smp.focus == 1 {
		m = e
	}
	if W <= 0 { // not zoomed yet: the slice with some room either side
		W = (e - s) * 16 / 10
		m = (s + e) / 2
	}
	W = max(min(trimCols, n), min(n, W))
	w0 := max(0, min(n-W, m-W/2))
	return w0, W
}

// smpTrimKnob: K1-K3 in SLICE.
func (a *app) smpTrimKnob(k, delta int) {
	if delta > 64 { // endless knobs wrap 127 -> 0
		delta -= 128
	} else if delta < -64 {
		delta += 128
	}
	n := a.dsp.smp.grabLen
	if n <= 0 {
		return
	}
	i := a.smp.sel
	ed := &a.smp.edits[i]
	s, e, _, _ := a.smpSlice(i, n)
	bs, be := a.smpBase(i, n)
	if e <= s {
		return
	}
	_, W := a.trimWindow(n, s, e)
	px := float64(W) / trimCols
	step := px * float64(delta)
	if abs(delta) >= 3 { // fast: jumps
		step *= float64(abs(delta)) * 2
	}
	mv := int(math.Round(step))
	if mv == 0 {
		mv = int(math.Copysign(1, float64(delta)))
	}
	switch {
	case a.shift && k == 0:
		if st := a.smpStep(k, delta); st != 0 {
			ed.pitch = max(-12, min(12, ed.pitch+st))
		}
	case a.shift && k == 1:
		if st := a.smpStep(k, delta); st != 0 {
			ed.rev = !ed.rev
		}
	case k == 2:
		if a.smp.zoomW <= 0 {
			a.smp.zoomW = W
			a.smp.focus = 0
		}
		a.smp.zoomW = int(float64(a.smp.zoomW) * math.Pow(1.12, -float64(delta)))
		a.smp.zoomW = max(min(trimCols, n), min(n, a.smp.zoomW))
	case k == 0 && a.smp.chop == chopMan:
		a.smpTrimFocus(0, W)
		lo, hi := 0, n-256
		if i > 0 {
			lo = a.smp.marks[i-1] + 256
		}
		if i < 15 {
			hi = a.smp.marks[i+1] - 256
		}
		a.smp.marks[i] = max(lo, min(hi, a.smp.marks[i]+mv))
	case k == 0:
		a.smpTrimFocus(0, W)
		ns := max(0, min(e-128, s+mv))
		ed.startFr = ns - bs
	case k == 1:
		a.smpTrimFocus(1, W)
		ne := max(s+128, min(n, e+mv))
		ed.endFr = ne - be
	}
	a.smpSync(true)
	a.dirty = true
}

// smpTrimFocus: the view now follows this marker (keeping the zoom you see).
func (a *app) smpTrimFocus(f, W int) {
	if a.smp.zoomW <= 0 {
		a.smp.zoomW = W
	}
	a.smp.focus = f
}

func (a *app) trimH() float64 {
	if a.smp.zh < 1 {
		return 1
	}
	return a.smp.zh
}

// smpPreview: left plays the slice from IN; right plays up to OUT.
func (a *app) smpPreview(toOut bool) {
	n := a.dsp.smp.grabLen
	i := a.smp.sel
	s, e, p, rev := a.smpSlice(i, n)
	if n <= 0 || e <= s {
		return
	}
	if !toOut {
		a.dsp.send("SL %d 1 0 0", i)
	} else {
		a.dsp.send("SLC 31 %d %d %d %d", max(s, e-28800), e, p, b2i(rev))
		a.dsp.send("SL 31 1 0 0")
	}
	a.smp.hitAt[i] = time.Now()
	a.animate(time.Now().Add(700 * time.Millisecond))
}

func (a *app) drawSliceView(c *gfx.Canvas) {
	i := a.smp.sel
	d := &a.dsp.smp
	d.mu.Lock()
	n := d.grabLen
	gw := d.gw
	gzm, gzS, gzE := d.gzm, d.gzmStart, d.gzmEnd
	d.mu.Unlock()
	if n <= 0 {
		return
	}
	supaButtons(c, [3]supaBtn{{icon: icText, text: "▶ FROM IN"}, {icon: icText, text: "CHOP"}, {icon: icText, text: "UP TO OUT ▶"}})
	if i != a.smp.trimSel { // a new slice: start from its overview
		a.smp.trimSel, a.smp.zoomW, a.smp.focus = i, 0, 0
	}
	s, e, p, rev := a.smpSlice(i, n)
	if e <= s {
		c.Text(160, 110, fmt.Sprintf("NO SLICE ON PAD %d", i+1), gfx.Body, uDim, gfx.Center|gfx.Mid)
		a.uiBottom(c, "DATA OR PADS: PICK A SLICE  ·  MIDDLE: CHOP")
		return
	}
	w0, W := a.trimWindow(n, s, e)
	if key := w0*131 + W; key != a.smp.gzmKey {
		a.smp.gzmKey = key
		a.dsp.send("GZM %d %d", w0, w0+W)
	}
	// overview: the whole buffer, this slice bright, the window boxed
	const ox0, ox1, oy = 10.0, 310.0, 52.0
	X := func(f int) float64 { return ox0 + (ox1-ox0)*float64(f)/float64(n) }
	levelWave(c, ox0, ox1, oy, 7, gw[:], 0x6A3A55, 0x56632E)
	p0, p1 := s*256/n, max(s*256/n+1, int(math.Ceil(float64(e)*256/float64(n))))
	levelWave(c, X(s), X(e), oy, 7, gw[max(0, p0):min(256, p1)], knobCols[2], uLime)
	for k := 0; k < 16; k++ {
		if bs, be := a.smpBase(k, n); be > bs {
			c.Line(X(bs), oy-9, X(bs), oy+9, 0.6, 0xFFFFFF, 0.2)
		}
	}
	c.RoundRectLine(X(w0)-1, oy-11, math.Max(3, X(w0+W)-X(w0)+2), 22, 2, 1.2, uText, 0.9)
	// the zoomed wave: the real samples (min / max per column)
	const zx0, zy, zh = 12.0, 112.0, 38.0
	h := zh * a.trimH()
	c.RoundRect(10, 66, 300, 92, 4, uPanel, 1)
	for x := 0; x < trimCols; x++ {
		t := w0 + int(float64(x)*float64(W)/trimCols)
		if gzE <= gzS || t < gzS || t >= gzE {
			continue
		}
		j := (t - gzS) * trimCols / (gzE - gzS)
		j = max(0, min(trimCols-1, j))
		lo, hi := float64(gzm[j][0])/127, float64(gzm[j][1])/127
		y0 := math.Max(68, math.Min(156, zy-hi*h))
		y1 := math.Max(68, math.Min(156, zy-lo*h))
		if y1-y0 < 1 {
			y1 = y0 + 1
		}
		col := uint32(0x56632E)
		if t >= s && t < e {
			col = uLime
		}
		c.Line(zx0+float64(x)+0.5, y0, zx0+float64(x)+0.5, y1, 1, col, 1)
	}
	c.Line(12, zy, 308, zy, 0.6, uDim, 0.5)
	ZX := func(f int) float64 { return zx0 + float64(f-w0)*trimCols/float64(W) }
	// playhead while the slice sounds
	if pr := a.slicePlaying(i); pr >= 0 {
		ph := s + int(pr*float64(e-s))
		if rev {
			ph = e - int(pr*float64(e-s))
		}
		if x := ZX(ph); x >= 12 && x <= 308 {
			c.Line(x, 70, x, 154, 1, uText, 0.8)
		}
		a.animate(time.Now().Add(100 * time.Millisecond))
	}
	// the IN and OUT flags (an arrow at the edge when off screen)
	for m, pos := range []int{s, e} {
		col := knobCols[m]
		lab := fmt.Sprintf("IN %.3f S", float64(s)/48000)
		if m == 1 {
			lab = fmt.Sprintf("OUT %.3f S", float64(e)/48000)
		}
		x := ZX(pos)
		switch {
		case x < 12:
			c.Poly([]float64{14, 20, 20}, []float64{74, 70, 78}, col, 1)
		case x > 308:
			c.Poly([]float64{306, 300, 300}, []float64{74, 70, 78}, col, 1)
		default:
			c.Line(x, 70, x, 154, 1.3, col, 1)
			c.Poly([]float64{x, x + 8, x + 8, x}, []float64{70, 72, 77, 79}, col, 1)
			if m == a.smp.focus || a.smp.zoomW <= 0 {
				py := 86.0 + float64(m)*14
				if x > 230 {
					pill(c, x-4, py, lab, col, true)
				} else {
					pill(c, x+4, py, lab, col, false)
				}
			}
		}
	}
	// the ruler: seconds into the buffer
	for k := 0; k <= 4; k++ {
		x := 12 + 296*float64(k)/4
		c.Line(x, 160, x, 163, 0.8, uDim, 0.8)
		t := float64(w0+W*k/4) / 48000
		lab := fmt.Sprintf("%.2f", t)
		if W < 4800 {
			lab = fmt.Sprintf("%.3f", t)
		}
		al := gfx.Center
		if k == 0 {
			al = gfx.Left
		} else if k == 4 {
			al = gfx.Right
		}
		c.Text(x, 169, lab, gfx.Small, uFaint, al|gfx.Mid)
	}
	// what this slice is, and the zoom
	info := fmt.Sprintf("SLICE %d  ·  %.2f S", i+1, float64(e-s)/48000)
	if p != 0 {
		info += fmt.Sprintf("  ·  %+d ST", p)
	}
	if rev {
		info += "  ·  REVERSED"
	}
	c.Text(10, 181, info, gfx.Label, smpCol(i), gfx.Left|gfx.Mid)
	zoom := "WHOLE"
	if W < n {
		zoom = fmt.Sprintf("x%.0f", float64(n)/float64(W))
	}
	zshort := zoom
	if W <= trimCols {
		zoom += "  ·  1 SAMPLE / PX"
	}
	c.Text(310, 181, "ZOOM "+zoom, gfx.Small, uDim, gfx.Right|gfx.Mid)
	if a.shift {
		mockKnobs(c, [3]string{"PITCH", "REVERSE", ""},
			[3]string{fmt.Sprintf("%+d ST", p), map[bool]string{true: "ON", false: "OFF"}[rev], ""},
			[3]float64{float64(p+12) / 24, boolF(rev), 0}, a.smpActiveKnob())
	} else {
		k1 := "IN"
		if a.smp.chop == chopMan {
			k1 = "MARKER"
		}
		zn := 0.0
		if n > trimCols {
			zn = math.Log(float64(n)/float64(W)) / math.Log(float64(n)/trimCols)
		}
		mockKnobs(c, [3]string{k1, "OUT", "ZOOM"},
			[3]string{fmt.Sprintf("%.3f S", float64(s)/48000), fmt.Sprintf("%.3f S", float64(e)/48000), zshort},
			[3]float64{float64(s) / float64(n), float64(e) / float64(n), zn}, a.smpActiveKnob())
	}
	a.uiBottom(c, "TURN SLOW: FINE  ·  FAST: FAR  ·  K3: ZOOM  ·  SHIFT: PITCH, REVERSE")
}
