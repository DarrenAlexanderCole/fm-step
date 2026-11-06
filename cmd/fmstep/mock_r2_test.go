package main

// Design round 2 (after the 0.9.24 test): MODEL without the flag, the knob
// layer as one smooth curve, the MIXTAPE trim with the new controls and a line
// wave, the recorder with MONITOR, and PAD FX as a RACK of sends.
// FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR2

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

// curveLayer: the three knob levels joined by one smooth curve (Catmull-Rom
// through K1, K2, K3), filled below in the colour of the nearest knob, mixed
// across the joins. The proposed replacement for the three flat tiers.
func curveLayer(c *gfx.Canvas, lv [3]float64, gl [3]float64) {
	const top, bot, x0, x1 = 42.0, 136.0, 12, 308
	cx := [3]float64{62, 160, 258}
	Y := func(n float64) float64 { return bot - (bot-top)*n }
	pts := []float64{Y(lv[0]), Y(lv[0]), Y(lv[1]), Y(lv[2]), Y(lv[2])}
	px := []float64{x0 - 40, cx[0], cx[1], cx[2], x1 + 40}
	yAt := func(x float64) float64 {
		i := 0
		for i < len(px)-2 && x > px[i+1] {
			i++
		}
		t := (x - px[i]) / (px[i+1] - px[i])
		p0 := pts[max(0, i-1)]
		p1, p2 := pts[i], pts[i+1]
		p3 := pts[min(len(pts)-1, i+2)]
		t2, t3 := t*t, t*t*t
		return 0.5 * (2*p1 + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t2 + (-p0+3*p1-3*p2+p3)*t3)
	}
	var xs, ys []float64
	for x := float64(x0); x < x1; x++ {
		y := math.Max(top, math.Min(bot, yAt(x+0.5)))
		// colour: the knobs' colours mixed by distance
		var w [3]float64
		sum := 0.0
		for k := 0; k < 3; k++ {
			d := (x + 0.5 - cx[k]) / 60
			w[k] = math.Exp(-d * d)
			sum += w[k]
		}
		col := knobCols[0]
		acc := w[0] / sum
		g := gl[0] * w[0] / sum
		for k := 1; k < 3; k++ {
			acc += w[k] / sum
			col = gfx.Mix(col, knobCols[k], (w[k]/sum)/acc)
			g += gl[k] * w[k] / sum
		}
		c.Line(x+0.5, y+1, x+0.5, bot-1, 1, col, 0.18+0.3*g)
		xs, ys = append(xs, x+0.5), append(ys, y)
		_ = col
	}
	for i := 1; i < len(xs); i++ {
		var w [3]float64
		sum := 0.0
		for k := 0; k < 3; k++ {
			d := (xs[i] - cx[k]) / 60
			w[k] = math.Exp(-d * d)
			sum += w[k]
		}
		col := gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], w[1]/(w[0]+w[1])), knobCols[2], w[2]/sum)
		c.Line(xs[i-1], ys[i-1], xs[i], ys[i], 1.6, col, 1)
	}
	for k := 0; k < 3; k++ {
		c.Circle(cx[k], yAt(cx[k]), 3, knobCols[k], 1)
	}
}

func mockR2Model(a *app, c *gfx.Canvas) {
	a.pageIdx = pageIndex("MODEL")
	a.preset.P["engine"] = 10
	a.touched("engine")
	a.lastKey = "engine"
	a.drawParamPage(c)
}

func mockR2Curve(a *app, c *gfx.Canvas, page string, lv [3]float64) {
	a.pageIdx = pageIndex(page)
	a.ui.touchKey = ""
	c.Text(10, 31, page, gfx.Big, uText, gfx.Left|gfx.Mid)
	c.RoundRect(8, 40, 304, 98, 10, uPanel, 1)
	a.ambientDots(c)
	curveLayer(c, lv, [3]float64{0.2, 0.9, 0.2})
	for k := 0; k < 3; k++ {
		pr := a.cur(k)
		uiKnob(c, 62+float64(k)*98, 169, 18.5, lv[k], -1, pr.label, pct(lv[k]), tcol(a.proj.Track), k == 1, false)
		c.RoundRect(62+float64(k)*98-7, 204, 14, 2.6, 1.3, knobCols[k], 0.8)
	}
	a.uiBottom(c, "ONE CURVE THROUGH K1 · K2 · K3")
}

// a line wave from min/max columns (like the SLICE zoom), not bars
func lineWave(c *gfx.Canvas, x0, x1, ym, h float64, w []float64, col uint32, al float64) {
	n := int(x1 - x0)
	for k := 0; k < n; k++ {
		i0, i1 := k*len(w)/n, max(k*len(w)/n+1, (k+1)*len(w)/n)
		lo, hi := 0.0, 0.0
		for i := i0; i < i1 && i < len(w); i++ {
			lo, hi = math.Min(lo, w[i]), math.Max(hi, w[i])
		}
		x := x0 + float64(k) + 0.5
		c.Line(x, ym-hi*h, x, ym-lo*h, 0.9, col, al)
	}
	c.Line(x0, ym, x1, ym, 0.5, col, al*0.25)
}

func mockR2Trim(a *app, c *gfx.Canvas, sel int) {
	btnLegend(c, [3]string{"IN", "PLAY", "OUT"}, sel)
	w := c.Text(10, 57, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(10+w+7, 58.5, "TAKE 011  ·  TRIM", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pill(c, 312, 57, "LOOP 1 BAR", knobCols[1], true)
	wave := breakWave(96000, 6.8, 2)
	// the whole take: a slim strip with IN / OUT and the zoom window
	glass(c, 8, 68, 304, 20, 6, pBlue)
	lineWave(c, 12, 308, 78, 7, wave, 0xE8E4D8, 0.6)
	in, out := 0.22, 0.61
	c.Rect(12, 70, 296*in, 16, uBG, 0.55)
	c.Rect(12+296*out, 70, 296*(1-out), 16, uBG, 0.55)
	c.RoundRectLine(12+296*0.18, 69, 296*0.30, 18, 3, 1, uText, 0.6)
	// the zoomed card: the real wave as a line, IN selected (DATA moves it)
	glass(c, 8, 94, 304, 82, 10, gCyan)
	z := wave[int(0.18*96000):int(0.48*96000)]
	lineWave(c, 12, 308, 135, 34, z, 0xE8E4D8, 0.9)
	ix := 12 + 296*(in-0.18)/0.30
	lx0, lx1 := ix, 12+296*(in+0.12-0.18)/0.30
	c.Rect(12, 96, ix-12, 78, uBG, 0.5)
	c.RoundRect(lx0, 97, lx1-lx0, 3, 1.5, knobCols[1], 0.95) // the loop
	selCol := uint32(gCyan)
	c.Glow(ix, 135, 18, selCol, 0.25)
	c.Line(ix, 98, ix, 174, 1.6, selCol, 1)
	lab := "IN 1.496 S  ·  DATA MOVES IT"
	if sel == 2 {
		lab = "OUT"
	}
	cw := gfx.Small.Width(lab) + 10
	c.RoundRect(ix+4, 102, cw, 12, 6, selCol, 1)
	c.Text(ix+4+cw/2, 108, lab, gfx.Small, uBG, gfx.Center|gfx.Mid)
	hx := ix + 46
	c.Line(hx, 98, hx, 174, 1.2, uLime, 0.9) // the head, running inside the loop
	mockKnobs(c, [3]string{"HEIGHT", "LOOP", "ZOOM"}, [3]string{"x2.0", "1 BAR", "x3"}, [3]float64{0.3, 0.55, 0.45}, 1)
	a.uiBottom(c, "TURN DATA: BIG MOVES  ·  TAP DATA: TINY STEPS  ·  SHIFT + PLAY: SAVE THE TRIM")
}

func mockR2Rec(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"MONITOR ON", "PAUSE", ""}, 0)
	w := c.Text(10, 57, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(10+w+7, 58.5, "TAKE 012", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pill(c, 312, 57, "REC  0:31", uRed, true)
	glass(c, 8, 68, 304, 92, 10, uRed)
	wave := breakWave(96000, 8, 3)
	lineWave(c, 12, 240, 114, 34, wave[:70000], 0xE8E4D8, 0.9)
	for x := 246.0; x < 306; x += 6 {
		c.Circle(x, 114, 0.9, uDim, 0.6)
	}
	c.Line(240, 72, 240, 156, 1.4, uRed, 1)
	c.Text(16, 150, "MONITOR: YOU HEAR THE TAKE AFTER LEVEL, PAD AND COMP", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Dots(12, 168, "0:31", dotGlyph, 2.2, uText, 0x26271F)
	mockKnobs(c, [3]string{"REC LEVEL", "PAD", "COMP"}, [3]string{"+2.0 DB", "-10 DB", "PUNCH"}, [3]float64{0.72, 0.25, 0.66}, 2)
	a.uiBottom(c, "LEFT: MONITOR  ·  MIDDLE: PAUSE  ·  SAMPLE RECORD: STOP")
}

// PAD FX becomes the RACK: what the sends go to.
func mockR2Rack(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "RACK", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(70, 34, "SENDS  ·  MASTER", gfx.Small, uDim, gfx.Left|gfx.Mid)
	slot := func(y float64, tag, name, info string, col uint32, sel bool, amt []float64) {
		glass(c, 8, y, 304, 40, 8, col)
		if sel {
			c.RoundRectLine(6, y-2, 308, 44, 9, 1.2, uText, 0.8)
		}
		c.RoundRect(16, y+8, 26, 24, 6, col, 0.9)
		c.Text(29, y+20, tag, gfx.Label, uBG, gfx.Center|gfx.Mid)
		c.Text(52, y+14, name, gfx.Label, uText, gfx.Left|gfx.Mid)
		c.Text(52, y+28, info, gfx.Small, uDim, gfx.Left|gfx.Mid)
		// who sends here: a dot per track, sized by its send
		for t, v := range amt {
			x := 200 + float64(t)*13
			c.Circle(x, y+20, 1.2+4*v, tcol(t), 0.25+0.75*math.Min(1, v*2))
		}
	}
	slot(44, "A", "DUB DELAY", "1/4 DOTTED  ·  FEEDBACK 62", knobCols[2], true, []float64{0, 0.5, 0, 0, 0.8, 0.1, 0, 0.3})
	slot(90, "B", "REVERB", "SIZE 70  ·  TONE 40", pLilac, false, []float64{0.2, 0.3, 0.1, 0.2, 0.4, 0.6, 0.2, 0.1})
	slot(136, "M", "MASTER: TAPE + GLUE", "DRIVE 40  ·  GLUE 50", pYellow, false, []float64{})
	c.Text(200, 186, "1 2 3 4 5 6 7 8  WHO SENDS", gfx.Small, uFaint, gfx.Left|gfx.Mid)
	mockKnobs(c, [3]string{"EFFECT", "TIME", "FEEDBACK"}, [3]string{"DUB DELAY", "1/4.", "62"}, [3]float64{0.2, 0.5, 0.62}, 0)
	a.uiBottom(c, "DATA: SLOT  ·  K1: WHICH EFFECT  ·  ON THE MIXER, K3 SENDS TO A, SHIFT + K3 TO B")
}

func mockR2MixSend(a *app, c *gfx.Canvas) {
	a.mode = modeTracks
	a.ui.mixer = true
	a.touched("delay_send")
	a.drawTracksV2(c)
	pill(c, 312, 33, "K3 SENDS TO  A · DUB DELAY", knobCols[2], true)
	a.mode = modePlay
}

func TestMockR2(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 MODEL no flag", func(c *gfx.Canvas) { mockR2Model(a, c) }},
		{"2 CURVE amp", func(c *gfx.Canvas) { mockR2Curve(a, c, "AMP", [3]float64{0.25, 0.55, 0.84}) }},
		{"3 CURVE wobble", func(c *gfx.Canvas) { mockR2Curve(a, c, "WOBBLE", [3]float64{0.7, 0.15, 0.4}) }},
		{"4 MIXTAPE trim IN", func(c *gfx.Canvas) { mockR2Trim(a, c, 0) }},
		{"5 MIXTAPE rec MONITOR", func(c *gfx.Canvas) { mockR2Rec(a, c) }},
		{"6 RACK", func(c *gfx.Canvas) { mockR2Rack(a, c) }},
		{"7 MIXER send target", func(c *gfx.Canvas) { mockR2MixSend(a, c) }},
	}
	for _, s := range shots {
		c := a.scr.C
		c.Clear(uBG)
		a.statusAt = time.Time{}
		a.uiHeader(c)
		s.f(c)
		savePNG(a, filepath.Join(dir, s.name+".png"))
	}
	_ = fmt.Sprint
}
