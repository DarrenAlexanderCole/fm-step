package main

// Design round 7 (after the 0.9.27 test). FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR7

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

const r7Orange = 0xE8582A

var r7Names = []string{"BOOM KICK", "BRUSH SNR", "JAZZ HAT", "JAZZ RIDE", "DUB SUB", "JAZZ CHRD", "STEPPA", "MODAL BELL",
	"BANK A", "BANK B", "BANK C", "BANK D", "BANK E", "BANK F", "BANK G", "BANK H"}

// 16 pads as they sit (1-4 bottom row), with a draw func per pad
func r7Pads(c *gfx.Canvas, y0 float64, f func(i int, x, y, w, h float64)) {
	w, h := 74.0, 36.0
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		f(i, 10+float64(col)*76, y0+float64(row)*38, w, h)
	}
}

func r7Mutes(a *app, c *gfx.Canvas, page int) {
	tabs := []string{"TRACK MUTES", "PAD MUTES", "SOLO"}
	c.Text(10, 33, "MUTE", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 312, 33, tabs, page, uText)
	muted := map[int]bool{2: true, 6: true, 11: true, 13: true}
	solo := map[int]bool{4: true, 8: true}
	r7Pads(c, 46, func(i int, x, y, w, h float64) {
		name := r7Names[i]
		col := tcol(i % 8)
		if i >= 8 {
			col = gfx.Mix(tcol(i%8), 0xFFFFFF, 0.25)
		}
		on := true
		switch page {
		case 0:
			on = !muted[i]
		case 1:
			name = fmt.Sprintf("C%02d", i+1)
			on = i != 3 && i != 7 && i != 10
		case 2:
			on = solo[i]
		}
		al := 0.9
		if !on {
			al = 0.18
		}
		glass(c, x, y, w, h, 6, col)
		c.RoundRect(x+5, y+6, 8, 8, 2, col, al)
		c.Text(x+70, y+10, fmt.Sprint(i+1), gfx.Small, uDim, gfx.Right|gfx.Mid)
		tc := uint32(uText)
		if !on {
			tc = uDim
		}
		c.Text(x+6, y+26, name, gfx.Small, tc, gfx.Left|gfx.Mid)
		if page == 0 && !on {
			c.Text(x+70, y+26, "MUTE", gfx.Small, uRed, gfx.Right|gfx.Mid)
		}
		if page == 2 && on {
			c.Text(x+70, y+26, "SOLO", gfx.Small, shLime, gfx.Right|gfx.Mid)
		}
	})
	hint := []string{"PADS: MUTE A TRACK  ·  1-8 SYNTHS  ·  9-16 SAMPLE BANKS A-H  ·  MUTE: NEXT",
		"PADS: MUTE A PAD OF TRACK 11 (BANK C)  ·  MUTE: SOLO", "PADS: SOLO  ·  MUTE: DONE  ·  SHIFT + MUTE: ALL CLEAR"}[page]
	a.uiBottom(c, hint)
}

func r7Tracks(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "TRACKS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 34, "1-8 SYNTHS  ·  9-16 SAMPLES", gfx.Small, uDim, gfx.Left|gfx.Mid)
	r7Pads(c, 46, func(i int, x, y, w, h float64) {
		col := tcol(i % 8)
		glass(c, x, y, w, h, 6, col)
		if i == 10 {
			c.RoundRectLine(x-2, y-2, w+4, h+4, 7, 1.2, uText, 0.85)
		}
		c.Circle(x+10, y+10, 5, col, 1)
		c.Text(x+10, y+10.5, fmt.Sprint(i+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		c.Text(x+6, y+27, r7Names[i], gfx.Small, uText, gfx.Left|gfx.Mid)
		// a small strip of what plays: steps
		for s := 0; s < 8; s++ {
			on := (i*3+s*5)%7 < 3
			c.RoundRect(x+22+float64(s)*6, y+7, 4, 6, 1, col, map[bool]float64{true: 0.9, false: 0.15}[on])
		}
	})
	a.uiBottom(c, "PADS: PICK THE TRACK  ·  SEQ AGAIN: STEP EDIT OF TRACK 11")
}

func r7Mixer16(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "MIXER", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Circle(170, 33, 3, knobCols[1], 1)
	c.Text(177, 33.5, "A DUB DELAY", gfx.Small, uText, gfx.Left|gfx.Mid)
	c.Circle(246, 33, 3, knobCols[2], 1)
	c.Text(253, 33.5, "B REVERB", gfx.Small, uText, gfx.Left|gfx.Mid)
	for t := 0; t < 16; t++ {
		x := 6 + float64(t)*19.3
		col := tcol(t % 8)
		sel := t == 10
		bg := uint32(uPanel)
		if sel {
			bg = uPanel2
		}
		c.RoundRect(x, 44, 17, 146, 5, bg, 1)
		if sel {
			c.RoundRectLine(x, 44, 17, 146, 5, 1.2, col, 1)
		}
		c.Circle(x+8.5, 53, 6, col, 1)
		c.Text(x+8.5, 53.5, fmt.Sprint(t+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		if t >= 8 {
			c.Text(x+8.5, 66, string(rune('A'+t-8)), gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
		lv := 0.4 + 0.5*math.Abs(math.Sin(float64(t)*1.7))
		c.RoundRect(x+7, 74, 3, 80, 1.5, uBG, 1)
		c.RoundRect(x+7, 154-80*lv, 3, 80*lv, 1.5, col, 1)
		c.RoundRect(x+3, 152-80*lv, 11, 4, 2, uText, 1)
		for i := 0; i < 2; i++ {
			sv := math.Abs(math.Sin(float64(t*(i+2)) * 0.9))
			c.RoundRect(x+3, 162+float64(i)*5, 11, 2.5, 1.2, uBG, 1)
			c.RoundRect(x+3, 162+float64(i)*5, 11*sv, 2.5, 1.2, []uint32{knobCols[1], knobCols[2]}[i], 0.9)
		}
		state := "ON"
		sc := col
		if t == 2 || t == 13 {
			state, sc = "M", uRed
		}
		c.Text(x+8.5, 182, state, gfx.Small, sc, gfx.Center|gfx.Mid)
	}
	mockKnobs(c, [3]string{"LEVEL", "SEND A", "SEND B"}, [3]string{"72", "35", "40"}, [3]float64{0.72, 0.35, 0.4}, -1)
	a.uiBottom(c, "PADS 1-16: THE TRACK  ·  KNOB FX AGAIN: RACK  ·  SHIFT + K1: PAN")
}

// knob pick-up: the bar is the value; the hollow tick is where the knob is
func r7Pickup(a *app, c *gfx.Canvas) {
	c.Text(10, 46, "C03", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(64, 47, "SNARE 2", gfx.Small, uDim, gfx.Left|gfx.Mid)
	segTabs(c, 310, 46, pageNames, 2, uText)
	glass(c, 8, 58, 304, 116, 10, 0x9AA0A6)
	c.Text(160, 100, "-5 ST", gfx.Title, uText, gfx.Center|gfx.Mid)
	c.Text(160, 122, "K1 IS AT +14  ·  TURN DOWN TO -5 TO PICK IT UP", gfx.Small, uDim, gfx.Center|gfx.Mid)
	const y = 196.0
	labels := []string{"PITCH", "FINE", "LEVEL"}
	vals := []string{"-5", "+12", "53"}
	norms := []float64{0.4, 0.56, 0.53}
	knob := []float64{0.79, 0.56, 0.2}
	for k := 0; k < 3; k++ {
		bx, bw := 10+float64(k)*101, 96.0
		lc := uint32(uDim)
		if k == 0 {
			lc = knobCols[0]
		}
		c.Text(bx, y, labels[k], gfx.Small, lc, gfx.Left|gfx.Mid)
		c.Text(bx+bw, y, vals[k], gfx.Small, uText, gfx.Right|gfx.Mid)
		c.RoundRect(bx, y+6, bw, 4, 2, uPanel2, 1)
		c.RoundRect(bx, y+6, bw*norms[k], 4, 2, knobCols[k], 0.8)
		picked := math.Abs(knob[k]-norms[k]) < 0.02
		if !picked {
			kx := bx + bw*knob[k]
			c.RoundRectLine(kx-2.5, y+3, 5, 10, 2, 1, uText, 0.9)
			c.Line(bx+bw*norms[k], y+8, kx, y+8, 1, uText, 0.25)
		}
	}
	a.uiBottom(c, "A KNOB PICKS THE VALUE UP WHEN IT PASSES IT, THEN MOVES IT ALL THE WAY")
}

// COLLAGE: tiny shapes, gravity from the bands, one band per shape
func r7Collage(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "COLLAGE", gfx.Big, uText, gfx.Left|gfx.Mid)
	names := []string{"TRI", "SQR", "CIRC", "DIAM", "DOT", "DASH"}
	for i, n := range names {
		c.Text(130+float64(i)*30, 34, n, gfx.Small, map[bool]uint32{true: uText, false: uDim}[i == 2], gfx.Left|gfx.Mid)
	}
	// paper box
	c.RoundRect(8, 44, 304, 146, 6, 0xEFE9DF, 0.95)
	for y := 48.0; y < 188; y += 5 { // a faint halftone
		for x := 12.0; x < 310; x += 5 {
			v := 0.5 + 0.5*math.Sin(x/31+y/19)
			c.Circle(x, y, 0.3+0.5*v, 0x1A1A1A, 0.12)
		}
	}
	seed := uint32(7)
	rnd := func() float64 { seed = seed*1664525 + 1013904223; return float64(seed>>8) / float64(1<<24) }
	bands := []float64{0.9, 0.6, 0.4, 0.75, 0.3, 0.55}
	for i := 0; i < 60; i++ {
		kind := i % 6
		x := 16 + rnd()*288
		ground := 184.0
		y := ground - rnd()*130*bands[kind]
		s := 3 + rnd()*3
		col := uint32(0x161616)
		if kind%2 == 0 {
			col = r7Orange
		}
		switch kind {
		case 0:
			c.Poly([]float64{x, x + s, x - s}, []float64{y - s, y + s, y + s}, col, 1)
		case 1:
			c.Rect(x-s, y-s, 2*s, 2*s, col, 1)
		case 2:
			c.Circle(x, y, s, col, 1)
		case 3:
			c.Poly([]float64{x, x + s, x, x - s}, []float64{y - s, y, y + s, y}, col, 1)
		case 4:
			c.Circle(x, y, 1.4, col, 1)
		case 5:
			c.Line(x-s, y, x+s, y, 1.6, col, 1)
		}
	}
	mockKnobs(c, [3]string{"GRAVITY", "BOUNCE", "COUNT"}, [3]string{"60", "40", "60"}, [3]float64{0.6, 0.4, 0.6}, -1)
	a.uiBottom(c, "LOWS PUSH THE TRIANGLES, HIGHS THE DASHES  ·  16 LEVELS: NEXT")
}

// MELT: the knobs reshape it: K1 how they flow together, K2 how many and how big, K3 the texture
func r7Melt(a *app, c *gfx.Canvas, k1, k2, k3 float64, title string) {
	c.Text(10, 33, "MELT", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(310, 33, title, gfx.Small, uDim, gfx.Right|gfx.Mid)
	n := 3 + int(k2*5)
	type b struct{ x, y, r float64 }
	var bs []b
	for i := 0; i < n; i++ {
		ang := float64(i) / float64(n) * 2 * math.Pi
		rr := 30 + 40*(1-k1)
		bs = append(bs, b{160 + rr*math.Cos(ang)*1.3, 118 + rr*0.7*math.Sin(ang), 12 + 10*k2})
	}
	thr := 0.4 + 0.8*(1-k1)
	for y := 46.0; y < 188; y += 2 {
		for x := 4.0; x < 316; x += 2 {
			f := 0.0
			for _, q := range bs {
				dx, dy := x-q.x, y-q.y
				f += q.r * q.r / (dx*dx + dy*dy + 1)
			}
			if f < thr {
				continue
			}
			// K3: the texture inside: dots -> lines -> checks
			show := true
			switch {
			case k3 < 0.33:
				show = int(x/2+y/2)%2 == 0 || f > thr*2.5
			case k3 < 0.66:
				show = int(y/2)%3 != 0
			default:
				show = (int(x/6)+int(y/6))%2 == 0
			}
			if !show {
				continue
			}
			col := uint32(r7Orange)
			if f > thr*2.2 {
				col = 0x161616
			}
			c.Rect(x, y, 2, 2, gfx.Mix(col, cvCol(x), 0.25), 0.9)
		}
	}
	mockKnobs(c, [3]string{"CUTOFF", "RESONANCE", "DRIVE"}, [3]string{fmt.Sprint(int(k1 * 99)), fmt.Sprint(int(k2 * 99)), fmt.Sprint(int(k3 * 99))}, [3]float64{k1, k2, k3}, 0)
	a.uiBottom(c, "THE PAGE'S K1-K3 SHAPE IT: K1 FLOW  ·  K2 HOW MANY  ·  K3 TEXTURE")
}

func TestMockR7(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 MUTE track mutes", func(c *gfx.Canvas) { r7Mutes(a, c, 0) }},
		{"2 MUTE pad mutes", func(c *gfx.Canvas) { r7Mutes(a, c, 1) }},
		{"3 MUTE solo", func(c *gfx.Canvas) { r7Mutes(a, c, 2) }},
		{"4 TRACKS 16", func(c *gfx.Canvas) { r7Tracks(a, c) }},
		{"5 MIXER 16", func(c *gfx.Canvas) { r7Mixer16(a, c) }},
		{"6 KNOB PICKUP", func(c *gfx.Canvas) { r7Pickup(a, c) }},
		{"7 COLLAGE", func(c *gfx.Canvas) { r7Collage(a, c) }},
		{"8 MELT low", func(c *gfx.Canvas) { r7Melt(a, c, 0.2, 0.3, 0.1, "K1 LOW · K3 DOTS") }},
		{"9 MELT high", func(c *gfx.Canvas) { r7Melt(a, c, 0.85, 0.8, 0.8, "K1 HIGH · K3 CHECKS") }},
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
