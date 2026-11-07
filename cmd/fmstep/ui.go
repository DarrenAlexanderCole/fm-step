package main

// FM-STEP 0.9 screens: parameter pages with live graphics and arc knobs, and
// the dot-grid step sequencer (see claude/fmstep-ui-direction.md). Drawn with
// the gfx kit (anti-aliased shapes, Inter text, dot-matrix numbers) into the
// screen's off-screen canvas.

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/panel"
	"fmstep/internal/seq"
)

const (
	uBG     = 0x0B0C0A
	uPanel  = 0x171815
	uPanel2 = 0x22231F
	uLine   = 0x35372F
	uText   = 0xEEEBE2
	uDim    = 0x9A978B
	uFaint  = 0x4A4A43
	uLime   = 0xD6F55C
	uRed    = 0xFF5440
	uOrange = 0xFFA040
)

// uiState: animation and timing for the new screens.
type uiState struct {
	juice     float64 // 0.9.25: the curve's breath (follows the output level)
	rackSlot  int     // 0.9.25 RACK: A, B, MASTER
	sendB     bool    // 0.9.25 mixer: K3 sets send B (else A)
	titleDir  int     // page change: the title slides in from this side
	titleAt   time.Time
	touchKey  string // last knob turned (for the background layer)
	touchAt   time.Time
	animUntil time.Time // keep redrawing until then
	scope     int       // play view face: 0 pages, 1 spectrum, 2 flow (16 LEVELS cycles)
	hits      []hit     // recent notes, for the flow visualizer
	mixer     bool      // tracks view shows the mixer (else the notes of all tracks)
	lastScope int       // the visualizer face the middle button returns to
	spec      specState
	bank      bankCache // sound names of the bank on screen
	frames    int
	sent      int // frames that changed the panel
	areaSum   int
	drawSum   time.Duration
	drawMax   time.Duration
	statsAt   time.Time
}

const (
	titleAnim = 160 * time.Millisecond
	touchHold = 900 * time.Millisecond
	touchFade = 600 * time.Millisecond
)

func (a *app) animate(until time.Time) {
	if until.After(a.ui.animUntil) {
		a.ui.animUntil = until
	}
}

// touched starts the background layer for the knob just turned.
func (a *app) touched(key string) {
	a.ui.touchKey, a.ui.touchAt = key, time.Now()
	a.animate(a.ui.touchAt.Add(touchHold + touchFade))
}

func tcol(t int) uint32 { return rgb7(trackColours[t]) }

func dotGlyph(r rune) [7]uint8 { return panel.Glyph5x7(r) }

// ------------------------------------------------------------------ shared parts

func (a *app) uiHeader(c *gfx.Canvas) {
	t := a.proj.Track
	c.RoundRect(6, 6, 18, 14, 7, tcol(t), 1)
	c.Text(15, 13, fmt.Sprintf("%d", t+1), gfx.Label, uBG, gfx.Center|gfx.Mid)
	name := a.preset.Name
	w := c.Text(30, 13, name, gfx.Title, uText, gfx.Left|gfx.Mid)
	if a.modified {
		c.Circle(30+w+5, 10, 2, uOrange, 1)
	}
	// transport chip: play state + pattern and bar.beat in dot matrix
	c.RoundRect(214, 5, 100, 17, 8.5, uPanel, 1)
	x := 221.0
	if a.sq.playing && a.sq.posPat >= 0 {
		c.Poly([]float64{x, x, x + 7}, []float64{9.5, 17.5, 13.5}, uLime, 1)
		s := fmt.Sprintf("P%d %d.%d", a.sq.posPat+1, a.sq.posStep/16+1, a.sq.posStep%16/4+1)
		c.Dots(x+11, 8.5, s, dotGlyph, 1.42, uLime, 0x283019)
	} else {
		c.RoundRect(x, 10, 7, 7, 1.5, uDim, 1)
		c.Dots(x+11, 8.5, fmt.Sprintf("P%d %d", a.proj.Sel+1, int(math.Round(a.proj.G["tempo"]))), dotGlyph, 1.42, uDim, 0x24241F)
	}
	if a.sq.recArm {
		c.Circle(306, 13.5, 3, uRed, 1)
	}
	a.mtBadge(c)
	a.uiTally(c)
}

// uiTally: a thin loop line under the header on every screen. One segment
// per bar of the playing pattern; it fills as the loop plays (red while
// recording), with a bright head on the current step.
func (a *app) uiTally(c *gfx.Canvas) {
	pi := a.proj.Sel
	playing := a.sq.playing && a.sq.posPat >= 0
	if playing {
		pi = a.sq.posPat
	}
	n := a.proj.Pat[pi].Len
	nb := bars(n)
	const x0, x1, y, gap = 6.0, 314.0, 23.8, 3.0
	sw := (x1 - x0 - gap*float64(nb-1)) / float64(nb)
	fill := uint32(uLime)
	if a.sq.recArm {
		fill = uRed
	}
	for b := 0; b < nb; b++ {
		steps := min(16, n-b*16)
		bx := x0 + float64(b)*(sw+gap)
		bw := sw * float64(steps) / 16
		c.RoundRect(bx, y-1, bw, 2, 1, uPanel2, 1)
		if !playing {
			continue
		}
		if s := a.sq.posStep - b*16; s >= 0 {
			done := math.Min(float64(steps), float64(s)+1)
			c.RoundRect(bx, y-1, bw*done/float64(steps), 2, 1, fill, 0.85)
			if s < steps {
				hx := bx + bw*(float64(s)+0.5)/float64(steps)
				c.Circle(hx, y, 2, uText, 1)
			}
		}
	}
}

func uiPageDots(c *gfx.Canvas, idx, n int, y float64) {
	x0 := 160 - float64(n*7)/2
	for i := 0; i < n; i++ {
		if i == idx {
			c.RoundRect(x0+float64(i*7)-2, y-1.5, 9, 3, 1.5, uLime, 1)
		} else {
			c.Circle(x0+float64(i*7)+2.5, y, 1.3, uFaint, 1)
		}
	}
}

// uiKnob: arc knob. v and neutral are 0..1; with neutral >= 0 the arc grows
// from that point (bipolar), else from the start.
func uiKnob(c *gfx.Canvas, x, y, r, v, neutral float64, label, val string, col uint32, active, alt bool) {
	c.Arc(x, y, r, 3.2, 135, 405, uPanel2, 1)
	start := 135.0
	if neutral >= 0 {
		start = 135 + 270*neutral
	}
	end := 135 + 270*v
	if math.Abs(end-start) > 0.5 {
		c.Arc(x, y, r, 3.2, math.Min(start, end), math.Max(start, end), col, 1)
	}
	ang := end * math.Pi / 180
	dot := col
	if active {
		dot = uText
	}
	c.Circle(x+r*math.Cos(ang), y+r*math.Sin(ang), 2.6, dot, 1)
	f := gfx.Value
	if len(val) > 4 {
		f = gfx.Body
	}
	if f.Width(val) > 2*r-4 {
		f = gfx.Small
	}
	c.Text(x, y, val, f, uText, gfx.Center|gfx.Mid)
	lc := uint32(uDim)
	if active {
		lc = uText
	}
	if alt {
		lc = uOrange
	}
	c.Text(x, y+r+11, label, gfx.Body, lc, gfx.Center|gfx.Mid)
}

// knobCols: one colour per knob (K1 cyan, K2 orange, K3 magenta), used for
// the knob layer and the small tag under each knob label.
var knobCols = [3]uint32{0x20E8E0, 0xFF9A2E, 0xFF3FA4}

const (
	wordHold = 350 * time.Millisecond
	wordFade = 300 * time.Millisecond
	uWord    = 0xF5E24A
)

// knobGlow: how strongly knob k's layer shows (base, brighter while turning).
func (a *app) knobGlow(key string) float64 {
	g := 0.20
	dt := time.Since(a.ui.touchAt)
	if a.ui.touchKey == key && dt < touchHold+touchFade {
		e := 1.0
		if dt > touchHold {
			e = math.Round((1-float64(dt-touchHold)/float64(touchFade))*6) / 6
		}
		g += 0.16 * e
	}
	return g
}

// touchLayer: a heat map of the three knobs behind the graph. Each knob is a
// band of light in its colour, centred over the knob, filled up to its value
// (from the middle for bipolar controls); neighbouring bands overlap and add
// up, so the mix of all three settings is always visible. The knob being
// turned glows brighter. Choices with names (model, delay time...) also flash
// the name in large bright dot matrix, which fades quickly.
func (a *app) touchLayer(c *gfx.Canvas, col uint32) {
	// 0.9.25: one smooth curve through the three knob levels (Catmull-Rom),
	// shaded 40% DOTS, 20% LAYERS, 20% FLOW LINES and 20% JUICE (the fill
	// breathes with the sound). Replaces the three flat tiers.
	var lv, gl [3]float64
	for k := 0; k < 3; k++ {
		pr := a.cur(k)
		lv[k] = pr.norm(a.val(pr.key))
		gl[k] = a.knobGlow(pr.key)
	}
	level := 0.0
	if a.dsp != nil {
		a.dsp.specMu.Lock()
		if time.Since(a.dsp.specAt) < 300*time.Millisecond {
			level = math.Max(a.dsp.peakL, a.dsp.peakR)
		}
		a.dsp.specMu.Unlock()
	}
	a.ui.juice += (math.Min(1, level*1.6) - a.ui.juice) * 0.35
	curveShade(c, lv, gl, a.ui.juice, time.Since(appStart).Seconds())
	// named choices: the name, large, bright and brief
	dt := time.Since(a.ui.touchAt)
	if a.ui.touchKey == "" || dt > wordHold+wordFade {
		return
	}
	var pr *param
	for i := 0; i < 3; i++ {
		if p := a.cur(i); p.key == a.ui.touchKey {
			pr = p
		}
	}
	if pr == nil || pr.count() <= 1 {
		return
	}
	if pn := pages[a.pageIdx].name; pn == "GLOBAL" || pn == "MODEL" {
		// GLOBAL already shows BPM and voices large, MODEL the name and the lit
		// chip: no big dot word on top (0.9.25: no saying the same thing twice)
		return
	}
	we := 1.0
	if dt > wordHold {
		we = math.Round((1-float64(dt-wordHold)/float64(wordFade))*4) / 4
	}
	v := a.val(pr.key)
	s := strings.ToUpper(pr.show(v))
	if pr.key == "harmonics" {
		if nm := harmonicsName(int(a.val("engine")), v); nm != "" {
			s = strings.ToUpper(nm)
		}
	}
	if we <= 0 || s == "" {
		return
	}
	n6 := float64(len([]rune(s)) * 6)
	p := math.Min(8, 280/n6)
	w := n6*p - p
	c.Dots(160-w/2, 89-3.5*p, s, dotGlyph, p, gfx.Mix(uPanel, uWord, 0.75*we), 0)
}

// uiHint: the bottom pill carries instructions, messages and the tour.
func uiHint(c *gfx.Canvas, text string, dot uint32, y float64) {
	w := gfx.Small.Width(text) + 18
	if w > 312 {
		w = 312
	}
	c.RoundRect(160-w/2, y-7.5, w, 15, 7.5, uPanel, 1)
	c.Circle(160-w/2+8, y, 2, dot, 1)
	c.Text(160+5, y, text, gfx.Small, uDim, gfx.Center|gfx.Mid)
}

// uiBottom: tour step, a fresh message, or the page's hint.
func (a *app) uiBottom(c *gfx.Canvas, hint string) {
	if !a.tourDone && a.tourIdx < len(a.tour) {
		c.RoundRect(8, 204, 304, 33, 9, 0x2A2C1C, 1)
		c.Text(16, 211, fmt.Sprintf("TOUR %d/%d", a.tourIdx+1, len(a.tour)), gfx.Label, uLime, gfx.Left|gfx.Mid)
		c.Text(304, 211, "ERASE+PUSH: SKIP", gfx.Small, uFaint, gfx.Right|gfx.Mid)
		c.Text(16, 225, strings.ReplaceAll(a.tour[a.tourIdx].text, "\n", " "), gfx.Small, uText, gfx.Left|gfx.Mid)
		return
	}
	if a.status != "" && time.Since(a.statusAt) < 4*time.Second {
		uiHint(c, a.status, uLime, 229)
		return
	}
	if hint != "" {
		uiHint(c, hint, uFaint, 229)
	}
}

// ------------------------------------------------------------------ graphs

func logX(f, x0, x1 float64) float64 {
	return x0 + (math.Log10(f)-math.Log10(20))/(math.Log10(20000)-math.Log10(20))*(x1-x0)
}

func uiFreqGrid(c *gfx.Canvas, x0, y0, x1, y1 float64, dbs []float64, dbMin, dbMax float64) {
	for _, f := range []float64{50, 100, 200, 500, 1000, 2000, 5000, 10000} {
		x := logX(f, x0, x1)
		c.Line(x, y0, x, y1, 0.5, 0xFFFFFF, 0.05)
	}
	for _, db := range dbs {
		y := y0 + (dbMax-db)/(dbMax-dbMin)*(y1-y0)
		al := 0.04
		if db == 0 {
			al = 0.1
		}
		c.Line(x0, y, x1, y, 0.5, 0xFFFFFF, al)
	}
	for _, l := range []struct {
		f float64
		t string
	}{{100, "100"}, {1000, "1K"}, {10000, "10K"}} {
		c.Text(logX(l.f, x0, x1), y1+5, l.t, gfx.Small, uFaint, gfx.Center|gfx.Mid)
	}
}

func biquadDB(kind int, f0, db, q, f float64) float64 {
	A := math.Pow(10, db/40)
	w0 := 2 * math.Pi * f0 / 48000
	cs, sn := math.Cos(w0), math.Sin(w0)
	al := sn / (2 * q)
	sq := 2 * math.Sqrt(A) * al
	var b, ac [3]float64
	switch kind {
	case 1: // peak
		b = [3]float64{1 + al*A, -2 * cs, 1 - al*A}
		ac = [3]float64{1 + al/A, -2 * cs, 1 - al/A}
	case 0: // low shelf
		b = [3]float64{A * ((A + 1) - (A-1)*cs + sq), 2 * A * ((A - 1) - (A+1)*cs), A * ((A + 1) - (A-1)*cs - sq)}
		ac = [3]float64{(A + 1) + (A-1)*cs + sq, -2 * ((A - 1) + (A+1)*cs), (A + 1) + (A-1)*cs - sq}
	default: // high shelf
		b = [3]float64{A * ((A + 1) + (A-1)*cs + sq), -2 * A * ((A - 1) + (A+1)*cs), A * ((A + 1) + (A-1)*cs - sq)}
		ac = [3]float64{(A + 1) - (A-1)*cs + sq, 2 * ((A - 1) - (A+1)*cs), (A + 1) - (A-1)*cs - sq}
	}
	w := 2 * math.Pi * f / 48000
	z1 := cmplx.Exp(complex(0, -w))
	z2 := z1 * z1
	h := (complex(b[0], 0) + complex(b[1], 0)*z1 + complex(b[2], 0)*z2) / (complex(ac[0], 0) + complex(ac[1], 0)*z1 + complex(ac[2], 0)*z2)
	return 20 * math.Log10(cmplx.Abs(h)+1e-12)
}

const gx0, gy0, gx1, gy1 = 14.0, 44.0, 306.0, 124.0 // graph area inside the card

// graphEQ: the mix spectrum behind, each band's own shape filled in its
// knob's colour (low shelf K1, mid bell K2, high shelf K3, layered like
// watercolour), and the combined curve in white on top.
func (a *app) graphEQ(c *gfx.Canvas, col uint32) {
	a.updateSpec()
	// grid: + marks at frequency / level crossings
	Y := func(db float64) float64 { return gy0 + (18-math.Max(-36, math.Min(18, db)))/54*(gy1-gy0) }
	for _, f := range []float64{50, 100, 200, 500, 1000, 2000, 5000, 10000} {
		x := logX(f, gx0, gx1)
		for _, db := range []float64{12, 0, -12, -24} {
			y := Y(db)
			c.Line(x-1.5, y, x+1.5, y, 0.6, 0xFFFFFF, 0.10)
			c.Line(x, y-1.5, x, y+1.5, 0.6, 0xFFFFFF, 0.10)
		}
	}
	c.Line(gx0, Y(0), gx1, Y(0), 0.6, 0xFFFFFF, 0.14)
	for _, l := range []struct {
		f float64
		t string
	}{{100, "100"}, {1000, "1K"}, {10000, "10K"}} {
		c.Text(logX(l.f, gx0, gx1), gy1+5, l.t, gfx.Small, uFaint, gfx.Center|gfx.Mid)
	}
	// the music: mix spectrum, soft grey, from the bottom
	sxs, sys := a.specCurve(&a.ui.spec.show, gx0, gy0+6, gx1, gy1, -72, 0)
	c.FillBetween(sxs, sys, gy1, 0xFFFFFF, 0.07)
	c.Polyline(sxs, sys, 0.8, 0xFFFFFF, 0.22)

	lo, mi, hi := a.val("eq_low"), a.val("eq_mid"), a.val("eq_high")
	fl, fm, fh := eqLowHz(a.val("eq_low_freq")), eqMidHz(a.val("eq_mid_freq")), eqHighHz(a.val("eq_high_freq"))
	bands := []struct {
		kind int
		f, q float64
		db   float64
		lab  string
		key  string
	}{{0, fl, 0.707, eqDb(lo), "L", "eq_low"}, {1, fm, 0.9, eqDb(mi), "M", "eq_mid"}, {2, fh, 0.707, eqDb(hi), "H", "eq_high"}}
	const n = 150
	fs := make([]float64, n)
	xs := make([]float64, n)
	for i := range fs {
		fs[i] = 20 * math.Pow(1000, float64(i)/(n-1))
		xs[i] = logX(fs[i], gx0, gx1)
	}
	total := make([]float64, n)
	for k, b := range bands {
		ys := make([]float64, n)
		for i, f := range fs {
			d := biquadDB(b.kind, b.f, b.db, b.q, f)
			total[i] += d
			ys[i] = Y(d)
		}
		al := 0.30
		if a.lastKey == b.key || a.lastKey == b.key+"_freq" {
			al = 0.42
		}
		c.FillBetween(xs, ys, Y(0), knobCols[k], al)
		c.Polyline(xs, ys, 1, knobCols[k], 0.9)
	}
	tys := make([]float64, n)
	for i := range total {
		tys[i] = Y(total[i])
	}
	c.Polyline(xs, tys, 1.6, uText, 1)
	for k, b := range bands {
		x, y := logX(b.f, gx0, gx1), Y(b.db*boolF(b.kind == 1)+biquadDB(b.kind, b.f, b.db, b.q, b.f)*boolF(b.kind != 1))
		act := a.lastKey == b.key || a.lastKey == b.key+"_freq"
		if act {
			c.Circle(x, y, 6.5, knobCols[k], 1)
			c.Text(x, y, b.lab, gfx.Label, uBG, gfx.Center|gfx.Mid)
		} else {
			c.Circle(x, y, 6, knobCols[k], 1)
			c.Circle(x, y, 4.8, uPanel, 1)
			c.Text(x, y, b.lab, gfx.Label, uText, gfx.Center|gfx.Mid)
		}
	}
	if eqDb(lo) <= -35.5 || eqDb(mi) <= -35.5 || eqDb(hi) <= -35.5 {
		c.Text(gx1-2, gy0+5, "KILL", gfx.Label, uRed, gfx.Right|gfx.Mid)
	}
}

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func ladderDB(fc, res, f float64) float64 {
	s := complex(0, f/fc)
	k := res * 3.8
	h := 1 / ((1+s)*(1+s)*(1+s)*(1+s) + complex(k, 0))
	return 20 * math.Log10(cmplx.Abs(h)*(1+k)+1e-12)
}

func (a *app) graphFilter(c *gfx.Canvas, col uint32) {
	uiFreqGrid(c, gx0, gy0, gx1, gy1, []float64{-24, -12, 0, 12}, -30, 15)
	fc := 25 * math.Pow(720, a.val("cutoff"))
	res := a.val("resonance")
	Y := func(db float64) float64 { return gy0 + (15-math.Max(-30, math.Min(15, db)))/45*(gy1-gy0) }
	xs, ys := make([]float64, 0, 150), make([]float64, 0, 150)
	for i := 0; i < 150; i++ {
		f := 20 * math.Pow(1000, float64(i)/149)
		xs, ys = append(xs, logX(f, gx0, gx1)), append(ys, Y(ladderDB(fc, res, f)))
	}
	c.FillUnder(xs, ys, gy1, col, 0.45)
	c.Polyline(xs, ys, 1.6, col, 1)
	cx := logX(math.Max(20, math.Min(20000, fc)), gx0, gx1)
	c.Line(cx, gy0, cx, gy1, 0.8, uLime, 0.35)
	lab := fmt.Sprintf("%d HZ", int(fc))
	if fc >= 1000 {
		lab = fmt.Sprintf("%.1f KHZ", fc/1000)
	}
	w := gfx.Label.Width(lab) + 12
	bx := math.Min(cx+4, gx1-w)
	c.RoundRect(bx, gy0+2, w, 13, 6.5, uLime, 0.92)
	c.Text(bx+w/2, gy0+8.5, lab, gfx.Label, uBG, gfx.Center|gfx.Mid)
	// sub oscillator: a sine whose height is the sub level
	if sub := a.val("sub_level"); sub > 0.01 {
		xs2, ys2 := make([]float64, 60), make([]float64, 60)
		for i := range xs2 {
			xs2[i] = gx0 + 2 + float64(i)*0.6
			ys2[i] = gy1 - 6 - 4*sub*math.Sin(float64(i)*0.26)
		}
		c.Polyline(xs2, ys2, 1, col, 0.8)
		c.Text(gx0+2, gy1-17, fmt.Sprintf("SUB -%d", int(a.val("sub_oct"))), gfx.Small, col, gfx.Left|gfx.Mid)
	}
}

func timeSec(v, lo, hi float64) float64 { return lo * math.Pow(hi/lo, v) }

func (a *app) graphAmp(c *gfx.Canvas, col uint32) {
	at := timeSec(a.val("attack"), 0.001, 4)
	dc := timeSec(a.val("decay2"), 0.01, 6)
	su := a.val("sustain")
	rl := timeSec(a.val("release"), 0.01, 6)
	// time axis is compressed (square root) so short and long envelopes both read
	sq := func(t float64) float64 { return math.Sqrt(t) }
	hold := 0.35 * (sq(at) + sq(dc) + sq(rl) + 0.2)
	tot := sq(at) + sq(dc) + hold + sq(rl)
	X := func(u float64) float64 { return gx0 + 4 + u/tot*(gx1-gx0-8) }
	Y := func(v float64) float64 { return gy1 - v*(gy1-gy0-6) }
	var xs, ys []float64
	add := func(u, v float64) { xs, ys = append(xs, X(u)), append(ys, Y(v)) }
	add(0, 0)
	for i := 1; i <= 16; i++ {
		f := float64(i) / 16
		add(sq(at)*f, math.Pow(f, 0.7))
	}
	for i := 1; i <= 24; i++ {
		f := float64(i) / 24
		add(sq(at)+sq(dc)*f, su+(1-su)*math.Exp(-4.6*f))
	}
	add(sq(at)+sq(dc)+hold, su)
	for i := 1; i <= 24; i++ {
		f := float64(i) / 24
		add(sq(at)+sq(dc)+hold+sq(rl)*f, su*math.Exp(-4.6*f))
	}
	c.FillUnder(xs, ys, gy1, col, 0.45)
	c.Polyline(xs, ys, 1.6, col, 1)
	for _, m := range []struct {
		u float64
		t string
	}{{sq(at), "A"}, {sq(at) + sq(dc), "D"}, {sq(at) + sq(dc) + hold, "S"}, {tot, "R"}} {
		c.Line(X(m.u), gy0, X(m.u), gy1, 0.5, 0xFFFFFF, 0.07)
		c.Text(X(m.u)-3, gy0+3, m.t, gfx.Label, uFaint, gfx.Right|gfx.Mid)
	}
}

func (a *app) graphModel(c *gfx.Canvas, col uint32) {
	eng := int(a.val("engine"))
	c.Text(gx0+4, gy0+12, engineNames[eng], gfx.Big, uText, gfx.Left|gfx.Mid)
	sub := harmonicsName(eng, a.val("harmonics"))
	if sub == "" {
		if indexOfOK(modelChoices, float64(eng)) < 0 {
			sub = "AN OLDER MODEL  ·  K1 PICKS ONE OF THE 8"
		}
		// 0.9.25: no "INSTRUMENT n OF 4" line: the chips already say it
	}
	if indexOfOK(modelChoices, float64(eng)) < 0 { // 0.9.25: only for an older model; the chips say the rest
		c.Text(gx0+4, gy0+30, sub, gfx.Body, col, gfx.Left|gfx.Mid)
	}
	if st := modelStyle[eng]; st != "" {
		w := gfx.Big.Width(engineNames[eng])
		c.Text(gx0+10+w, gy0+13, st, gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
	// the instruments as chips (two rows of 4); the current one lit
	for i, m := range modelChoices {
		name := engineNames[int(m)]
		w := (gx1 - gx0 - 8 - 3*6) / 4
		x := gx0 + 4 + float64(i%4)*(w+6)
		y := gy0 + 32.0 + float64(i/4)*24
		if int(m) == eng {
			c.RoundRect(x, y, w, 19, 9.5, col, 0.9)
			c.Text(x+w/2, y+9.5, name, gfx.Small, uBG, gfx.Center|gfx.Mid)
			// 0.9.25: while K1 turns, a plain line up to a node above the chip (no flag, no words)
			// 0.9.26: top row: the node above the chip, line from the top; bottom row: below, from the bottom
			if kk, e := a.turning(); kk == 0 && e > 0 {
				if i < 4 {
					c.Line(x+w/2, gy0-2, x+w/2, y-6, 1.2, knobCols[0], 0.9*e)
					c.Glow(x+w/2, y-6, 8, knobCols[0], 0.5*e)
					c.Circle(x+w/2, y-6, 2.6, knobCols[0], e)
				} else {
					c.Line(x+w/2, y+25, x+w/2, gy1+12, 1.2, knobCols[0], 0.9*e)
					c.Glow(x+w/2, y+25, 8, knobCols[0], 0.5*e)
					c.Circle(x+w/2, y+25, 2.6, knobCols[0], e)
				}
			}
		} else {
			c.RoundRectLine(x, y, w, 19, 9.5, 1, uFaint, 1)
			c.Text(x+w/2, y+9.5, name, gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
	}
}

func indexOf(xs []float64, v float64) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return 0
}

func (a *app) graphSound(c *gfx.Canvas, col uint32) {
	// a dot field: timbre across, morph up, harmonics sets the spread
	tx, my, hz := a.val("timbre"), a.val("morph"), a.val("harmonics")
	px := gx0 + 10 + tx*(gx1-gx0-20)
	py := gy1 - 6 - my*(gy1-gy0-12)
	for j := 0; j < 7; j++ {
		for i := 0; i < 24; i++ {
			x := gx0 + 10 + float64(i)*(gx1-gx0-20)/23
			y := gy1 - 6 - float64(j)*(gy1-gy0-12)/6
			d := math.Hypot((x-px)/(gx1-gx0), (y-py)/(gy1-gy0)*1.6)
			k := math.Exp(-d * d / (0.01 + 0.06*hz))
			c.Circle(x, y, 1.2+3*k, gfx.Mix(uFaint, col, k), 1)
		}
	}
	c.Circle(px, py, 3, uText, 1)
}

func (a *app) graphWobble(c *gfx.Canvas, col uint32) {
	depth := a.val("wobble_depth")
	div := divNames[int(a.val("wobble_div"))]
	cycles := map[string]float64{"1/32": 16, "1/16T": 12, "1/16": 8, "1/8T": 6, "1/8": 4, "1/8D": 2.67, "1/4T": 3, "1/4": 2, "1/4D": 1.33, "1/2": 1, "1/1": 0.5}[div]
	mid := (gy0 + gy1) / 2
	xs, ys := make([]float64, 140), make([]float64, 140)
	for i := range xs {
		xs[i] = gx0 + 4 + float64(i)*(gx1-gx0-8)/139
		ys[i] = mid - (gy1-gy0-14)/2*math.Max(0.04, depth)*math.Cos(2*math.Pi*cycles*float64(i)/139)
	}
	c.FillUnder(xs, ys, gy1, col, 0.3)
	c.Polyline(xs, ys, 1.6, col, 1)
	c.Text(gx1-4, gy0+6, div+" · 1 BAR", gfx.Small, uDim, gfx.Right|gfx.Mid)
}

func (a *app) graphDelay(c *gfx.Canvas, col uint32) {
	beats := []float64{0.125, 1.0 / 6, 0.25, 1.0 / 3, 0.5, 0.75, 2.0 / 3, 1, 1.5, 2, 4}[int(a.val("delay_div"))]
	fb := math.Min(1.15*a.val("delay_feedback"), 0.99)
	send := a.val("delay_send")
	span := 4.0 // beats shown
	c.Line(gx0+4, gy1, gx1-4, gy1, 0.6, uLine, 1)
	for b := 0.0; b <= span; b++ {
		x := gx0 + 4 + b/span*(gx1-gx0-8)
		c.Line(x, gy0, x, gy1, 0.5, 0xFFFFFF, 0.06)
	}
	h := gy1 - gy0 - 10
	c.RoundRect(gx0+2, gy1-h, 4, h, 2, uText, 1) // the dry hit
	// echo heights show the feedback decay; the send sets how bright they are
	g := 0.8
	al := 0.35 + 0.65*send
	for n := 1; ; n++ {
		t := float64(n) * beats
		if t > span || g < 0.02 || n > 40 {
			break
		}
		x := gx0 + 4 + t/span*(gx1-gx0-8)
		c.RoundRect(x-2, gy1-h*g, 4, h*g, 2, col, al*(0.4+0.6*g))
		g *= fb
	}
	c.Text(gx0+10, gy0+6, fmt.Sprintf("SEND %d%%", int(send*100+0.5)), gfx.Small, uDim, gfx.Left|gfx.Mid)
	lab := fmt.Sprintf("FEEDBACK %d%%", int(a.val("delay_feedback")*100+0.5))
	if a.val("delay_feedback") > 0.87 {
		lab = "RUNAWAY"
	}
	c.Text(gx1-4, gy0+6, lab, gfx.Small, uDim, gfx.Right|gfx.Mid)
}

func (a *app) graphReverb(c *gfx.Canvas, col uint32) {
	size, tone, send := a.val("reverb_size"), a.val("reverb_tone"), a.val("reverb_send")
	decay := 0.6 + 5*size
	xs, ys := make([]float64, 120), make([]float64, 120)
	for i := range xs {
		t := float64(i) / 119 * 6
		xs[i] = gx0 + 4 + float64(i)*(gx1-gx0-8)/119
		ys[i] = gy1 - (gy1-gy0-14)*math.Exp(-3*t/decay)
	}
	c.FillUnder(xs, ys, gy1, gfx.Mix(0x4060FF, col, tone), 0.15+0.5*send)
	c.Polyline(xs, ys, 1.4, col, 0.35+0.65*send)
	c.Text(gx0+10, gy0+6, fmt.Sprintf("SEND %d%%", int(send*100+0.5)), gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(gx1-4, gy0+6, fmt.Sprintf("~%.1f S TAIL", decay), gfx.Small, uDim, gfx.Right|gfx.Mid)
}

func (a *app) graphTape(c *gfx.Canvas, col uint32) {
	drive, age := a.val("tape_drive"), a.val("tape_age")
	k := 1 + drive*2
	cx, cy, r := (gx0+gx1)/2, (gy0+gy1)/2, (gy1-gy0)/2-4
	c.Line(cx-r*1.6, cy, cx+r*1.6, cy, 0.5, 0xFFFFFF, 0.08)
	c.Line(cx, cy-r, cx, cy+r, 0.5, 0xFFFFFF, 0.08)
	c.Line(cx-r, cy+r, cx+r, cy-r, 0.6, 0xFFFFFF, 0.12) // clean
	xs, ys := make([]float64, 80), make([]float64, 80)
	for i := range xs {
		x := -1 + 2*float64(i)/79
		xs[i] = cx + r*1.6*x
		ys[i] = cy - r*math.Tanh(k*x)/math.Tanh(k)*(1+0.03*age*math.Sin(float64(i)*0.9))
	}
	c.Polyline(xs, ys, 1.8, col, 1)
	// hiss and crackle as faint dots along the bottom
	for i := 0; i < int(40*a.val("hiss")+30*a.val("crackle")); i++ {
		x := gx0 + 6 + float64((i*53)%271)
		y := gy1 - 3 - float64((i*29)%7)
		c.Circle(x, y, 0.8, uDim, 0.6)
	}
}

func (a *app) graphWarmth(c *gfx.Canvas, col uint32) {
	drift, chorus, soft := a.val("drift"), a.val("chorus"), a.val("softness")
	mid := (gy0 + gy1) / 2
	for v := 0; v < 3; v++ {
		xs, ys := make([]float64, 120), make([]float64, 120)
		det := float64(v-1) * drift * 0.06
		for i := range xs {
			t := float64(i) / 119
			xs[i] = gx0 + 4 + t*(gx1-gx0-8)
			w := math.Sin(2*math.Pi*(3+det)*t + float64(v)*chorus*1.2)
			// softness rounds the wave (fewer harmonics)
			w = (1-soft)*math.Copysign(math.Pow(math.Abs(w), 0.45), w) + soft*w
			ys[i] = mid - (gy1-gy0-16)/2*w
		}
		al := 1.0
		if v != 1 {
			al = 0.25 + 0.5*chorus
		}
		c.Polyline(xs, ys, 1.4, col, al)
	}
}

func (a *app) graphGlobal(c *gfx.Canvas, col uint32) {
	bpm := fmt.Sprintf("%d", int(math.Round(a.proj.G["tempo"])))
	c.Dots(gx0+8, gy0+8, bpm, dotGlyph, 5, uText, 0x26271F)
	c.Text(gx0+8+float64(len(bpm))*30+4, gy0+38, "BPM", gfx.Label, uDim, gfx.Left|gfx.Mid)
	n := int(a.val("voices"))
	for i := 0; i < 6; i++ {
		x := gx1 - 90 + float64(i)*14
		if i < n {
			c.Circle(x, gy0+18, 4.5, col, 1)
		} else {
			c.Ring(x, gy0+18, 4, 1, uFaint, 1)
		}
	}
	c.Text(gx1-90-6, gy0+34, "VOICES", gfx.Label, uDim, gfx.Left|gfx.Mid)
	lv := a.val("level")
	c.RoundRect(gx1-96, gy0+50, 86, 5, 2.5, uPanel2, 1)
	c.RoundRect(gx1-96, gy0+50, 86*lv, 5, 2.5, col, 1)
	c.Text(gx1-96, gy0+64, "LEVEL", gfx.Label, uDim, gfx.Left|gfx.Mid)
}

func (a *app) graphGroove(c *gfx.Canvas, col uint32) {
	sw := a.proj.G["swing"]
	y := gy0 + 22
	for i := 0; i < 16; i++ {
		x := gx0 + 12 + float64(i)*17.4
		if i%2 == 1 {
			x += sw * 0.42 * 17.4
		}
		r := 3.0
		if i%4 == 0 {
			r = 4.5
		}
		c.Circle(x, y, r, col, 1)
	}
	c.Text(gx0+6, y+16, fmt.Sprintf("SWING %d%%", int(50+sw*21+0.5)), gfx.Label, uDim, gfx.Left|gfx.Mid)
	// pan: a dot on a line
	pan := a.val("pan")
	px0, px1, py := gx0+40, gx1-40, gy1-14
	c.Line(px0, py, px1, py, 1, uLine, 1)
	c.Line((px0+px1)/2, py-4, (px0+px1)/2, py+4, 0.8, uFaint, 1)
	c.Circle(px0+pan*(px1-px0), py, 4.5, col, 1)
	c.Text(px0-8, py, "L", gfx.Label, uDim, gfx.Right|gfx.Mid)
	c.Text(px1+8, py, "R", gfx.Label, uDim, gfx.Left|gfx.Mid)
}

// ------------------------------------------------------------------ pages

var pageHints = map[string]string{
	"EQ":           "SHIFT + K1 K2 K3  FREQUENCY  ·  SHIFT + PUSH  FLAT",
	"AMP":          "SHIFT + K3  RELEASE",
	"SUB + FILTER": "SHIFT + K1  SUB OCTAVE  ·  SHIFT + PUSH  RESET",
	"DUB DELAY":    "SHIFT + K3  TONE  ·  NOTE REPEAT  THROW",
	"TAPE":         "SHIFT + K1  GLUE  ·  SHIFT + K3  CRACKLE",
	"GLOBAL":       "SHIFT + K3  USB LEVEL  ·  HOLD TAP + DATA  BPM",
	"GROOVE":       "SHIFT + K3  PAN SNAP",
	"MODEL":        "K1  THE 8 INSTRUMENTS  ·  SAMPLE SELECT  SOUNDS",
}

var bipolarKeys = map[string]bool{"eq_low": true, "eq_mid": true, "eq_high": true, "pan": true}

func (a *app) drawParamPage(c *gfx.Canvas) {
	pg := &pages[a.pageIdx]
	col := tcol(a.proj.Track)
	// page title: large, slides in from the side the encoder turned toward
	tx, tc := 10.0, uint32(uText)
	if dt := time.Since(a.ui.titleAt); dt < titleAnim && a.ui.titleDir != 0 {
		t := float64(dt) / float64(titleAnim)
		e := 1 - math.Pow(1-t, 3)
		tx += float64(a.ui.titleDir) * 36 * (1 - e)
		tc = gfx.Mix(uBG, uText, e)
	}
	w := c.Text(tx, 31, pg.name, gfx.Big, tc, gfx.Left|gfx.Mid)
	c.Text(tx+w+6, 32.5, fmt.Sprintf("%d/%d", a.pageIdx+1, len(pages)), gfx.Small, uFaint, gfx.Left|gfx.Mid)
	uiPageDots(c, a.pageIdx, len(pages), 212)
	// fader effect chip (only while an effect is on)
	if i := a.faderIdx; a.fxOn(i) {
		ft := faderTargets[i]
		t := ft.label + " " + fxAmount(i, a.fxNow[i])
		w := gfx.Small.Width(t) + 12
		c.RoundRect(312-w, 25.5, w, 13, 6.5, rgb7(ft.col), 0.25)
		c.Text(312-w/2, 32, t, gfx.Small, uText, gfx.Center|gfx.Mid)
	}
	c.RoundRect(8, 40, 304, 98, 10, uPanel, 1)
	if pg.name != "EQ" { // the EQ draws its bands in the knob colours itself
		a.ambientDots(c)
		if pg.name != "MODEL" { // 0.9.25: MODEL stays plain: the name and the chips say it
			a.touchLayer(c, col)
		}
	}
	switch pg.name {
	case "EQ":
		a.graphEQ(c, col)
	case "SUB + FILTER":
		a.graphFilter(c, col)
	case "AMP":
		a.graphAmp(c, col)
	case "MODEL":
		a.graphModel(c, col)
	case "SOUND":
		a.graphSound(c, col)
	case "WOBBLE":
		a.graphWobble(c, col)
	case "DUB DELAY":
		a.graphDelay(c, col)
	case "REVERB":
		a.graphReverb(c, col)
	case "TAPE":
		a.graphTape(c, col)
	case "WARMTH":
		a.graphWarmth(c, col)
	case "GLOBAL":
		a.graphGlobal(c, col)
	case "GROOVE":
		a.graphGroove(c, col)
	}
	// the flag rides with the knob being turned (the filter page has its own cutoff chip)
	if kk, _ := a.turning(); kk >= 0 && pg.name == "GLOBAL" {
		a.knobDot(c, 16, 304, 131) // just a dot along the bottom: the big BPM stays clear
	} else if kk >= 0 && !(pg.name == "SUB + FILTER" && a.cur(kk).key == "cutoff") && pg.name != "MODEL" {
		a.knobFlag(c, 16, 56, 304, 134) // chip below the graph's own top labels
	}
	for k := 0; k < 3; k++ {
		pr := a.cur(k)
		alt := a.shift && pg.alt[k] != nil
		v := a.val(pr.key)
		n := pr.norm(v)
		shown := pr.show(v)
		if pr.key == "harmonics" {
			if nm := harmonicsName(int(a.val("engine")), v); nm != "" {
				shown = nm
			}
		}
		neutral := -1.0
		if bipolarKeys[pr.key] {
			neutral = 0.5
		}
		kc := col
		if pr.key == "eq_low" || pr.key == "eq_mid" || pr.key == "eq_high" {
			if eqDb(v) <= -35.5 {
				kc = uRed
			}
		}
		active := a.lastKey == pr.key
		x := 62 + float64(k)*98
		if a.sq.resetKey == pr.key && time.Since(a.sq.resetAt) < 400*time.Millisecond {
			c.Glow(x, 169, 32, uLime, 0.35)
		}
		uiKnob(c, x, 169, 18.5, n, neutral, pr.label, shown, kc, active, alt)
		tagA := 0.8
		if a.ui.touchKey == pr.key && time.Since(a.ui.touchAt) < touchHold+touchFade {
			tagA = 1
		}
		c.RoundRect(x-7, 204, 14, 2.6, 1.3, knobCols[k], tagA)
	}
	a.uiBottom(c, pageHints[pg.name])
}

// ------------------------------------------------------------------ steps

func (a *app) drawStepsV2(c *gfx.Canvas) {
	p := a.pat()
	t := a.proj.Track
	col := tcol(t)
	c.Text(10, 32, fmt.Sprintf("STEPS  P%d  ·  %d", a.sq.viewPat+1, p.Len), gfx.Label, uDim, gfx.Left|gfx.Mid)
	if a.sq.noFollow {
		c.Text(310, 32, "HOLD BAR", gfx.Label, uOrange, gfx.Right|gfx.Mid)
	} else {
		c.Text(310, 32, "FOLLOW", gfx.Label, uLime, gfx.Right|gfx.Mid)
	}
	const x0, y0, dx, dy = 31.0, 50.0, 17.6, 20.0
	bars := bars(p.Len)
	playing := a.sq.playing && a.sq.posPat == a.sq.viewPat
	for r := 0; r < bars; r++ {
		y := y0 + float64(r)*dy
		lc := uint32(uFaint)
		if r == a.sq.stepPage {
			c.RoundRect(6, y-9, 308, 18, 9, uPanel, 1)
			lc = uLime
		}
		c.Text(13, y, fmt.Sprintf("%d", r+1), gfx.Label, lc, gfx.Center|gfx.Mid)
	}
	tails := noteTails(p, t)
	// long notes: capsules under the heads
	for s := 0; s < p.Len; s++ {
		st := &p.S[t][s]
		if !st.On || st.Gate <= 1 {
			continue
		}
		row, cc := s/16, s%16
		end := math.Min(float64(cc)+st.Gate, 16)
		c.RoundRect(x0+float64(cc)*dx, y0+float64(row)*dy-2.5, (end-float64(cc)-1)*dx+4, 5, 2.5, col, 0.45)
	}
	_ = tails
	for s := 0; s < p.Len; s++ {
		row, cc := s/16, s%16
		x, y := x0+float64(cc)*dx, y0+float64(row)*dy
		st := &p.S[t][s]
		if st.On {
			x += st.Micro * dx
			r := 2.6 + 3.2*st.Vel
			if st.Gate < 0.4 {
				r *= 0.8
			}
			c.Circle(x, y, r, col, 0.45+0.55*st.Vel)
		} else if cc%4 == 0 {
			c.Circle(x, y, 1.6, 0x5A5A52, 1)
		} else {
			c.Circle(x, y, 1.0, 0x3C3C37, 1)
		}
		if len(st.Locks) > 0 {
			c.Ring(x, y, 8, 1, uLime, 0.9)
		}
		if a.sq.held == s+1 {
			c.Ring(x, y, 9, 1.6, uOrange, 1)
		}
	}
	if playing && a.sq.posStep < p.Len {
		s := a.sq.posStep
		px, py := x0+float64(s%16)*dx, y0+float64(s/16)*dy
		c.Glow(px, py, 12, uText, 0.4)
		c.Circle(px, py, 3.6, uText, 1)
	}
	// card: the held step, or how to use this view
	cy := y0 + float64(bars-1)*dy + 16
	if cy < 124 {
		cy = 124
	}
	if a.sq.held > 0 {
		s := a.sq.held - 1
		st := &p.S[t][s]
		c.RoundRect(8, cy, 304, 44, 10, uPanel, 1)
		c.Dots(16, cy+7, fmt.Sprintf("%d", s+1), dotGlyph, 2.2, uText, 0x2A2A25)
		c.Text(56, cy+15, noteName(st.Note), gfx.Big, uText, gfx.Left|gfx.Mid)
		c.Text(56, cy+32, fmt.Sprintf("STEP %d · BAR %d", s+1, s/16+1), gfx.Small, uDim, gfx.Left|gfx.Mid)
		bar := func(y float64, lab string, v float64, txt string) {
			c.Text(132, y, lab, gfx.Label, uFaint, gfx.Left|gfx.Mid)
			c.RoundRect(154, y-2.5, 56, 5, 2.5, uPanel2, 1)
			c.RoundRect(154, y-2.5, 56*math.Max(0.06, math.Min(1, v)), 5, 2.5, col, 1)
			c.Text(214, y, txt, gfx.Small, uDim, gfx.Left|gfx.Mid)
		}
		bar(cy+10, "VEL", st.Vel, fmt.Sprintf("%d", int(st.Vel*99+0.5)))
		bar(cy+22, "LEN", math.Log2(st.Gate*10+1)/math.Log2(161), strings.Replace(gateName(st.Gate), " STEP", "", 1))
		c.Text(132, cy+34, "TIME", gfx.Label, uFaint, gfx.Left|gfx.Mid)
		c.Line(154, cy+34, 210, cy+34, 1, uPanel2, 1)
		c.Line(182, cy+30, 182, cy+38, 0.6, uFaint, 1)
		c.Circle(182+st.Micro/0.45*26, cy+34, 2.6, uText, 1)
		for i, l := range st.Locks {
			if i >= 2 {
				c.Text(304, cy+38, fmt.Sprintf("+%d", len(st.Locks)-2), gfx.Small, uLime, gfx.Right|gfx.Mid)
				break
			}
			ly := cy + 6 + float64(i)*16
			c.RoundRect(238, ly, 68, 13, 6.5, uLime, 0.16)
			c.Circle(245, ly+6.5, 2, uLime, 1)
			name := strings.ToUpper(strings.ReplaceAll(l.Key, "_", " "))
			if len(name) > 7 {
				name = name[:7]
			}
			c.Text(250, ly+6.5, name, gfx.Label, uLime, gfx.Left|gfx.Mid)
			c.Text(302, ly+6.5, lockShortShow(l), gfx.Label, uText, gfx.Right|gfx.Mid)
		}
	} else {
		c.RoundRect(8, cy, 304, 44, 10, uPanel, 1)
		lines := []string{"TAP PAD: STEP ON / OFF   ·   HOLD PAD + K1 K2 K3: NOTE VEL LENGTH",
			"HOLD PAD + SHIFT + K: LOCK A SOUND   ·   HOLD + DATA: TIMING",
			"DATA: LENGTH · PAD BANK: BAR · ERASE+PAD: CLEAR · SHIFT+ERASE x2: TRACK"}
		for i, l := range lines {
			c.Text(16, cy+10+float64(i)*12, l, gfx.Small, uDim, gfx.Left|gfx.Mid)
		}
	}
	// the other tracks: one row of tiny dots each
	oy := cy + 52
	sx := math.Min(4.6, 288/float64(max(p.Len, 1)))
	for tr := 0; tr < seq.Tracks; tr++ {
		for s := 0; s < p.Len; s++ {
			if p.S[tr][s].On {
				al := 0.5
				if tr == t {
					al = 1
				}
				if a.proj.Tracks[tr].Muted {
					al = 0.15
				}
				c.Circle(12+float64(s)*sx+float64(s/16)*1.2, oy+float64(tr)*3, 0.95, tcol(tr), al)
			}
		}
	}
	if playing {
		px := 12 + float64(a.sq.posStep)*sx + float64(a.sq.posStep/16)*1.2
		c.Line(px, oy-3, px, oy+23, 0.7, uText, 0.8)
	}
	if !a.tourDone && a.tourIdx < len(a.tour) || (a.status != "" && time.Since(a.statusAt) < 4*time.Second) {
		a.uiBottom(c, "")
	}
}

func lockShortShow(l seq.Lock) string {
	s := lockShow(l)
	s = strings.TrimSuffix(s, " DB")
	if len(s) > 5 {
		s = s[:5]
	}
	return s
}

// ------------------------------------------------------------------ frame

// drawV2 draws the new-style screens (play view pages and step view) and
// returns false for views still drawn the old way.
func (a *app) drawV2() bool {
	mockKnobsDrawn = false
	var ok bool
	if a.sq.muteMode != muteOff && a.mode != modeConfirmExit {
		a.scr.C.Clear(uBG)
		a.uiHeader(a.scr.C)
		a.drawMutes(a.scr.C)
		ok = true
	} else {
		ok = a.drawV2Page()
	}
	if ok {
		a.drawPickup(a.scr.C)
	}
	if ok && a.util.menu {
		a.drawProjectMenu(a.scr.C)
	} else if ok && a.utilShown() {
		a.drawUtil(a.scr.C)
	}
	return ok
}

func (a *app) drawV2Page() bool {
	a.specSource()
	c := a.scr.C
	c.Clear(uBG)
	a.uiHeader(c)
	if a.mt.view && a.mode != modeConfirmExit {
		a.drawMixtape(c)
		return true
	}
	if a.smp.view != smpOff {
		a.drawSampler(c)
		return true
	}
	switch a.mode {
	case modeStep:
		a.drawStepsV2(c)
	case modePlay:
		if a.ui.scope == 1 {
			a.drawScopeV2(c)
		} else if a.ui.scope == 2 {
			a.drawFlowV2(c)
		} else if a.ui.scope == 4 {
			a.drawMeltV2(c)
		} else if a.ui.scope == 5 {
			a.drawCollageV2(c)
		} else if a.ui.scope == 3 {
			a.drawDotsV2(c)
		} else if seq.IsSample(a.proj.Track) {
			a.drawSampleTrack(c)
		} else {
			a.drawParamPage(c)
		}
	case modeTracks:
		a.drawTracksV2(c)
	case modePatterns:
		a.drawPatternsV2(c)
	case modeFX:
		a.drawFXV2(c)
	case modeProject:
		a.drawProjectsV2(c)
	case modePreset:
		a.drawSoundsV2(c)
	case modeConfirmExit:
		a.drawExitV2(c)
	case modeRack:
		a.drawRack(c)
	default:
		return false
	}
	return true
}

// present flushes the frame, animating a page slide when one is pending.
func (a *app) present() {
	s := a.scr
	start := time.Now()
	s.Flush()
	d := time.Since(start)
	if s.LastArea > 0 {
		a.ui.sent++
		a.ui.areaSum += s.LastArea
	}
	a.ui.frames++
	a.ui.drawSum += d
	if d > a.ui.drawMax {
		a.ui.drawMax = d
	}
}

// startSlide: the page switches at once; only its title slides in.
func (a *app) startSlide(dir int) {
	a.ui.titleDir, a.ui.titleAt = dir, time.Now()
	a.animate(a.ui.titleAt.Add(titleAnim + 20*time.Millisecond))
}

// uiStats logs drawing cost every 10 s (how much animation the panel allows).
func (a *app) uiStats(drawTime time.Duration) {
	if time.Since(a.ui.statsAt) < 10*time.Second || a.ui.frames == 0 {
		return
	}
	area := 0
	if a.ui.sent > 0 {
		area = a.ui.areaSum / a.ui.sent
	}
	a.log("UI_STATS frames=%d sent=%d avg_area_px=%d avg_present_ms=%.1f max_present_ms=%.1f last_draw_ms=%.1f",
		a.ui.frames, a.ui.sent, area, float64(a.ui.drawSum.Microseconds())/1000/float64(a.ui.frames),
		float64(a.ui.drawMax.Microseconds())/1000, float64(drawTime.Microseconds())/1000)
	a.ui.frames, a.ui.sent, a.ui.areaSum, a.ui.drawSum, a.ui.drawMax, a.ui.statsAt = 0, 0, 0, 0, 0, time.Now()
}

// ------------------------------------------------------------------ overlays

// sheet dims the screen and draws a bottom sheet from y to the bottom.
func sheet(c *gfx.Canvas, y float64) {
	c.Dim(0, 0, c.W, c.H, 0.35)
	c.RoundRect(10, y, 300, 240-y+12, 16, 0x151613, 1)
	c.RoundRect(146, y+5, 28, 3, 1.5, uFaint, 1)
}

func (a *app) overlayTempo(c *gfx.Canvas) {
	sheet(c, 92)
	c.Text(22, 112, "TEMPO", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(22, 128, "HOLD TAP TEMPO + TURN DATA", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(22, 140, "OR RELEASE AND KEEP TAPPING", gfx.Small, uDim, gfx.Left|gfx.Mid)
	bpm := fmt.Sprintf("%d", int(math.Round(a.proj.G["tempo"])))
	w := float64(len(bpm)) * 6 * 6
	c.Dots(292-w, 104, bpm, dotGlyph, 6, uLime, 0x2A3018)
	c.Text(292, 160, "BPM", gfx.Label, uDim, gfx.Right|gfx.Mid)
	// a beat pulse row
	beat := int(time.Now().UnixMilli()/int64(60000/math.Max(40, a.proj.G["tempo"]))) % 4
	for i := 0; i < 4; i++ {
		col := uint32(uFaint)
		if i == beat {
			col = uLime
		}
		c.Circle(30+float64(i)*16, 190, 4, col, 1)
	}
	a.dirty = true
}

func (a *app) overlayLength(c *gfx.Canvas) {
	sheet(c, 104)
	p := a.livePattern()
	n := a.proj.Pat[p].Len
	c.Text(22, 124, fmt.Sprintf("LENGTH  ·  PATTERN %d", p+1), gfx.Body, uText, gfx.Left|gfx.Mid)
	c.Dots(222, 114, fmt.Sprintf("%2d", n), dotGlyph, 4, uLime, 0x2A3018)
	labels := []string{"1", "2", "4", "8", "1 BAR", "2 BARS", "4 BARS"}
	for i, l := range labels {
		x := 26 + float64(i%4)*72
		y := 156 + float64(i/4)*26
		on := lengthPads[i] == n
		bg, fg := uint32(uPanel2), uint32(uText)
		if on {
			bg, fg = uLime, uBG
		}
		c.RoundRect(x-4, y-9, 66, 18, 9, bg, 1)
		c.Text(x+3, y, fmt.Sprintf("%d", i+1), gfx.Label, gfx.Mix(fg, bg, 0.4), gfx.Left|gfx.Mid)
		c.Text(x+58, y, l, gfx.Label, fg, gfx.Right|gfx.Mid)
	}
	c.Text(26+3*72-4+33, 182, "15 -   16 +", gfx.Label, uDim, gfx.Center|gfx.Mid)
	c.Text(160, 222, "PADS 1-7 LENGTH  ·  15/16 ONE STEP  ·  STAYS ON THE BEAT", gfx.Small, uFaint, gfx.Center|gfx.Mid)
}

func clamp01f(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func indexOfOK(xs []float64, v float64) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
