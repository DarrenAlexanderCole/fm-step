package main

// Drawing kit for the FLUX views (segment tabs, rules, pills, diamonds, big
// numbers, dot accents, slim knob bars) and the design mockups of all five
// screens. TAPE / CHOP / SLICE are live in sampler.go; GRID and ROLL are the
// grid editor designs, drawn by mock_flux2_test.go until they are built.

import (
	"fmt"
	"math"

	"fmstep/internal/gfx"
)

// mockKnobs: the three knobs as slim bars (like the visualizer faces), so
// the view above gets the full height.
func mockKnobs(c *gfx.Canvas, labels, vals [3]string, norms [3]float64, active int) {
	const y = 196.0
	for k := 0; k < 3; k++ {
		bx, bw := 10+float64(k)*101, 96.0
		act := k == active
		lc, vc, h := uint32(uDim), uint32(uDim), 3.0
		if act {
			lc, vc, h = knobCols[k], uText, 5
		}
		c.Text(bx, y, labels[k], gfx.Small, lc, gfx.Left|gfx.Mid)
		c.Text(bx+bw, y, vals[k], gfx.Small, vc, gfx.Right|gfx.Mid)
		c.RoundRect(bx, y+8-h/2, bw, h, h/2, uPanel2, 1)
		c.RoundRect(bx, y+8-h/2, math.Max(h, bw*norms[k]), h, h/2, knobCols[k], 0.55+0.45*boolF(act))
	}
}

// pastel line colours (from the line-art reference), next to the knob colours
const (
	pBlue   = 0x8FB8C8
	pLilac  = 0xB9A9E0
	pYellow = 0xF2D35B
	pCream  = 0xF4EFE6
)

// segTabs: joined tabs, the selected one filled; right-aligned at xr.
func segTabs(c *gfx.Canvas, xr, y float64, labels []string, sel int, fill uint32) {
	ws := make([]float64, len(labels))
	tot := 0.0
	for i, l := range labels {
		ws[i] = gfx.Small.Width(l) + 12
		tot += ws[i]
	}
	x := xr - tot
	c.RoundRectLine(x, y-6.5, tot, 13, 3, 1, uFaint, 1)
	for i, l := range labels {
		if i == sel {
			c.RoundRect(x, y-6.5, ws[i], 13, 3, fill, 1)
			c.Text(x+ws[i]/2, y, l, gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else {
			c.Text(x+ws[i]/2, y, l, gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
		if i > 0 && i != sel && i-1 != sel {
			c.Line(x, y-6, x, y+6, 1, uFaint, 1)
		}
		x += ws[i]
	}
}

// rule: a section label with a line either side.
func rule(c *gfx.Canvas, x0, x1, y float64, label string, col uint32) {
	w := gfx.Small.Width(label)
	m := (x0 + x1) / 2
	c.Line(x0, y, m-w/2-5, y, 1, col, 0.6)
	c.Line(m+w/2+5, y, x1, y, 1, col, 0.6)
	c.Text(m, y, label, gfx.Small, col, gfx.Center|gfx.Mid)
}

// pill: a value pill with a coloured dot (the dashboard's price tag).
func pill(c *gfx.Canvas, x, y float64, text string, dot uint32, right bool) {
	w := gfx.Small.Width(text) + 20
	if right {
		x -= w
	}
	c.RoundRect(x, y-6.5, w, 13, 6.5, uCream(), 1)
	c.Circle(x+7, y, 2.4, dot, 1)
	c.Text(x+12, y, text, gfx.Small, uBG, gfx.Left|gfx.Mid)
}

func uCream() uint32 { return pCream }

func diamond(c *gfx.Canvas, x, y, r float64, col uint32) {
	c.Poly([]float64{x, x + r, x, x - r}, []float64{y - r, y, y + r, y}, col, 1)
	c.Circle(x, y, r*0.3, uBG, 1)
}

// bigNum: a dot-matrix whole part and a lighter, smaller fraction.
func bigNum(c *gfx.Canvas, x, y float64, whole, frac, unit string, p float64) {
	w := c.Dots(x, y, whole, dotGlyph, p, uText, 0)
	fx := x + w + p
	fw := c.Dots(fx, y+7*p-7*p*0.6, frac, dotGlyph, p*0.6, uDim, 0)
	if unit != "" {
		c.Text(fx+fw+4, y+7*p-4, unit, gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
}

// dotAccent: a tiny dot grid (the poster's accents).
func dotAccent(c *gfx.Canvas, x, y float64, cols, rows int, col uint32) {
	for r := 0; r < rows; r++ {
		for k := 0; k < cols; k++ {
			c.Circle(x+float64(k)*3.2, y+float64(r)*3.2, 0.7, col, 0.8)
		}
	}
}

// twoWave: two smooth signed waves (lows and highs of the audio) filled to
// the centre line in flat colour, crossing each other like the reference.
// env(t) gives the loudness 0..1 at t in 0..1.
func twoWave(c *gfx.Canvas, x0, x1, ym, h float64, env func(float64) float64, ca, cb uint32, al float64, ph float64) {
	twoWaveN(c, x0, x1, ym, h, env, ca, cb, al, ph, (x1-x0)/36)
}

func twoWaveN(c *gfx.Canvas, x0, x1, ym, h float64, env func(float64) float64, ca, cb uint32, al float64, ph, cycles float64) {
	n := int(x1 - x0)
	for layer := 0; layer < 2; layer++ {
		xs, ys := make([]float64, 0, n+1), make([]float64, 0, n+1)
		cyc := cycles * (1 + 0.37*float64(layer))
		for i := 0; i <= n; i++ {
			t := float64(i) / float64(n)
			e := env(t)
			s := math.Sin(2*math.Pi*(t*cyc+ph) + float64(layer)*1.9)
			if layer == 1 {
				e *= 0.8
			}
			xs = append(xs, x0+float64(i))
			ys = append(ys, ym-s*e*h)
		}
		col := ca
		if layer == 1 {
			col = cb
		}
		// fill between the curve and the centre line, column by column
		for i := range xs {
			y := ys[i]
			lo, hi := math.Min(y, ym), math.Max(y, ym)
			c.Line(xs[i], lo, xs[i], hi, 1.2, col, al)
		}
	}
}

// envelope of the synthetic jam, smoothed (0..1 over t in 0..1, bars bars long)
func jamEnv(bars float64) func(float64) float64 {
	return func(t float64) float64 {
		b := t * bars * 4 // beats
		ph := math.Mod(b, 1)
		v := 0.35 + 0.6*math.Exp(-ph*1.4)
		if int(b)%2 == 1 {
			v *= 0.8
		}
		return math.Min(1, v*(0.85+0.15*math.Sin(t*7)))
	}
}

// ---------------------------------------------------------------- TAPE
func mock2Tape(a *app, c *gfx.Canvas) {
	bigNum(c, 10, 25, "2", ".00", "BARS TO GRAB", 2.9)
	segTabs(c, 310, 33, []string{"JAM", "INPUT", "USB"}, 0, uLime)
	const x0, x1, ym, h = 10.0, 262.0, 108.0, 42.0
	// the volume bars under it (dashboard style): level per 16th
	for i := 0; i < 64; i++ {
		x := x0 + (x1-x0)*(float64(i)+0.5)/64
		v := jamEnv(4)(float64(i) / 64)
		c.Line(x, 172, x, 172-v*16, 2, uFaint, 0.8)
	}
	gx := x0 + (x1-x0)*0.5 // GRAB: the last 2 of 4 bars
	env := jamEnv(4)
	// history: muted; the part GRAB will take: full colour
	twoWave(c, x0, gx, ym, h, func(t float64) float64 { return env(t * 0.5) }, 0x6A3A55, 0x56632E, 1, 0)
	twoWave(c, gx, x1, ym, h, func(t float64) float64 { return env(0.5 + t*0.5) }, knobCols[2], uLime, 1, 0.3)
	// dotted guide at the centre
	for x := x0; x < 308; x += 4 {
		c.Circle(x, ym, 0.6, uDim, 0.7)
	}
	// bar labels
	for b := 0; b <= 4; b++ {
		bx := x0 + (x1-x0)*float64(b)/4
		lab := fmt.Sprintf("-%d", 4-b)
		if b == 4 {
			lab = "NOW"
		}
		c.Text(math.Max(x0+5, math.Min(bx, x1-8)), 180, lab, gfx.Small, uFaint, gfx.Center|gfx.Mid)
	}
	rule(c, gx, x1, 56, "GRAB", uLime)
	c.Line(gx, 60, gx, 156, 1, uLime, 0.5)
	// the write head: line, diamond, pill
	c.Line(x1, 52, x1, 172, 1.4, uText, 1)
	diamond(c, x1, ym, 5, uText)
	pill(c, 310, ym, "REC", uRed, true)
	// input level as two thin lines on the right
	for i := 0; i < 2; i++ {
		mx := 296 + float64(i)*6
		c.Line(mx, 172, mx, 128, 2, uPanel2, 1)
		c.Line(mx, 172, mx, 172-(36+4*float64(i)), 2, 0x5FD414, 1)
	}
	dotAccent(c, 278, 64, 6, 4, uDim)
	mockKnobs(c, [3]string{"LENGTH", "GAIN", "MONITOR"}, [3]string{"2 BARS", "+3 DB", "ON"}, [3]float64{0.33, 0.6, 1}, 0)
	a.uiBottom(c, "PAD FX: GRAB  ·  SHIFT + PAD FX: SOURCE  ·  ALWAYS ROLLING")
}

// ---------------------------------------------------------------- CHOP
// The reel as line art: 16 circles in a ring, overlapping like the
// reference, each as big as its slice is loud; the playing one fills.
func mock2Chop(a *app, c *gfx.Canvas, playing int) {
	if a.sq.playing && a.sq.posPat >= 0 {
		playing = (a.sq.posStep / 2) % 16 // 2 bars over 16 slices
	}
	c.Text(10, 33, "CHOP", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"EVEN", "BEATS", "8", "32"}, 0, uText)
	const cx, cy = 77.0, 117.0
	env := jamEnv(2)
	cols := []uint32{pBlue, pCream, pLilac, pYellow}
	rule(c, 10, 144, 50, "REEL", uDim)
	for i := 0; i < 16; i++ {
		ang := -math.Pi/2 + (float64(i)+0.5)*2*math.Pi/16
		peak := 0.0
		for j := 0; j < 8; j++ {
			peak = math.Max(peak, env((float64(i)+float64(j)/8)/16))
		}
		r := 9 + 11*peak
		x, y := cx+36*math.Cos(ang), cy+36*math.Sin(ang)
		col := cols[i%4]
		if i == playing {
			c.Glow(x, y, r+8, col, 0.5)
			c.Circle(x, y, r, col, 0.9)
			c.Text(x, y, fmt.Sprintf("%d", i+1), gfx.Label, uBG, gfx.Center|gfx.Mid)
		} else {
			c.Ring(x, y, r, 1, col, 0.85)
		}
	}
	// the hub: dot-matrix slice number
	c.Ring(cx, cy, 14, 1, uFaint, 1)
	c.Dots(cx-8.5, cy-5, fmt.Sprintf("%02d", playing+1), dotGlyph, 1.5, uLime, 0)
	// pads: 4 x 4, slice 1 bottom-left like the MPC; the playing one glows
	rule(c, 162, 310, 50, "PADS", uDim)
	const px0, py0, pw = 163.0, 58.0, 34.0
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		x, y := px0+float64(col)*(pw+3), py0+float64(row)*(pw-3)
		hgt := pw - 6
		on := i == playing
		if on {
			c.Glow(x+pw/2, y+hgt/2, 26, knobCols[0], 0.55)
			c.RoundRect(x, y, pw, hgt, 4, 0xBFE9F0, 1)
		} else {
			c.RoundRect(x, y, pw, hgt, 4, uPanel2, 1)
		}
		// a tiny two-colour wave on each pad
		e := func(t float64) float64 { return env((float64(i) + t) / 16) }
		ca, cb := uint32(knobCols[2]), uint32(uLime)
		if on {
			ca, cb = 0x2A6F86, 0x1D4F5E
		}
		twoWaveN(c, x+4, x+pw-4, y+hgt/2, hgt*0.34, e, ca, cb, 1, float64(i)*0.3, 1.5)
	}
	mockKnobs(c, [3]string{"DRIFT", "CHOP", "MOTION"}, [3]string{"DILLA 40%", "EVEN 16", "TAPE"}, [3]float64{0.4, 0.25, 0.5}, 0)
	a.uiBottom(c, "PADS: PLAY SLICES  ·  MIDDLE BUTTON: SLICE  ·  REC: INTO STEPS")
}

// ---------------------------------------------------------------- SLICE
func mock2Slice(a *app, c *gfx.Canvas, sel int) {
	bigNum(c, 10, 25, "5", ".16", "SEC  ·  PAD 5", 2.9)
	segTabs(c, 310, 33, []string{"FWD", "REV"}, 0, uText)
	const x0, x1, ym, h = 10.0, 310.0, 88.0, 24.0
	env := jamEnv(2)
	sw := (x1 - x0) / 16
	sx0, sx1 := x0+sw*float64(sel), x0+sw*float64(sel+1)
	twoWave(c, x0, x1, ym, h, env, 0x6A3A55, 0x56632E, 1, 0)
	twoWaveN(c, sx0, sx1, ym, h, func(t float64) float64 { return env((float64(sel) + t) / 16) }, knobCols[2], uLime, 1, 0.2, 1.2)
	for i := 0; i <= 16; i++ {
		x := x0 + sw*float64(i)
		c.Line(x, ym-28, x, ym+28, 0.6, 0xFFFFFF, 0.12)
	}
	// start / end: lines with diamonds in the knob colours
	for k, hx := range []float64{sx0, sx1} {
		c.Line(hx, ym-32, hx, ym+30, 1.2, knobCols[k], 1)
		diamond(c, hx, ym-32, 4, knobCols[k])
	}
	// zoom: the slice as a hairline over fine bars (dashboard style)
	rule(c, 10, 310, 126, "ZOOM", uDim)
	const zx0, zx1, zy = 10.0, 310.0, 158.0
	xs, ys := []float64{}, []float64{}
	for i := 0; i <= 150; i++ {
		t := float64(i) / 150
		e := env((float64(sel) + t) / 16)
		x := zx0 + (zx1-zx0)*t
		c.Line(x, zy+18, x, zy+18-(0.15+0.85*math.Exp(-t*3.5))*18, 1, uFaint, 0.9)
		xs = append(xs, x)
		hit := math.Exp(-t*3.5) * (0.6 + 0.4*math.Sin(t*41)*math.Sin(t*13))
		ys = append(ys, zy-16*(hit*math.Sin(t*2*math.Pi*5.5)+0.08*math.Sin(t*90)))
		_ = e
	}
	c.Polyline(xs, ys, 1.2, uLime, 1)
	pill(c, 310, 138, "-2 ST", knobCols[2], true)
	dotAccent(c, 12, 134, 5, 3, uDim)
	mockKnobs(c, [3]string{"START", "END", "PITCH"}, [3]string{"+4 MS", "-12 MS", "-2 ST"}, [3]float64{0.52, 0.45, 0.42}, 0)
	a.uiBottom(c, "SHIFT + K3  REVERSE  ·  ERASE + PAD  JOIN  ·  CHOP + PAD  SPLIT")
}

// ---------------------------------------------------------------- GRID
// The warped-grid reference: thin white step lines and coloured track
// lines; a nudged note bends its step line, swing bends every other one.
func mock2Grid(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "GRID", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"BAR 1", "2", "3", "4"}, 0, uText)
	p := a.pat()
	const x0, y0, dx, dy = 40.0, 56.0, 17.0, 16.6
	swing := 0.18
	nudge := func(t, s int) float64 { // horizontal offset of the step at track t
		off := 0.0
		if s%2 == 1 {
			off += swing * dx
		}
		if t == 4 && s == 10 {
			off += 0.3 * dx // the nudged note
		}
		return off
	}
	// step lines: bend through each track's offset
	for s := 0; s < 16; s++ {
		xs, ys := []float64{}, []float64{}
		for t := 0; t <= 7; t++ {
			xs = append(xs, x0+float64(s)*dx+nudge(t, s))
			ys = append(ys, y0+float64(t)*dy)
		}
		xs = append([]float64{xs[0]}, append(xs, xs[len(xs)-1])...)
		ys = append([]float64{y0 - 8}, append(ys, y0+7*dy+8)...)
		al := 0.18
		if s%4 == 0 {
			al = 0.35
		}
		c.Polyline(xs, ys, 1, pCream, al)
	}
	for t := 0; t < 8; t++ {
		y := y0 + float64(t)*dy
		col := tcol(t)
		c.Line(x0-6, y, x0+15*dx+6, y, 1, col, 0.35)
		c.Circle(18, y, 5, col, 1)
		c.Text(18, y, fmt.Sprintf("%d", t+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		for s := 0; s < 16; s++ {
			st := p.S[t][s]
			if !st.On {
				continue
			}
			x := x0 + float64(s)*dx + nudge(t, s)
			if st.Gate > 1 {
				c.Line(x, y, x+(math.Min(st.Gate, float64(16-s))-1)*dx, y, 3, col, 0.6)
			}
			c.Circle(x, y, 2.4+3*st.Vel, col, 1)
		}
	}
	// playhead: a line with a diamond on top
	step := 6
	if a.sq.playing && a.sq.posPat >= 0 {
		step = a.sq.posStep % 16
	}
	ph := x0 + float64(step)*dx
	c.Line(ph, y0-8, ph, y0+7*dy+8, 1, uText, 0.5)
	diamond(c, ph, y0-8, 3.5, uText)
	// the cursor and its value pill
	cx, cy := x0+10*dx+nudge(4, 10), y0+4*dy
	c.Ring(cx, cy, 8, 1.4, uOrange, 1)
	pill(c, cx+12, cy-14, "NUDGE +30%", knobCols[2], false)
	mockKnobs(c, [3]string{"VELOCITY", "LENGTH", "NUDGE"}, [3]string{"92", "1/16", "+30%"}, [3]float64{0.72, 0.1, 0.65}, 2)
	a.uiBottom(c, "PADS: TOGGLE STEPS  ·  DATA: TRACK  ·  MIDDLE BUTTON: ROLL")
}

// ---------------------------------------------------------------- ROLL
// The poster reference: notes as flat blocks with a circle for velocity,
// a thin-line keyboard, dot-grid accents.
func mock2Roll(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "ROLL", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"T5 DUB SUB", "BAR 1"}, 0, tcol(4))
	col := tcol(4)
	const x0, x1, y0, rows, rh = 40.0, 310.0, 46.0, 19, 6.8
	low := 28
	black := map[int]bool{1: true, 3: true, 6: true, 8: true, 10: true}
	for r := 0; r < rows; r++ {
		n := low + r
		y := y0 + float64(rows-1-r)*rh
		if black[n%12] {
			c.Line(8, y+rh/2, 16, y+rh/2, 2, uFaint, 1)
		} else {
			c.Line(8, y+rh/2, 20, y+rh/2, 1, uDim, 0.8)
		}
		if n%12 == 0 {
			c.Text(34, y+rh/2, fmt.Sprintf("C%d", n/12-1), gfx.Small, uDim, gfx.Right|gfx.Mid)
			c.Line(x0, y+rh, x1, y+rh, 0.6, 0xFFFFFF, 0.12)
		}
	}
	dx := (x1 - x0) / 16
	for s := 0; s <= 16; s += 4 {
		for y := y0; y < y0+rows*rh; y += 3.4 {
			c.Circle(x0+float64(s)*dx, y, 0.5, uDim, 0.6)
		}
	}
	notes := []struct {
		s, n, len int
		vel       float64
	}{{0, 36, 3, 1}, {4, 36, 1, 0.6}, {6, 39, 2, 0.9}, {8, 34, 3, 1}, {12, 41, 1, 0.5}, {13, 39, 1, 0.7}, {14, 36, 2, 0.8}}
	for i, nt := range notes {
		y := y0 + float64(rows-1-(nt.n-low))*rh
		x := x0 + float64(nt.s)*dx
		w := float64(nt.len)*dx - 2
		c.Rect(x+1, y+0.5, w, rh-1, col, 0.9)
		c.Circle(x+3, y+rh/2, 1.8+3*nt.vel, pLilac, 1)
		if i == 2 {
			c.RoundRectLine(x-1, y-1.5, w+4, rh+3, 2, 1.2, uText, 1)
			pill(c, x+w+6, y+rh/2, "D#2  ·  88", pLilac, false)
		}
	}
	step := 9.0
	if a.sq.playing && a.sq.posPat >= 0 {
		step = float64(a.sq.posStep % 16)
	}
	ph := x0 + step*dx
	c.Line(ph, y0, ph, y0+rows*rh, 1, uText, 0.5)
	diamond(c, ph, y0, 3.5, uText)
	dotAccent(c, 288, 164, 6, 3, uDim)
	mockKnobs(c, [3]string{"PITCH", "LENGTH", "VELOCITY"}, [3]string{"D#2", "2/16", "88"}, [3]float64{0.5, 0.2, 0.69}, 0)
	a.uiBottom(c, "PADS: PLAY + ADD  ·  DATA: MOVE  ·  MIDDLE BUTTON: GRID")
}
