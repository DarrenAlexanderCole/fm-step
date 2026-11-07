package main

// Design round 6 (after the 0.9.26 test). FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR6

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

// a fake break wave, 0..1 per column
func r6Wave(n int, hits float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / float64(n)
		ph := math.Mod(t*hits, 1)
		out[i] = math.Min(1, math.Exp(-ph*4.5)*0.9+0.14*math.Exp(-ph*1.4)+0.04*math.Abs(math.Sin(float64(i)*1.9)))
	}
	return out
}

func r6DrawWave(c *gfx.Canvas, x0, x1, mid, amp float64, w []float64, col func(t float64) uint32, alpha float64) {
	n := len(w)
	for k := 0; k < n; k++ {
		t := float64(k) / float64(n-1)
		x := x0 + t*(x1-x0)
		cc := col(t)
		c.Line(x, mid-w[k]*amp, x, mid+w[k]*amp*0.9, 1, cc, alpha)
	}
}

// SHAPER · TRIM: the pad's sound in its source; IN / OUT like the tape
func r6ShaperTrim(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"IN · LOCK", "PLAY", "OUT"}, 2)
	c.Text(10, 46, "TRIM", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(66, 47, "PAD 05  ·  BREAK 09", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.RoundRect(250, 40, 62, 13, 6, uBG, 0.9)
	c.Text(306, 46.5, "0.48 S", gfx.Small, uText, gfx.Right|gfx.Mid)
	glass(c, 8, 58, 304, 116, 10, 0x9AA0A6)
	w := r6Wave(288, 5)
	in, out := 0.22, 0.64
	r6DrawWave(c, 16, 304, 118, 38, w, func(t float64) uint32 {
		if t < in || t > out {
			return uDim
		}
		return cvCol(16 + (t-in)/(out-in)*288)
	}, 0.55)
	xi, xo := 16+in*288, 16+out*288
	c.Rect(16, 66, xi-16, 100, uBG, 0.45)
	c.Rect(xo, 66, 304-xo, 100, uBG, 0.45)
	c.Line(xi, 66, xi, 168, 1.2, knobCols[0], 0.9)
	c.Line(xo, 66, xo, 168, 1.6, knobCols[2], 1)
	lockIcon(c, xi, 72, knobCols[0], true)
	c.Circle(xo, 72, 3, knobCols[2], 1)
	px := xi + 0.4*(xo-xi)
	c.Line(px, 70, px, 168, 1, 0xD8FF4A, 0.9)
	c.Text(24, 167, "IN 0:00.52", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(296, 167, "OUT 0:01.54", gfx.Small, uText, gfx.Right|gfx.Mid)
	mockKnobs(c, [3]string{"HEIGHT", "SNAP", "ZOOM"}, [3]string{"x1.0", "ZERO X", "2.4 S"}, [3]float64{0.2, 0.5, 0.6}, 2)
	a.uiBottom(c, "DATA: MOVE (SPIN FAST: FAR)  ·  PUSH: FINE  ·  RIGHT BUTTON IN SHAPER: TRIM")
}

// SAMPLE SELECT: folders on the card, the file's wave with a zoom
func r6Browser(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"UP A FOLDER", "LISTEN", "TO PAD 05"}, -1)
	c.Text(10, 46, "SAMPLE SELECT", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(150, 47, "/ SAMPLES / BREAKS", gfx.Small, uDim, gfx.Left|gfx.Mid)
	glass(c, 8, 58, 130, 128, 8, 0x9AA0A6)
	rows := []struct {
		name string
		dir  bool
	}{{"..", true}, {"90S", true}, {"JUNGLE", true}, {"AMEN 01.WAV", false}, {"BREAK 09.WAV", false}, {"FUNKY 3.WAV", false}, {"THINK.WAV", false}}
	for i, r := range rows {
		y := 70 + float64(i)*16
		if i == 4 {
			c.RoundRect(12, y-7, 122, 14, 4, 0xD8FF4A, 0.9)
		}
		col := uint32(uText)
		if i == 4 {
			col = uBG
		}
		pre := "  "
		if r.dir {
			pre = "/ "
			if i != 4 {
				col = 0x8FB8C8
			}
		}
		c.Text(18, y, pre+r.name, gfx.Small, col, gfx.Left|gfx.Mid)
	}
	glass(c, 144, 58, 168, 128, 8, 0x9AA0A6)
	w := r6Wave(156, 8)
	r6DrawWave(c, 150, 306, 112, 34, w, func(t float64) uint32 { return cvCol(16 + t*288) }, 0.6)
	c.Rect(150, 70, 156*0.3, 80, uBG, 0.5)
	c.Rect(150+156*0.7, 70, 156*0.3, 80, uBG, 0.5)
	c.RoundRectLine(150+156*0.3, 72, 156*0.4, 80, 4, 1, 0xD8FF4A, 0.8)
	c.Text(152, 164, "4.2 S  ·  2 BARS @ 114", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(152, 176, "ZOOM: 1.3 S FROM 1:08", gfx.Small, uText, gfx.Left|gfx.Mid)
	mockKnobs(c, [3]string{"START", "", "ZOOM"}, [3]string{"1.08 S", "", "1.3 S"}, [3]float64{0.3, 0, 0.4}, 2)
	a.uiBottom(c, "DATA: PICK  ·  PUSH: OPEN / LOAD  ·  K1 + K3: WHICH PART GOES ON THE PAD")
}

// PAD BANK in the sampler: 8 banks x 16 pads at a glance
func r6Banks(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "PAD BANKS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(108, 34, "128 SOUNDS  ·  BANK C", gfx.Small, uDim, gfx.Left|gfx.Mid)
	names := "ABCDEFGH"
	for b := 0; b < 8; b++ {
		bx := 8 + float64(b%4)*77
		by := 46 + float64(b/4)*72
		sel := b == 2
		tint := uint32(0x9AA0A6)
		if sel {
			tint = 0xD8FF4A
		}
		glass(c, bx, by, 73, 66, 6, tint)
		c.Text(bx+6, by+9, string(names[b]), gfx.Label, uText, gfx.Left|gfx.Mid)
		full := []int{16, 16, 11, 4, 0, 0, 7, 0}[b]
		c.Text(bx+68, by+9, fmt.Sprint(full), gfx.Small, uDim, gfx.Right|gfx.Mid)
		for p := 0; p < 16; p++ {
			px := bx + 6 + float64(p%4)*16.5
			py := by + 54 - float64(p/4)*11
			if p < full {
				w := r6Wave(12, 1+float64((p*7+b)%3))
				for k := 0; k < 12; k++ {
					c.Line(px+float64(k), py-w[k]*4, px+float64(k), py+w[k]*4, 1, cvCol(16+float64(k)*24), 0.8)
				}
			} else {
				c.Circle(px+6, py, 1, uDim, 0.4)
			}
		}
	}
	a.uiBottom(c, "PAD BANK: NEXT BANK  ·  DATA: PICK  ·  PUSH: OPEN  ·  ERASE + PAD: EMPTY IT")
}

// MIXER on KNOB FX: level, send A, send B, named by the RACK
func r6Mixer(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "MIXER", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(80, 34, "A  DUB DELAY   ·   B  PHASER", gfx.Small, uDim, gfx.Left|gfx.Mid)
	for t := 0; t < 8; t++ {
		x := 10 + float64(t)*38.5
		sel := t == 4
		glass(c, x, 46, 34, 134, 6, tcol(t))
		if sel {
			c.RoundRectLine(x-2, 44, 38, 138, 7, 1.2, uText, 0.85)
		}
		c.Text(x+17, 54, fmt.Sprint(t+1), gfx.Small, uText, gfx.Center|gfx.Mid)
		lv := []float64{0.8, 0.6, 0.45, 0.9, 0.7, 0.5, 0.66, 0.3}[t]
		sa := []float64{0.2, 0, 0.5, 0, 0.35, 0.1, 0, 0.6}[t]
		sb := []float64{0, 0.3, 0.1, 0.6, 0.45, 0, 0.2, 0.15}[t]
		// A and B as two small rings
		for i, v := range []float64{sa, sb} {
			cy := 70 + float64(i)*22
			col := []uint32{knobCols[1], knobCols[2]}[i]
			c.Arc(x+17, cy, 7, 2, 135, 405, uPanel2, 1)
			if v > 0 {
				c.Arc(x+17, cy, 7, 2, 135, 135+270*v, col, 1)
			}
			c.Text(x+17, cy, []string{"A", "B"}[i], gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
		c.RoundRect(x+13, 108, 8, 64, 3, uPanel2, 1)
		c.RoundRect(x+13, 108+64*(1-lv), 8, 64*lv, 3, tcol(t), 0.9)
	}
	mockKnobs(c, [3]string{"LEVEL", "SEND A", "SEND B"}, [3]string{"70", "35", "45"}, [3]float64{0.7, 0.35, 0.45}, 1)
	a.uiBottom(c, "KNOB FX: MIXER  ·  PADS 1-8: TRACK  ·  SHIFT + K1: PAN  ·  SHIFT + PAD FX: RACK")
}

// RACK master: TAPE, MASTER COMP, LIMITER with switches
func r6Master(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "MASTER", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 34, "RACK  ·  THE END OF THE CHAIN", gfx.Small, uDim, gfx.Left|gfx.Mid)
	blocks := []struct {
		name, info string
		on         bool
		col        uint32
	}{
		{"TAPE + GLUE", "DRIVE 40  ·  AGE 25", true, pYellow},
		{"MASTER COMP", "THRESH -12  ·  2:1  ·  ATTACK 30 MS", true, knobCols[1]},
		{"LIMITER", "CEILING -0.3 DB  ·  RELEASE 80 MS", true, knobCols[2]},
	}
	for i, b := range blocks {
		y := 46 + float64(i)*44
		glass(c, 8, y, 304, 40, 8, b.col)
		if i == 1 {
			c.RoundRectLine(6, y-2, 308, 44, 9, 1.2, uText, 0.85)
		}
		// the switch
		sx := 18.0
		c.RoundRect(sx, y+13, 24, 14, 7, uPanel2, 1)
		if b.on {
			c.RoundRect(sx, y+13, 24, 14, 7, b.col, 0.9)
			c.Circle(sx+17, y+20, 5, uBG, 1)
		} else {
			c.Circle(sx+7, y+20, 5, uDim, 1)
		}
		c.Text(52, y+14, b.name, gfx.Label, uText, gfx.Left|gfx.Mid)
		c.Text(52, y+28, b.info, gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
	// GR meter on the comp and limiter
	for i, gr := range []float64{3.5, 1.2} {
		y := 46 + float64(i+1)*44
		for k := 0; k < 10; k++ {
			on := float64(k) < gr*1.5
			col, al := uint32(uFaint), 0.6
			if on {
				col, al = blocks[i+1].col, 1
			}
			c.RoundRect(250+float64(k)*5.5, y+15, 4, 10, 1, col, al)
		}
	}
	mockKnobs(c, [3]string{"THRESH", "RATIO", "ATTACK"}, [3]string{"-12 DB", "2:1", "30 MS"}, [3]float64{0.5, 0.25, 0.35}, 0)
	a.uiBottom(c, "DATA: BLOCK  ·  PUSH: ON / OFF  ·  SHIFT + K1-K3: RELEASE · KNEE · MAKEUP")
}

// MIXTAPE: its own chain (not on the mix bus), pages by DATA
func r6TapeFX(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"MONITOR", "PAUSE", "TAPE FX"}, 2)
	c.Text(10, 50, "TAPE FX", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(90, 51, "ONLY ON THE TAKE  ·  NOT THE MIX", gfx.Small, uDim, gfx.Left|gfx.Mid)
	stages := []struct {
		name string
		on   bool
		col  uint32
	}{{"COMP", true, knobCols[2]}, {"EQ", true, knobCols[0]}, {"DRIVE", false, knobCols[1]}, {"WIDTH", true, pLilac}, {"SEND A", false, pYellow}}
	for i, s := range stages {
		x := 8 + float64(i)*61
		glass(c, x, 62, 57, 34, 6, s.col)
		if i == 1 {
			c.RoundRectLine(x-2, 60, 61, 38, 7, 1.2, uText, 0.85)
		}
		al := 1.0
		if !s.on {
			al = 0.4
		}
		c.Text(x+28.5, 74, s.name, gfx.Small, uText, gfx.Center|gfx.Mid)
		c.Circle(x+28.5, 87, 2.5, s.col, al)
		if i < len(stages)-1 {
			c.Text(x+59, 79, "›", gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
	}
	// the EQ curve
	glass(c, 8, 102, 304, 78, 8, knobCols[0])
	var xs, ys []float64
	for k := 0; k <= 120; k++ {
		f := float64(k) / 120
		g := -6*math.Exp(-math.Pow((f-0.08)/0.06, 2)) + 3*1/(1+math.Exp(-(f-0.75)*14)) + 2*math.Exp(-math.Pow((f-0.45)/0.1, 2))
		xs = append(xs, 16+f*288)
		ys = append(ys, 141-g*4)
	}
	c.Line(16, 141, 304, 141, 0.6, uDim, 0.4)
	c.Polyline(xs, ys, 1.6, knobCols[0], 0.95)
	for _, f := range []float64{0.08, 0.45, 0.75} {
		c.Circle(16+f*288, ys[int(f*120)], 3, uText, 1)
	}
	mockKnobs(c, [3]string{"LOW CUT", "MID", "AIR"}, [3]string{"80 HZ", "+2 DB", "+3 DB"}, [3]float64{0.3, 0.6, 0.65}, 1)
	a.uiBottom(c, "DATA: STAGE  ·  PUSH: ON / OFF  ·  RIGHT: BACK TO THE TAPE")
}

// 16 LEVELS: MELT, shapes that melt into each other with the knobs
func r6Melt(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "MELT", gfx.Big, uText, gfx.Left|gfx.Mid)
	type blob struct{ x, y, r float64 }
	bs := []blob{{80, 110, 20}, {135, 100, 26}, {200, 140, 22}, {150, 170, 15}, {250, 90, 14}, {255, 170, 12}}
	field := func(x, y float64) float64 {
		s := 0.0
		for _, b := range bs {
			d := (x-b.x)*(x-b.x) + (y-b.y)*(y-b.y)
			s += b.r * b.r / (d + 1)
		}
		return s
	}
	for y := 48.0; y < 218; y += 2 {
		for x := 4.0; x < 316; x += 2 {
			f := field(x, y)
			if f < 0.55 {
				continue
			}
			col := cvCol(x)
			al := math.Min(1, (f-0.55)*1.4)
			if f < 0.75 {
				col = 0xFFFFFF
				al = 0.5
			}
			c.Rect(x, y, 2, 2, col, al*0.85)
		}
	}
	c.Text(310, 33, "16 LEVELS: NEXT", gfx.Small, uDim, gfx.Right|gfx.Mid)
}

// 16 LEVELS: COLLAGE, cut-out shapes layered with the music
func r6Collage(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "COLLAGE", gfx.Big, uText, gfx.Left|gfx.Mid)
	rs := []struct {
		x, y, w, h, rot float64
		col             uint32
	}{
		{30, 60, 120, 70, 0, pCream}, {120, 90, 140, 90, 0, knobCols[2]}, {60, 140, 90, 60, 0, pYellow},
		{200, 54, 90, 60, 0, pLilac}, {170, 150, 120, 50, 0, knobCols[0]}, {20, 100, 60, 90, 0, pBlue},
	}
	for i, r := range rs {
		c.Rect(r.x, r.y, r.w, r.h, r.col, 0.55+0.08*float64(i%3))
		// torn edge: dots along the top
		for x := r.x; x < r.x+r.w; x += 3 {
			c.Circle(x, r.y+math.Sin(x*0.7+float64(i))*1.2, 1, r.col, 0.9)
		}
	}
	// halftone over the top
	for y := 52.0; y < 214; y += 6 {
		for x := 6.0; x < 314; x += 6 {
			v := 0.5 + 0.5*math.Sin(x/23+y/17)
			c.Circle(x, y, 0.4+1.4*v, uBG, 0.35)
		}
	}
	c.Text(310, 33, "16 LEVELS: NEXT", gfx.Small, uDim, gfx.Right|gfx.Mid)
}

func TestMockR6(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 SHAPER TRIM", func(c *gfx.Canvas) { r6ShaperTrim(a, c) }},
		{"2 SAMPLE SELECT folders zoom", func(c *gfx.Canvas) { r6Browser(a, c) }},
		{"3 PAD BANKS", func(c *gfx.Canvas) { r6Banks(a, c) }},
		{"4 MIXER KNOB FX", func(c *gfx.Canvas) { r6Mixer(a, c) }},
		{"5 RACK MASTER", func(c *gfx.Canvas) { r6Master(a, c) }},
		{"6 MIXTAPE TAPE FX", func(c *gfx.Canvas) { r6TapeFX(a, c) }},
		{"7 MELT", func(c *gfx.Canvas) { r6Melt(a, c) }},
		{"8 COLLAGE", func(c *gfx.Canvas) { r6Collage(a, c) }},
	}
	for _, s := range shots {
		c := a.scr.C
		c.Clear(uBG)
		a.statusAt = time.Time{}
		a.uiHeader(c)
		s.f(c)
		savePNG(a, filepath.Join(dir, s.name+".png"))
	}
}
