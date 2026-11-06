package main

// FM-STEP 0.9.6: the live spectrum. fmstep-dsp streams 48 log-spaced band
// levels of the mix (~25 per second, "SPEC" lines); the UI smooths them
// (instant rise, slow fall, peak hold) and draws them behind the EQ curve and
// on the visualizer view (16 LEVELS).

import (
	"fmt"
	"math"
	"strings"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/seq"
)

const specBands = 48

type specState struct {
	show  [specBands]float64 // displayed dB
	peak  [specBands]float64 // peak-hold dB
	held  [specBands]time.Time
	lvl   [2]float64 // displayed L/R peak (linear)
	lvlPk [2]float64
	at    time.Time
	data  time.Time // when the last SPEC arrived
	amb   [specBands]float64 // slow-moving copy for the ambient dot field
	src   int               // what the DSP analyses: track index, -1 = mix, -2 = not set
	srcAt time.Time
}

// specSource tells the DSP what to analyse: the selected track on the
// parameter pages (so its EQ and sub show), the whole mix elsewhere.
func (a *app) specSource() {
	want := -1
	if a.mode == modePlay && a.ui.scope == 0 {
		want = a.proj.Track
	}
	sp := &a.ui.spec
	if a.dsp == nil || (want == sp.src && !sp.srcAt.IsZero()) {
		return
	}
	sp.src, sp.srcAt = want, time.Now()
	if want >= 0 {
		a.dsp.send("SPEC 1 %d", want)
	} else {
		a.dsp.send("SPEC 1")
	}
}

// specFreq: centre frequency of band b (35 Hz .. 16 kHz, log spaced).
func specFreq(b float64) float64 { return 35 * math.Pow(16000.0/35, (b+0.5)/specBands) }

// updateSpec pulls the newest SPEC data and moves the display toward it.
func (a *app) updateSpec() {
	sp := &a.ui.spec
	now := time.Now()
	dt := now.Sub(sp.at).Seconds()
	if sp.at.IsZero() || dt > 0.5 {
		dt = 0.05
		for b := range sp.show {
			if sp.at.IsZero() {
				sp.show[b], sp.peak[b] = -90, -90
			}
		}
	}
	sp.at = now
	var in [specBands]float64
	var pl, pr float64
	fresh := false
	if a.dsp != nil {
		a.dsp.specMu.Lock()
		in, pl, pr = a.dsp.spec, a.dsp.peakL, a.dsp.peakR
		fresh = a.dsp.specAt.After(sp.data)
		if fresh {
			sp.data = a.dsp.specAt
		}
		a.dsp.specMu.Unlock()
	}
	stale := now.Sub(sp.data) > 300*time.Millisecond
	for b := 0; b < specBands; b++ {
		target := in[b]
		if stale {
			target = -90
		}
		if target > sp.show[b] {
			sp.show[b] += (target - sp.show[b]) * math.Min(1, dt*30) // fast rise
		} else {
			sp.show[b] = math.Max(target, sp.show[b]-dt*36) // fall 36 dB/s
		}
		sp.amb[b] += (sp.show[b] - sp.amb[b]) * math.Min(1, dt*2.5) // slow: fades in and out
		if sp.show[b] >= sp.peak[b] {
			sp.peak[b], sp.held[b] = sp.show[b], now
		} else if now.Sub(sp.held[b]) > 700*time.Millisecond {
			sp.peak[b] = math.Max(sp.show[b], sp.peak[b]-dt*20)
		}
	}
	for i, v := range []float64{pl, pr} {
		if stale || !fresh {
			v = 0
		}
		if v > sp.lvl[i] {
			sp.lvl[i] = v
		} else {
			sp.lvl[i] *= math.Exp(-dt * 6)
		}
		if sp.lvl[i] > sp.lvlPk[i] {
			sp.lvlPk[i] = sp.lvl[i]
		} else {
			sp.lvlPk[i] *= math.Exp(-dt * 1.2)
		}
	}
	// keep animating while sound is moving
	active := !stale
	for b := 0; b < specBands && !active; b++ {
		active = sp.show[b] > -88 || sp.peak[b] > -88
	}
	if active {
		a.animate(now.Add(60 * time.Millisecond))
	}
}

// specCurve: the smoothed spectrum as a curve over [x0,x1] (log frequency,
// 20 Hz .. 20 kHz like the EQ graph), dB mapped from dbMin..dbMax to y1..y0.
func (a *app) specCurve(vals *[specBands]float64, x0, y0, x1, y1, dbMin, dbMax float64) ([]float64, []float64) {
	n := 120
	xs, ys := make([]float64, 0, n+2), make([]float64, 0, n+2)
	val := func(i int) float64 { return vals[max(0, min(specBands-1, i))] }
	for k := 0; k <= n; k++ {
		f := 20 * math.Pow(1000, float64(k)/float64(n))
		// fractional band index of f, Catmull-Rom between band centres
		bf := math.Log(f/35)/math.Log(16000.0/35)*specBands - 0.5
		bf = math.Max(0, math.Min(specBands-1, bf))
		i := int(bf)
		t := bf - float64(i)
		p0, p1, p2, p3 := val(i-1), val(i), val(i+1), val(i+2)
		v := 0.5 * (2*p1 + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t*t + (-p0+3*p1-3*p2+p3)*t*t*t)
		if f > 17000 { // roll off past the last band
			v -= (f - 17000) / 3000 * 30
		}
		y := y1 - (v-dbMin)/(dbMax-dbMin)*(y1-y0)
		xs = append(xs, logX(f, x0, x1))
		ys = append(ys, math.Max(y0, math.Min(y1, y)))
	}
	return xs, ys
}

// ------------------------------------------------------------------ visualizer

func (a *app) drawScopeV2(c *gfx.Canvas) {
	a.updateSpec()
	sp := &a.ui.spec
	col := tcol(a.proj.Track)
	// title row: what is playing
	c.Text(10, 31, "VISUALIZER", gfx.Big, uText, gfx.Left|gfx.Mid)
	info := fmt.Sprintf("%d BPM", int(math.Round(a.proj.G["tempo"])))
	if a.sq.playing && a.sq.posPat >= 0 {
		info = fmt.Sprintf("P%d  ·  BAR %d  ·  %s", a.sq.posPat+1, a.sq.posStep/16+1, info)
	}
	c.Text(310, 32, info, gfx.Label, uDim, gfx.Right|gfx.Mid)

	const x0, y0, x1, y1 = 10.0, 42.0, 286.0, 170.0
	c.RoundRect(6, 39, 284, 134, 10, uPanel, 1)
	// grid: + marks at frequency / level crossings
	for _, f := range []float64{50, 100, 200, 500, 1000, 2000, 5000, 10000} {
		x := logX(f, x0, x1)
		for _, db := range []float64{-12, -24, -36, -48, -60} {
			y := y1 - (db+72)/72*(y1-y0)
			c.Line(x-1.5, y, x+1.5, y, 0.6, 0xFFFFFF, 0.10)
			c.Line(x, y-1.5, x, y+1.5, 0.6, 0xFFFFFF, 0.10)
		}
	}
	for _, l := range []struct {
		f float64
		t string
	}{{50, "50"}, {100, "100"}, {500, "500"}, {1000, "1K"}, {5000, "5K"}, {10000, "10K"}} {
		c.Text(logX(l.f, x0, x1), 165, l.t, gfx.Small, uFaint, gfx.Center|gfx.Mid)
	}
	// peak hold (faint), then the spectrum: flat fill in the track colour,
	// a soft halo and a crisp white line
	pxs, pys := a.specCurve(&sp.peak, x0, y0, x1, y1, -72, 0)
	c.Polyline(pxs, pys, 0.8, uText, 0.25)
	xs, ys := a.specCurve(&sp.show, x0, y0, x1, y1, -72, 0)
	zoneFill(c, xs, ys, y1, x0, x1)
	// a knob being turned washes the spectrum in its colour, and a knob that
	// sets a frequency marks it on the spectrum
	kk, env := a.turning()
	if kk >= 0 {
		c.FillBetween(xs, ys, y1, knobCols[kk], 0.30*env)
	}
	a.knobFlag(c, x0, y0, x1, y1)
	c.Polyline(xs, ys, 3, col, 0.25)
	c.Polyline(xs, ys, 1.3, uText, 1)

	// L / R meters
	for i := 0; i < 2; i++ {
		x := 297 + float64(i)*9
		c.RoundRect(x, 42, 5, 128, 2.5, uPanel, 1)
		db := 20 * math.Log10(sp.lvl[i]+1e-6)
		h := math.Max(0, math.Min(1, (db+48)/48)) * 128
		mc := uint32(0x5FD414)
		if db > -6 {
			mc = uOrange
		}
		if db > -1 {
			mc = uRed
		}
		c.RoundRect(x, 170-h, 5, h, 2.5, mc, 1)
		pk := 20 * math.Log10(sp.lvlPk[i]+1e-6)
		py := 170 - math.Max(0, math.Min(1, (pk+48)/48))*128
		c.Line(x, py, x+5, py, 1, uText, 0.8)
	}
	c.Text(299.5, 177, "L", gfx.Small, uFaint, gfx.Center|gfx.Mid)
	c.Text(308.5, 177, "R", gfx.Small, uFaint, gfx.Center|gfx.Mid)

	// tracks: a light per track that flashes on each hit
	now := time.Now()
	for t := 0; t < seq.Tracks; t++ {
		x := 14 + float64(t)*16
		hit := now.Sub(a.sq.flash[t])
		tc := tcol(t)
		if a.proj.Tracks[t].Muted {
			c.Circle(x, 186, 3, uFaint, 1)
			continue
		}
		if hit < 300*time.Millisecond {
			g := 1 - float64(hit)/float64(300*time.Millisecond)
			c.Glow(x, 186, 9, tc, 0.7*g)
			c.Circle(x, 186, 4, tc, 1)
		} else {
			c.Circle(x, 186, 3, tc, 0.35)
		}
	}
	if a.sq.playing {
		a.animate(now.Add(100 * time.Millisecond))
	}
	// effects that are on
	x := 140.0
	for i, ft := range faderTargets {
		if !a.fxOn(i) {
			continue
		}
		lab := strings.SplitN(ft.label, " ", 2)[0]
		w := gfx.Small.Width(lab) + 10
		if x+w > 312 {
			break
		}
		c.RoundRect(x, 180, w, 12, 6, rgb7(ft.col), 0.85)
		c.Text(x+w/2, 186, lab, gfx.Small, uBG, gfx.Center|gfx.Mid)
		x += w + 4
	}
	a.knobBars(c, 199)
	a.uiBottom(c, "16 LEVELS: FLOW  ·  TURN K1-K3 AND WATCH")
}

// zoneFill fills under the spectrum in the three knob colours by frequency
// zone (lows cyan, mids orange, highs magenta), blending softly where the
// zones meet, in flat colour per column (smooth on the panel).
func zoneFill(c *gfx.Canvas, xs, ys []float64, base, x0, x1 float64) {
	edge := func(f, at, width float64) float64 { // 0 below at, 1 above, smooth
		t := (math.Log2(f) - math.Log2(at)) / width
		t = math.Max(0, math.Min(1, t+0.5))
		return t * t * (3 - 2*t)
	}
	j := 0
	for px := int(xs[0]); px <= int(xs[len(xs)-1]); px++ {
		fx := float64(px) + 0.5
		for j+1 < len(xs)-1 && xs[j+1] < fx {
			j++
		}
		t := 0.0
		if xs[j+1] != xs[j] {
			t = math.Max(0, math.Min(1, (fx-xs[j])/(xs[j+1]-xs[j])))
		}
		y := ys[j] + (ys[j+1]-ys[j])*t
		f := 20 * math.Pow(1000, (fx-x0)/(x1-x0))
		m := edge(f, 250, 1.6)  // lows -> mids
		h := edge(f, 3500, 1.6) // mids -> highs
		colr := gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], m), knobCols[2], h)
		for py := int(y); py < int(base); py++ {
			cov := math.Min(1, float64(py)+1-y)
			c.Blend(px, py, colr, 0.32*cov)
		}
	}
}

// ambientDots: a field of tiny dots behind a parameter graph that lights up
// with the track's sound. Each page moves in its own way:
//   columns  (MODEL, SOUND)          levels per frequency, low to high
//   mirror   (SUB + FILTER)          levels grow out from the centre line
//   ripple   (AMP)                   rings pushed out by the low end
//   rain     (DUB DELAY, REVERB)     levels fall like echoes and tails
//   wave     (WARMTH, WOBBLE, GROOVE) a dotted wave whose swell is the level
//   twinkle  (TAPE, GLOBAL)          random flicker, like hiss and dust
func (a *app) ambientDots(c *gfx.Canvas) {
	a.updateSpec()
	sp := &a.ui.spec
	const x0, x1, y0, y1, pitch = 16.0, 304.0, 48.0, 132.0, 6.0
	rows := int((y1-y0)/pitch) + 1
	style := "columns"
	switch pages[a.pageIdx].name {
	case "SUB + FILTER":
		style = "mirror"
	case "AMP":
		style = "ripple"
	case "DUB DELAY", "REVERB":
		style = "rain"
	case "WARMTH", "WOBBLE", "GROOVE":
		style = "wave"
	case "TAPE", "GLOBAL":
		style = "twinkle"
	}
	band := func(lo, hi int) float64 { // average level 0..1 of bands lo..hi
		sum := 0.0
		for b := lo; b <= hi; b++ {
			sum += math.Max(0, math.Min(1, (sp.amb[b]+66)/60))
		}
		return sum / float64(hi-lo+1)
	}
	low, all := band(0, 12), band(0, specBands-1)
	t := time.Since(appStart).Seconds()
	beat := t * math.Max(40, a.proj.G["tempo"]) / 60
	for x := x0; x <= x1; x += pitch {
		f := 20 * math.Pow(1000, (x-gx0)/(gx1-gx0))
		bf := math.Log(f/35)/math.Log(16000.0/35)*specBands - 0.5
		bf = math.Max(0, math.Min(specBands-1, bf))
		i := int(bf)
		ft := bf - float64(i)
		v := sp.amb[i]*(1-ft) + sp.amb[min(i+1, specBands-1)]*ft
		col := math.Max(0, math.Min(1, (v+66)/60)) // this column's level 0..1
		colr := gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], zoneW(f, 250)), knobCols[2], zoneW(f, 3500))
		for r := 0; r < rows; r++ {
			y := y1 - float64(r)*pitch
			lit := 0.0
			switch style {
			case "columns":
				lit = col*float64(rows) - float64(r)
			case "mirror":
				d := math.Abs(float64(r) - float64(rows-1)/2)
				lit = col*float64(rows)/2 - d
			case "ripple":
				d := math.Hypot(x-60, (y-90)*1.4)
				w := math.Sin(d*0.16 - t*5)
				lit = (w - 0.4) * 2 * (0.25 + low) * math.Max(0, 1-d/320)
			case "rain":
				// streaks drop from the level line downward and fade
				top := float64(rows) * col
				if float64(r) < top {
					ph := math.Mod(beat*2+float64(int(x))*0.37-float64(r)*0.18, 1)
					lit = (1 - ph) * (0.4 + 0.6*float64(r)/math.Max(1, top))
				}
			case "wave":
				mid := float64(rows-1) / 2
				yy := mid + (1+5*all)*math.Sin(x*0.045+beat*math.Pi*0.5)
				lit = (1 - math.Abs(float64(r)-yy)/1.6) * (0.3 + col)
			case "twinkle":
				h := math.Sin(float64(int(x)*131+r*977)) * 43758.5453
				ph := h - math.Floor(h)
				lit = math.Sin(t*(1.5+ph*3)+ph*6.28)*1.2 - 1 + 1.4*col
			}
			lit = math.Max(0, math.Min(1, lit))
			if lit > 0.02 {
				c.Circle(x, y, 0.85, colr, 0.06+0.28*lit)
			} else {
				c.Circle(x, y, 0.55, 0xFFFFFF, 0.035)
			}
		}
	}
}

// zoneW: 0 below the crossover frequency at, 1 above, smooth over ~1.6 octaves.
func zoneW(f, at float64) float64 {
	t := (math.Log2(f)-math.Log2(at))/1.6 + 0.5
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// turning: which knob (0..2) is being turned on this page and how strongly
// to show it (1 while turning, fading after), or -1.
func (a *app) turning() (int, float64) {
	dt := time.Since(a.ui.touchAt)
	if a.ui.touchKey == "" || dt > touchHold+touchFade {
		return -1, 0
	}
	for k := 0; k < 3; k++ {
		if a.knobParam(k).key == a.ui.touchKey {
			e := 1.0
			if dt > touchHold {
				e = 1 - float64(dt-touchHold)/float64(touchFade)
			}
			return k, e
		}
	}
	return -1, 0
}

// paramFreq: the frequency a parameter sets, for parameters that set one.
func (a *app) paramFreq(key string) (float64, bool) {
	switch key {
	case "cutoff":
		return 25 * math.Pow(720, a.val("cutoff")), true
	case "eq_low", "eq_low_freq":
		return eqLowHz(a.val("eq_low_freq")), true
	case "eq_mid", "eq_mid_freq":
		return eqMidHz(a.val("eq_mid_freq")), true
	case "eq_high", "eq_high_freq":
		return eqHighHz(a.val("eq_high_freq")), true
	}
	return 0, false
}

var appStart = time.Now()

// knobFlag: while a knob turns, a flag rides with it across the area
// [x0,x1] x [y0,y1]: a line in the knob's colour and a chip with the value.
// Frequency parameters (cutoff, EQ bands) sit at their frequency on a log
// axis; everything else at its position from minimum (left) to maximum
// (right). The flag fades with the knob layer.
func (a *app) knobFlag(c *gfx.Canvas, x0, y0, x1, y1 float64) {
	kk, env := a.turning()
	if kk < 0 || env <= 0 {
		return
	}
	pr := a.knobParam(kk)
	v := a.val(pr.key)
	var fx float64
	var lab string
	if f, ok := a.paramFreq(pr.key); ok {
		fx = logX(math.Max(20, math.Min(20000, f)), x0, x1)
		lab = fmt.Sprintf("%d HZ", int(f))
		if f >= 1000 {
			lab = fmt.Sprintf("%.1fK", f/1000)
		}
	} else {
		fx = x0 + (x1-x0)*pr.norm(v)
		lab = pr.label + " " + pr.show(v)
		if pr.key == "harmonics" {
			if nm := harmonicsName(int(a.val("engine")), v); nm != "" {
				lab = nm
			}
		}
	}
	// 0.9.26: just a faint line and a node; the knob and the graph already say the value
	col := knobCols[kk]
	_ = lab
	c.Line(fx, y0, fx, y1, 1, col, 0.55*env)
	c.Glow(fx, y0, 7, col, 0.4*env)
	c.Circle(fx, y0, 2.4, col, env)
	c.Circle(fx, y1, 1.8, col, 0.8*env)
}

// knobBars: the page the knobs are on, and K1-K3 as slim bars in their
// colours (the one being turned grows), so a visualizer face always says
// what the knobs change. y is the page-name line; bars sit 18 px below.
func (a *app) knobBars(c *gfx.Canvas, y float64) {
	kk, _ := a.turning()
	// the knobs still work: three slim bars in the knob colours; the one
	// being turned grows
	pg := &pages[a.pageIdx]
	c.Text(10, y, pg.name, gfx.Label, uDim, gfx.Left|gfx.Mid)
	for k := 0; k < 3; k++ {
		pr := a.cur(k)
		v := a.val(pr.key)
		n := pr.norm(v)
		bx, bw := 10+float64(k)*101, 96.0
		act := k == kk
		lc, vc := uint32(uFaint), uint32(uFaint)
		h := 3.0
		if act {
			lc, vc, h = knobCols[k], uText, 5
		}
		c.Text(bx, y+10, pr.label, gfx.Small, lc, gfx.Left|gfx.Mid)
		c.Text(bx+bw, y+10, pr.show(v), gfx.Small, vc, gfx.Right|gfx.Mid)
		by := y + 18
		c.RoundRect(bx, by-h/2, bw, h, h/2, uPanel2, 1)
		from := 0.0
		if bipolarKeys[pr.key] {
			from = 0.5
		}
		lo, hi := math.Min(from, n), math.Max(from, n)
		c.RoundRect(bx+bw*lo, by-h/2, math.Max(h, bw*(hi-lo)), h, h/2, knobCols[k], 0.55+0.45*boolF(act))
	}
}
