package main

// Design mockups for S&S (Sample & Slice), the MIXTAPE recorder, the
// 4-instrument picker and the mixer with level dots. Drawn with the app's
// own kit; nothing here is wired in yet.
// FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockSS

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"fmstep/internal/gfx"
)

// a believable drum-break waveform (signed, -1..1) sampled at n points over secs
func breakWave(n int, secs float64, seed float64) []float64 {
	w := make([]float64, n)
	for i := range w {
		t := float64(i) / float64(n) * secs
		beat := t * 94 / 60
		ph := math.Mod(beat, 1)
		amp := 0.12 + 0.8*math.Exp(-ph*5)
		if int(beat)%2 == 1 {
			amp = 0.12 + 0.6*math.Exp(-ph*7)
		}
		amp += 0.18 * math.Exp(-math.Mod(beat*2, 1)*12)
		osc := math.Sin(t*2*math.Pi*(60+40*math.Exp(-ph*6))) * 0.7
		noise := math.Sin(t*7919+seed) * math.Sin(t*3301) * 0.5
		w[i] = math.Max(-1, math.Min(1, amp*(osc+noise)))
	}
	return w
}

// envelope of a signed wave over columns
func waveCols(w []float64, cols int) []float64 {
	out := make([]float64, cols)
	per := float64(len(w)) / float64(cols)
	for c := 0; c < cols; c++ {
		a, b := int(float64(c)*per), int(float64(c+1)*per)
		pk := 0.0
		for i := a; i < b && i < len(w); i++ {
			pk = math.Max(pk, math.Abs(w[i]))
		}
		out[c] = pk
	}
	return out
}

func drawEnv(c *gfx.Canvas, x0, ym, h float64, env []float64, col uint32, al float64) {
	for i, v := range env {
		x := x0 + float64(i) + 0.5
		c.Line(x, ym-v*h, x, ym+v*h, 1, col, al)
	}
}

// a Logic-style marker flag (a tab on top of a line), labelled
func flag(c *gfx.Canvas, x, y0, y1 float64, col uint32, lab string) {
	c.Line(x, y0, x, y1, 1.2, col, 1)
	c.Poly([]float64{x, x + 8, x + 8, x}, []float64{y0 - 8, y0 - 6, y0 - 1, y0 + 1}, col, 1)
	if lab != "" {
		c.Text(x+1, y1+6, lab, gfx.Small, col, gfx.Left|gfx.Mid)
	}
}

// screen-button legend: what the three buttons above the screen do here

// ---------------------------------------------------------------- 1 MIX
// The mixer with level dots: each track a column of dots that rise and
// fall with its level, a peak dot that hangs, the type under it.
func mockMix(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "MIX", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"LEVEL", "PAN", "SENDS"}, 0, uText)
	types := []string{"ANALOG", "FM", "CHORDS", "DRUMS", "SAMPLE", "SAMPLE", "ANALOG", "SAMPLE"}
	lv := []float64{0.82, 0.55, 0.38, 0.9, 0.7, 0.44, 0.6, 0.25}
	pk := []float64{0.9, 0.66, 0.47, 0.96, 0.78, 0.6, 0.66, 0.4}
	fd := []float64{0.8, 0.62, 0.5, 0.75, 0.7, 0.55, 0.66, 0.5}
	const rows = 14
	for t := 0; t < 8; t++ {
		x := 22 + float64(t)*38.5
		col := tcol(t)
		sel := t == 4
		if sel {
			c.RoundRect(x-17, 44, 34, 140, 8, uPanel, 1)
		}
		// dot column: lit to the level, the peak hangs above
		for r := 0; r < rows; r++ {
			y := 150 - float64(r)*7.0
			f := float64(r+1) / rows
			switch {
			case f <= lv[t]:
				dc := col
				if f > 0.86 {
					dc = uOrange
				}
				c.Circle(x-5, y, 2.4, dc, 0.55+0.45*f)
				c.Circle(x+5, y, 2.4, dc, 0.55+0.45*f)
			case math.Abs(f-pk[t]) < 0.5/rows:
				c.Circle(x-5, y, 2.4, uText, 0.9)
				c.Circle(x+5, y, 2.4, uText, 0.9)
			default:
				c.Circle(x-5, y, 1.1, uPanel2, 1)
				c.Circle(x+5, y, 1.1, uPanel2, 1)
			}
		}
		// fader position: a small notch beside the column
		fy := 150 - fd[t]*(rows-1)*7.0
		c.Poly([]float64{x + 11, x + 15, x + 15}, []float64{fy, fy - 3, fy + 3}, uText, 0.8)
		c.Circle(x, 163, 6, col, 1)
		c.Text(x, 163, fmt.Sprintf("%d", t+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		c.Text(x, 177, types[t], gfx.Small, map[bool]uint32{true: uText, false: uDim}[sel], gfx.Center|gfx.Mid)
	}
	mockKnobs(c, [3]string{"LEVEL T5", "PAN", "DELAY SEND"}, [3]string{"70", "C", "18"}, [3]float64{0.7, 0.5, 0.18}, 0)
	a.uiBottom(c, "PADS 1-8: TRACK  ·  9-16: MUTE  ·  DOTS: LEVEL, WHITE: PEAK")
}

// ---------------------------------------------------------------- 2 TYPE
// What a track is: one of the 4 instruments, or SAMPLE. Poster blocks.
func mockType(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "TRACK 5 IS", gfx.Big, uText, gfx.Left|gfx.Mid)
	opts := []struct {
		name, what string
		col        uint32
	}{
		{"ANALOG", "BASS · LEADS · SUB", 0x2E5BD8},
		{"FM", "KEYS · BELLS · 96 DX7", 0x7FE0E8},
		{"CHORDS", "STABS · PADS · STRINGS", 0x2E5BD8},
		{"DRUMS", "KICK · SNARE · HAT", 0x7FE0E8},
	}
	for i, o := range opts {
		y := 50 + float64(i)*28
		c.Rect(14, y, 150, 22, o.col, 1)
		c.Text(22, y+11, o.name, gfx.Title, uBG, gfx.Left|gfx.Mid)
		c.Text(172, y+11, o.what, gfx.Small, uDim, gfx.Left|gfx.Mid)
		c.Circle(160, y+4, 4+float64(i%2)*3, pLilac, 1)
	}
	// SAMPLE: the big pink block, selected
	c.RoundRect(14, 164, 292, 24, 4, pLilac, 1)
	c.Text(24, 176, "SAMPLE", gfx.Title, uBG, gfx.Left|gfx.Mid)
	c.Text(298, 176, "16 PADS · RECORD · TRIM · SLICE", gfx.Small, 0x33284D, gfx.Right|gfx.Mid)
	c.Circle(290, 162, 6, uText, 1)
	dotAccent(c, 268, 54, 7, 4, uDim)
	dotAccent(c, 282, 120, 4, 6, uDim)
	a.uiBottom(c, "DATA: CHOOSE  ·  PUSH: SET  ·  ITS NOTES STAY")
}

// ---------------------------------------------------------------- 3 PADS
// A SAMPLE track's 16 pads. Each shows its sound; a pad with a sub-layer
// (slices) shows the layers stacked behind it and how many.
func mockPads(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "PADS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(64, 33, "T5 SAMPLE", gfx.Small, tcol(4), gfx.Left|gfx.Mid)
	// pool meter
	c.Text(310, 29, "POOL 5:46 LEFT", gfx.Small, uDim, gfx.Right|gfx.Mid)
	c.RoundRect(232, 35, 78, 3, 1.5, uPanel2, 1)
	c.RoundRect(232, 35, 78*0.28, 3, 1.5, uLime, 1)
	names := []string{"KICK", "SNARE", "BREAK", "HAT", "VOX 1", "VOX 2", "RHODES", "", "STAB", "SUB", "", "", "MIXTAPE", "", "", "CLAP"}
	slices := map[int]int{2: 12, 12: 16, 6: 4}
	const x0, y0, pw, ph = 12.0, 46.0, 72.0, 33.0
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		x, y := x0+float64(col)*(pw+4), y0+float64(row)*(ph+3)
		if names[i] == "" {
			c.RoundRectLine(x, y, pw, ph, 4, 1, uPanel2, 1)
			c.Text(x+pw/2, y+ph/2, "+", gfx.Body, uFaint, gfx.Center|gfx.Mid)
			continue
		}
		if n, ok := slices[i]; ok {
			// the sub-layer: layers stacked behind the pad (the isometric reference)
			for k := 2; k >= 1; k-- {
				c.RoundRectLine(x+float64(k)*2.5, y-float64(k)*2.5, pw, ph, 4, 1, smpCol(i), 0.35+0.2*float64(2-k))
			}
			c.RoundRect(x, y, pw, ph, 4, uPanel2, 1)
			c.RoundRect(x+pw-22, y+3, 19, 10, 5, smpCol(i), 1)
			c.Text(x+pw-12.5, y+8, fmt.Sprintf("%d", n), gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else {
			c.RoundRect(x, y, pw, ph, 4, uPanel2, 1)
		}
		sel := i == 2
		if sel {
			c.Glow(x+pw/2, y+ph/2, 34, knobCols[0], 0.35)
			c.RoundRectLine(x, y, pw, ph, 4, 1.4, uText, 1)
		}
		env := waveCols(breakWave(600, 0.5+float64(i%3)*0.4, float64(i)), int(pw-10))
		drawEnv(c, x+5, y+ph/2+4, 8, env, smpCol(i), 0.9)
		c.Text(x+5, y+8, names[i], gfx.Small, uText, gfx.Left|gfx.Mid)
	}
	mockKnobs(c, [3]string{"LEVEL", "PITCH", "MODE"}, [3]string{"84", "+0 ST", "ONE SHOT"}, [3]float64{0.84, 0.5, 0.33}, -1)
	a.uiBottom(c, "PADS: PLAY  ·  HOLD SHIFT + PAD: OPEN ITS SLICES  ·  SAMPLE REC: RECORD TO PAD")
}

// ---------------------------------------------------------------- 4 TRIM
// Resolve/Logic-style trimming: the overview strip shows where the zoomed
// view is; the view follows the marker you turn; slow = fine, fast = far.
func mockTrim(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"▶ FROM IN", "LOOP", "UP TO OUT ▶"}, 1)
	const secs = 4.0
	w := breakWave(48000, secs, 3)
	// overview strip: whole sample, the window it is zoomed on, the trim
	ov := waveCols(w, 296)
	c.RoundRect(10, 43, 300, 16, 3, uPanel, 1)
	inF, outF := 0.31, 0.47
	for i, v := range ov {
		x := 12 + float64(i)
		f := float64(i) / 296
		col := uint32(0x56632E)
		if f >= inF && f <= outF {
			col = uLime
		}
		c.Line(x, 51-v*6, x, 51+v*6, 1, col, 1)
	}
	wx0, wx1 := 12+296*0.27, 12+296*0.36
	c.RoundRectLine(wx0, 44, wx1-wx0, 14, 2, 1.2, uText, 1)
	// zoomed view around the IN point
	z0, z1 := int(0.27*48000), int(0.36*48000)
	seg := w[z0:z1]
	zx0, zx1, ym, h := 10.0, 310.0, 112.0, 42.0
	n := int(zx1 - zx0)
	// at this zoom the samples show as a line, like zooming in on a transient
	xs, ys := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		v := seg[i*len(seg)/n]
		xs[i], ys[i] = zx0+float64(i), ym-v*h
	}
	inX := zx0 + (zx1-zx0)*(inF-0.27)/0.09
	c.Rect(inX, 66, zx1-inX, 92, uLime, 0.06)
	c.Rect(zx0, 66, inX-zx0, 92, uBG, 0.5)
	c.Polyline(xs, ys, 1.1, uLime, 1)
	c.Line(zx0, ym, zx1, ym, 0.6, uDim, 0.5)
	// time ruler
	for k := 0; k <= 10; k++ {
		x := zx0 + (zx1-zx0)*float64(k)/10
		c.Line(x, 160, x, 164, 0.8, uDim, 0.8)
		if k%2 == 0 {
			c.Text(x, 170, fmt.Sprintf("%.2f", (0.27+0.009*float64(k))*secs), gfx.Small, uFaint, gfx.Center|gfx.Mid)
		}
	}
	flag(c, inX, 68, 158, knobCols[0], "")
	pill(c, inX+6, 76, "IN 1.244 S", knobCols[0], false)
	// zero-crossing snap marker
	c.Circle(inX, ym, 2.5, uText, 1)
	c.Text(310, 76, "x24", gfx.Value, uText, gfx.Right|gfx.Mid)
	c.Text(310, 88, "ZOOM", gfx.Small, uDim, gfx.Right|gfx.Mid)
	mockKnobs(c, [3]string{"IN", "OUT", "ZOOM"}, [3]string{"1.244 S", "1.880 S", "x24"}, [3]float64{0.31, 0.47, 0.6}, 0)
	a.uiBottom(c, "TURN SLOW: SAMPLE BY SAMPLE  ·  FAST: FAR  ·  DATA: SCROLL  ·  FADER: SCRUB")
}

// ---------------------------------------------------------------- 5 SLICE
// A pad's sub-layer: slice flags like Logic, numbered by the pad they land
// on; the selected slice shaded; the pads show the layer is open.
func mockSlices(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "BREAK", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(70, 33, "PAD 3 · SUB-LAYER", gfx.Small, smpCol(2), gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"HITS", "16", "8", "MAN"}, 0, uText)
	const secs = 2.55
	w := breakWave(48000, secs, 7)
	x0, x1, ym, h := 10.0, 310.0, 112.0, 40.0
	env := waveCols(w, int(x1-x0))
	marks := []float64{0, 0.06, 0.125, 0.19, 0.25, 0.32, 0.375, 0.44, 0.5, 0.56, 0.625, 0.69, 0.75, 0.81, 0.875, 0.94}
	sel := 4
	sx0, sx1 := x0+(x1-x0)*marks[sel], x0+(x1-x0)*marks[sel+1]
	c.Rect(sx0, 62, sx1-sx0, 100, uText, 0.08)
	for i, v := range env {
		x := x0 + float64(i) + 0.5
		f := float64(i) / float64(len(env))
		col := uint32(0x3A6EA8)
		if f >= marks[sel] && f < marks[sel+1] {
			col = 0x6FB2F0
		}
		c.Line(x, ym-v*h, x, ym+v*h, 1, col, 1)
	}
	for i, m := range marks {
		x := x0 + (x1-x0)*m
		col := uint32(pYellow)
		if i == sel {
			col = uText
		}
		c.Line(x, 64, x, 160, 1, col, 0.9)
		c.Poly([]float64{x, x + 7, x + 7, x}, []float64{56, 58, 63, 65}, col, 1)
		c.Text(x+1, 168, fmt.Sprintf("%d", i+1), gfx.Small, col, gfx.Left|gfx.Mid)
	}
	// the playhead inside the selected slice
	ph := sx0 + (sx1-sx0)*0.4
	c.Line(ph, 62, ph, 160, 1, uText, 0.8)
	diamond(c, ph, 62, 3.5, uText)
	c.Text(310, 182, "12 HITS FOUND", gfx.Small, uDim, gfx.Right|gfx.Mid)
	mockKnobs(c, [3]string{"SENSITIVITY", "SLICE 5 IN", "SLICE 5 OUT"}, [3]string{"72", "0.638 S", "0.816 S"}, [3]float64{0.72, 0.25, 0.32}, 0)
	a.uiBottom(c, "PADS: THE 16 SLICES  ·  K2/K3 TRIM THIS ONE  ·  SHIFT + PAD 3: BACK TO PADS")
}

// ---------------------------------------------------------------- 6 MIXTAPE
// The cassette recorder: the reels turn, tape moves from one to the other
// as the take grows; dot-matrix counter; what's left on the card.
func mockMixtape(a *app, c *gfx.Canvas, stopped bool) {
	c.Text(10, 33, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 33, "TAKE 007", gfx.Small, pLilac, gfx.Left|gfx.Mid)
	// the cassette shell
	const cx, cy = 160.0, 104.0
	c.RoundRect(cx-120, cy-52, 240, 104, 8, 0xE9E3D6, 1)
	c.RoundRect(cx-108, cy-44, 216, 30, 3, pLilac, 1) // the label
	c.Text(cx-100, cy-36, "FM-STEP  ·  JAM  ·  5 NOV", gfx.Small, 0x33284D, gfx.Left|gfx.Mid)
	c.Line(cx-100, cy-24, cx+100, cy-24, 0.8, 0x33284D, 0.5)
	c.Text(cx+100, cy-36, "A", gfx.Title, 0x33284D, gfx.Right|gfx.Mid)
	// window with the two reels
	c.RoundRect(cx-74, cy-10, 148, 40, 18, 0x2A2A28, 1)
	frac := 0.21 // how much of the 20 minutes is used
	for k, rx := range []float64{cx - 44, cx + 44} {
		tape := 9 + 13*(1-frac)
		if k == 1 {
			tape = 9 + 13*frac
		}
		c.Circle(rx, cy+10, tape, 0x3B2A1E, 1)
		c.Circle(rx, cy+10, 7, 0xE9E3D6, 1)
		for s := 0; s < 6; s++ { // the hub teeth, turned
			ang := float64(s)*math.Pi/3 + 0.5
			c.Line(rx+3*math.Cos(ang), cy+10+3*math.Sin(ang), rx+6.5*math.Cos(ang), cy+10+6.5*math.Sin(ang), 1.6, 0x2A2A28, 1)
		}
	}
	c.Rect(cx-44, cy+28, 88, 2, 0x3B2A1E, 1)
	// screws and the bottom bridge
	for _, p := range [][2]float64{{cx - 112, cy - 46}, {cx + 112, cy - 46}, {cx - 112, cy + 46}, {cx + 112, cy + 46}} {
		c.Circle(p[0], p[1], 2, 0xB8B0A0, 1)
	}
	c.Poly([]float64{cx - 50, cx + 50, cx + 42, cx - 42}, []float64{cy + 52, cy + 52, cy + 38, cy + 38}, 0xD6CEBE, 1)
	// counter and state
	c.Dots(12, 160, "04:12", dotGlyph, 3.2, uText, 0x26271F)
	if stopped {
		c.Text(114, 168, "STOPPED", gfx.Label, uDim, gfx.Left|gfx.Mid)
	} else {
		c.Glow(120, 166, 9, uRed, 0.6)
		c.Circle(120, 166, 4, uRed, 1)
		c.Text(130, 166, "REC", gfx.Label, uRed, gfx.Left|gfx.Mid)
	}
	c.Text(310, 160, "15:48 LEFT ON THE TAPE", gfx.Small, uDim, gfx.Right|gfx.Mid)
	c.Text(310, 172, "CARD: 11.2 GB FREE", gfx.Small, uFaint, gfx.Right|gfx.Mid)
	// input level
	for i := 0; i < 2; i++ {
		y := 182 + float64(i)*5
		c.RoundRect(12, y, 296, 3, 1.5, uPanel2, 1)
		c.RoundRect(12, y, 296*(0.62+0.05*float64(i)), 3, 1.5, 0x5FD414, 1)
	}
	if stopped {
		// the question on stop, answered with the screen buttons
		c.RoundRect(40, 60, 240, 96, 10, uBG, 0.92)
		c.RoundRectLine(40, 60, 240, 96, 10, 1, uFaint, 1)
		c.Text(160, 80, "KEEP TAKE 007?", gfx.Title, uText, gfx.Center|gfx.Mid)
		c.Text(160, 98, "4 MIN 12 S  ·  48 MB", gfx.Small, uDim, gfx.Center|gfx.Mid)
		for i, l := range []string{"DELETE", "LISTEN", "SAVE"} {
			x := 82 + float64(i)*78
			col, fg := uint32(uPanel2), uint32(uDim)
			if i == 2 {
				col, fg = uLime, uBG
			}
			c.RoundRect(x-32, 120, 64, 22, 11, col, 1)
			c.Text(x, 131, l, gfx.Label, fg, gfx.Center|gfx.Mid)
		}
		a.uiBottom(c, "SCREEN BUTTONS: DELETE · LISTEN · SAVE")
		return
	}
	a.uiBottom(c, "SAMPLE RECORD: STOP  ·  SAVED TO FMSTEP/MIXTAPE")
}

// ---------------------------------------------------------------- 7 CHUNK
// From a saved take to a pad: the whole take, a 15 s window you move with
// DATA and size in bars, then TRIM or SLICE onto a pad.
func mockChunk(a *app, c *gfx.Canvas) {
	c.Text(10, 33, "TAKE 007", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"TRIM", "SLICE"}, 1, uText)
	// whole take: 4:12
	full := breakWave(60000, 252, 11)
	env := waveCols(full, 296)
	for i, v := range env {
		x := 12 + float64(i)
		f := float64(i) / 296
		vv := v * (0.45 + 0.4*math.Sin(f*9)*math.Sin(f*3.1)*math.Sin(f*3.1) + 0.15*f)
		c.Line(x, 62-vv*12, x, 62+vv*12, 1, 0x56632E, 1)
	}
	wf0 := 0.52
	wf1 := wf0 + 15.0/252
	c.RoundRectLine(12+296*wf0-1, 46, 296*(wf1-wf0)+2, 32, 2, 1.4, uLime, 1)
	for k := 0; k <= 4; k++ {
		c.Text(12+296*float64(k)/4, 84, fmt.Sprintf("%d:%02d", 63*k/60, 63*k%60), gfx.Small, uFaint, gfx.Center|gfx.Mid)
	}
	// the window, big: 15 s = 6 bars at 94 BPM, bar lines
	rule(c, 10, 310, 96, "2:11 - 2:26  ·  6 BARS", uLime)
	w := breakWave(48000, 15, 13)
	e := waveCols(w, 296)
	twoLo, twoHi := uint32(knobCols[2]), uint32(uLime)
	for i, v := range e {
		x := 12 + float64(i)
		c.Line(x, 136-v*26, x, 136+v*26, 1, twoLo, 1)
		c.Line(x, 136-v*16, x, 136+v*16, 1, twoHi, 1)
	}
	for b := 0; b <= 6; b++ {
		x := 12 + 296*float64(b)/6
		c.Line(x, 106, x, 166, 0.8, uText, 0.25)
		c.Text(x+2, 172, fmt.Sprintf("%d", b+1), gfx.Small, uFaint, gfx.Left|gfx.Mid)
	}
	pill(c, 310, 180, "TO PAD 3 · T5", tcol(4), true)
	mockKnobs(c, [3]string{"LENGTH", "NUDGE", "ZOOM"}, [3]string{"6 BARS", "+0 BEAT", "x1"}, [3]float64{0.4, 0.5, 0.1}, 0)
	a.uiBottom(c, "DATA: MOVE  ·  FADER: SCRUB  ·  PAD: SEND HERE (THE REST IS LEFT ON THE TAPE)")
}

func TestMockSS(t *testing.T) {
	dir := os.Getenv("FMSTEP_MOCK")
	if dir == "" {
		t.Skip("set FMSTEP_MOCK")
	}
	a := newTestApp(t)
	a.proj.Track = 4
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 6
	shots := []struct {
		name string
		f    func(c *gfx.Canvas)
	}{
		{"1 MIX - level dots", func(c *gfx.Canvas) { mockMix(a, c) }},
		{"2 TRACK TYPE - 4 instruments or SAMPLE", func(c *gfx.Canvas) { mockType(a, c) }},
		{"3 PADS - a SAMPLE track", func(c *gfx.Canvas) { mockPads(a, c) }},
		{"4 TRIM - zoom follows the marker", func(c *gfx.Canvas) { mockTrim(a, c) }},
		{"5 SLICE - a pad's sub-layer", func(c *gfx.Canvas) { mockSlices(a, c) }},
		{"6 MIXTAPE - recording", func(c *gfx.Canvas) { mockMixtape(a, c, false) }},
		{"7 MIXTAPE - keep the take", func(c *gfx.Canvas) { mockMixtape(a, c, true) }},
		{"8 TAKE TO PAD - pick 15 s", func(c *gfx.Canvas) { mockChunk(a, c) }},
	}
	for _, s := range shots {
		c := a.scr.C
		c.Clear(uBG)
		a.uiHeader(c)
		s.f(c)
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				v := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
			}
		}
		f, _ := os.Create(filepath.Join(dir, s.name+".png"))
		png.Encode(f, img)
		f.Close()
	}
}
