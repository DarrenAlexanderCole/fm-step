package main

// Design round 4 (after the 0.9.25 test). FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR4

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

// lock: a small padlock
func lockIcon(c *gfx.Canvas, x, y float64, col uint32, closed bool) {
	c.RoundRect(x-3.5, y-1, 7, 6, 1.5, col, 1)
	if closed {
		c.Arc(x, y-1, 2.6, 1.2, math.Pi, 2*math.Pi, col, 1)
	} else {
		c.Arc(x+2, y-1.5, 2.6, 1.2, math.Pi, 1.8*math.Pi, col, 1)
	}
}

func r4Utility(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "SHIFT + PAD", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(150, 34, "UTILITIES", gfx.Small, uDim, gfx.Left|gfx.Mid)
	acts := []string{"CLEAR AUTO\nTRACK", "CLEAR AUTO\nPATTERN", "CLEAR\nPATTERN", "UNDO",
		"NEW\nPROJECT", "SAVE", "SAVE AS", "EXPORT\nWAV",
		"COPY\nPATTERN", "PASTE\nPATTERN", "", "",
		"", "", "", "TEMPO\nTAP"}
	for i := 0; i < 16; i++ {
		x, y, w, h := padTile(i)
		l := acts[i]
		col := []uint32{gCyan, gCyan, gCyan, uLime, pYellow, pYellow, pYellow, pYellow, pLilac, pLilac, uFaint, uFaint, uFaint, uFaint, uFaint, gPink}[i]
		if l == "" {
			c.RoundRectLine(x, y, w, h, 7, 0.8, uFaint, 0.6)
			c.Text(x+w-6, y+9, fmt.Sprintf("%d", i+1), gfx.Small, uFaint, gfx.Right|gfx.Mid)
			continue
		}
		glass(c, x, y, w, h, 7, col)
		c.Text(x+w-6, y+9, fmt.Sprintf("%d", i+1), gfx.Small, col, gfx.Right|gfx.Mid)
		l1, l2 := l, ""
		for j := range l {
			if l[j] == '\n' {
				l1, l2 = l[:j], l[j+1:]
			}
		}
		c.Text(x+6, y+h-19, l1, gfx.Small, uText, gfx.Left|gfx.Mid)
		c.Text(x+6, y+h-9, l2, gfx.Small, uText, gfx.Left|gfx.Mid)
	}
	a.uiBottom(c, "HOLD SHIFT + PAD  ·  BIG ONES (NEW, CLEAR) ASK TWICE")
}

func r4Trim(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"IN  LOCKED", "PLAY", "OUT"}, 2)
	w := c.Text(10, 57, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(10+w+7, 58.5, "TAKE 015  ·  2:32", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pill(c, 312, 57, "LOOP 1 BAR", knobCols[1], true)
	wave := breakWave(96000, 9, 2)
	glass(c, 8, 68, 304, 20, 6, pBlue)
	lineWave(c, 12, 308, 78, 7, wave, 0xE8E4D8, 0.6)
	in, out := 0.20, 0.70
	c.Rect(12, 70, 296*in, 16, uBG, 0.55)
	c.Rect(12+296*out, 70, 296*(1-out), 16, uBG, 0.55)
	c.RoundRectLine(12+296*0.38, 69, 296*0.22, 18, 3, 1, uText, 0.6)
	c.RoundRect(12+296*0.42, 69, 296*0.06, 3, 1.5, knobCols[1], 1) // the loop, inside IN-OUT
	glass(c, 8, 94, 304, 82, 10, gCyan)
	z := wave[int(0.38*96000):int(0.60*96000)]
	lineWave(c, 12, 308, 135, 34, z, 0xE8E4D8, 0.9)
	lx0, lx1 := 12+296*(0.42-0.38)/0.22, 12+296*(0.48-0.38)/0.22
	c.RoundRect(lx0, 96, lx1-lx0, 78, 3, knobCols[1], 0.10)
	c.Line(lx0, 96, lx0, 174, 1, knobCols[1], 0.8)
	c.Line(lx1, 96, lx1, 174, 1, knobCols[1], 0.8)
	c.RoundRect(lx0, 97, lx1-lx0, 3, 1.5, knobCols[1], 1)
	c.Text((lx0+lx1)/2, 106, "LOOP", gfx.Small, knobCols[1], gfx.Center|gfx.Mid)
	hx := lx0 + (lx1-lx0)*0.4
	c.Line(hx, 98, hx, 174, 1.2, uLime, 0.95)
	c.Text(16, 166, "IN 0:30.4  ·  OUT 1:46.9", gfx.Small, uDim, gfx.Left|gfx.Mid)
	lockIcon(c, 54-30, 32, uBG, true)
	mockKnobs(c, [3]string{"HEIGHT", "LOOP", "ZOOM"}, [3]string{"x2.0", "1 BAR", "x9"}, [3]float64{0.3, 0.55, 0.6}, 1)
	a.uiBottom(c, "SPIN FAST: FAR  ·  SHIFT + DATA: SCROLL  ·  IN AGAIN: UNLOCK")
}

func r4Comp(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"MONITOR ON", "PAUSE", "COMP + FX"}, 2)
	w := c.Text(10, 57, "COMP + FX", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(10+w+7, 58.5, "ON THE TAKE", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pill(c, 312, 57, "REC  1:12", uRed, true)
	// transfer curve
	glass(c, 8, 68, 150, 110, 10, gPink)
	x0, y0, s := 20.0, 168.0, 90.0
	c.Line(x0, y0, x0+s, y0-s, 0.6, uFaint, 1)
	var xs, ys []float64
	for i := 0; i <= 60; i++ {
		in := float64(i) / 60
		thr, ratio := 0.55, 4.0
		o := in
		if in > thr {
			o = thr + (in-thr)/ratio
		}
		xs, ys = append(xs, x0+in*s), append(ys, y0-o*s*1.25)
	}
	c.Polyline(xs, ys, 1.6, gPink, 1)
	c.Circle(x0+0.55*s, y0-0.55*s*1.25, 3, gPink, 1)
	c.Text(24, 80, "PUNCH  4:1", gfx.Small, uDim, gfx.Left|gfx.Mid)
	// gain reduction meter
	glass(c, 164, 68, 148, 50, 10, gPink)
	c.Text(172, 80, "GAIN REDUCTION", gfx.Small, uDim, gfx.Left|gfx.Mid)
	for i := 0; i < 20; i++ {
		on := i < 7
		col := uint32(uPanel2)
		if on {
			col = gPink
		}
		c.RoundRect(172+float64(i)*6.6, 92, 5, 14, 2, col, 1)
	}
	c.Text(304, 99, "-4.5", gfx.Label, uText, gfx.Right|gfx.Mid)
	// send to the rack
	glass(c, 164, 124, 148, 54, 10, knobCols[2])
	c.Text(172, 136, "SEND TO RACK A", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(172, 154, "DUB DELAY", gfx.Label, uText, gfx.Left|gfx.Mid)
	c.RoundRect(172, 166, 132, 4, 2, uPanel2, 1)
	c.RoundRect(172, 166, 132*0.3, 4, 2, knobCols[2], 1)
	mockKnobs(c, [3]string{"COMP", "AMOUNT", "SEND A"}, [3]string{"PUNCH", "62", "30"}, [3]float64{0.66, 0.62, 0.3}, 0)
	a.uiBottom(c, "RIGHT: BACK TO THE TAPE  ·  WHAT YOU HEAR WITH MONITOR ON")
}

func r4Shaper(a *app, c *gfx.Canvas, prompt bool) {
	if !prompt {
		c.Text(10, 33, "SHAPER", gfx.Big, uText, gfx.Left|gfx.Mid)
		c.Text(96, 34, "PAD 05  ·  EMPTY", gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
	glass(c, 8, 44, 304, 128, 10, gCyan)
	t := 0.7
	for gy := 52.0; gy < 168; gy += 6 {
		for gx := 14.0; gx < 308; gx += 6 {
			d := math.Abs(gy-108) / 60
			p := 0.5 + 0.5*math.Sin(gx/31+t+d*3)
			c.Circle(gx, gy, 0.6+0.5*p*(1-d), cvCol(gx), 0.12+0.25*p*(1-d))
		}
	}
	if !prompt {
		c.Text(160, 102, "HIT A PAD", gfx.Title, uText, gfx.Center|gfx.Mid)
		c.Text(160, 122, "ITS SOUND SHOWS HERE, READY TO SHAPE", gfx.Small, uDim, gfx.Center|gfx.Mid)
		mockKnobs(c, [3]string{"ATTACK", "DECAY", "RELEASE"}, [3]string{"-", "-", "-"}, [3]float64{0, 0, 0}, -1)
		a.uiBottom(c, "ADSR · ONE SHOT / GATE / LOOP · TRIM · MOD  ·  ONCE A PAD HAS A SOUND")
		return
	}
	btnLegend(c, [3]string{"SAMPLE IN", "", "SAMPLE SELECT"}, -1)
	c.RoundRect(40, 76, 240, 70, 12, uBG, 0.9)
	c.RoundRectLine(40, 76, 240, 70, 12, 1, gCyan, 0.7)
	c.Text(160, 94, "PAD 05 IS EMPTY", gfx.Title, uText, gfx.Center|gfx.Mid)
	c.Text(160, 116, "LEFT: SAMPLE IN (INPUTS / USB-C)", gfx.Small, uDim, gfx.Center|gfx.Mid)
	c.Text(160, 130, "RIGHT: SAMPLE SELECT (FROM THE CARD)", gfx.Small, uDim, gfx.Center|gfx.Mid)
	a.uiBottom(c, "ANY OTHER PAD: CANCEL")
}

func r4Tracks16(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "TRACKS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 34, "16  ·  PADS SELECT", gfx.Small, uDim, gfx.Left|gfx.Mid)
	names := []string{"BOOM KICK", "BRUSH SNARE", "JAZZ HAT", "JAZZ RIDE", "DUB SUB", "JAZZ CHORDS", "STEPPA", "MODAL BELL",
		"REESE", "HOUSE STAB", "WOBBLE", "NOISE BED", "", "", "", ""}
	cols := []uint32{0xFF5440, 0xFF9A2E, 0xF2D35B, 0x8EE04A, 0x2E7BFF, 0xB15CFF, 0x20E8E0, 0xFF3FA4,
		0xFF7A7A, 0xFFC27A, 0xF7EE8A, 0xB8F08A, 0x7AA8FF, 0xD3A0FF, 0x8AF4EE, 0xFF9AD0}
	muted := map[int]bool{2: true, 10: true}
	for i := 0; i < 16; i++ {
		x, y, w, h := padTile(i)
		col := cols[i]
		if names[i] == "" {
			c.RoundRectLine(x, y, w, h, 7, 0.8, uFaint, 0.6)
			c.Text(x+6, y+9, fmt.Sprintf("%d", i+1), gfx.Small, uFaint, gfx.Left|gfx.Mid)
			continue
		}
		al := 1.0
		if muted[i] {
			al = 0.35
		}
		glass(c, x, y, w, h, 7, col)
		if i == 4 {
			c.RoundRectLine(x-1.5, y-1.5, w+3, h+3, 8, 1.4, uText, 0.9)
		}
		c.Circle(x+9, y+9, 5.5, col, al)
		c.Text(x+9, y+9.5, fmt.Sprintf("%d", i+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		c.Text(x+6, y+h-10, names[i], gfx.Small, uText, gfx.Left|gfx.Mid)
		if muted[i] {
			c.Text(x+w-5, y+9, "MUTE", gfx.Small, uRed, gfx.Right|gfx.Mid)
		} else {
			for k := 0; k < 6; k++ { // activity dots
				c.Circle(x+w-24+float64(k)*4, y+9, 1.2, col, 0.3+0.7*math.Abs(math.Sin(float64(i*3+k))))
			}
		}
	}
	a.uiBottom(c, "MUTE: MUTE / UNMUTE THE TRACK  ·  SHIFT + MUTE: ALL  ·  SHIFT + SEQ: STEP EDIT")
}

func r4Rack(a *app, c *gfx.Canvas) {
	a.mode = modeRack
	a.ui.rackSlot = 0
	a.drawRack(c)
	a.mode = modePlay
	// the effect list K1 picks from (6)
	list := []string{"DUB DELAY", "REVERB", "SPACE ECHO", "CHORUS", "PHASER", "CRUSH"}
	c.RoundRect(150, 44, 162, 104, 8, uBG, 0.95)
	c.RoundRectLine(150, 44, 162, 104, 8, 1, knobCols[0], 0.8)
	for i, l := range list {
		y := 54 + float64(i)*16
		if i == 4 {
			c.RoundRect(154, y-7, 154, 14, 7, knobCols[0], 0.9)
			c.Text(162, y, l, gfx.Small, uBG, gfx.Left|gfx.Mid)
			c.Text(304, y, "NEW", gfx.Small, uBG, gfx.Right|gfx.Mid)
		} else {
			c.Text(162, y, l, gfx.Small, uDim, gfx.Left|gfx.Mid)
			if i >= 3 {
				c.Text(304, y, "NEW", gfx.Small, uFaint, gfx.Right|gfx.Mid)
			}
		}
	}
}

func TestMockR4(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 MODEL top row", func(c *gfx.Canvas) {
			a.pageIdx = pageIndex("MODEL")
			a.preset.P["engine"] = 10
			a.touched("engine")
			a.drawParamPage(c)
		}},
		{"2 MODEL bottom row", func(c *gfx.Canvas) {
			a.preset.P["engine"] = 13
			a.touched("engine")
			a.drawParamPage(c)
		}},
		{"3 WARMTH turning", func(c *gfx.Canvas) {
			a.pageIdx = pageIndex("WARMTH")
			a.touched("chorus")
			a.lastKey = "chorus"
			a.drawParamPage(c)
		}},
		{"4 UTILITIES", func(c *gfx.Canvas) { r4Utility(a, c) }},
		{"5 MIXTAPE trim lock loop", func(c *gfx.Canvas) { r4Trim(a, c) }},
		{"6 MIXTAPE COMP FX", func(c *gfx.Canvas) { r4Comp(a, c) }},
		{"7 SHAPER empty", func(c *gfx.Canvas) { r4Shaper(a, c, false) }},
		{"8 SHAPER pad empty", func(c *gfx.Canvas) { r4Shaper(a, c, true) }},
		{"9 TRACKS 16", func(c *gfx.Canvas) { r4Tracks16(a, c) }},
		{"10 RACK effects", func(c *gfx.Canvas) { r4Rack(a, c) }},
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
