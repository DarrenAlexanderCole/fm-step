package main

// Design round for 0.9.25 "PADS" (the sampler rebuilt around 16 pads), drawn
// with the app's own kit. About 30% of Darren's references: the clear acrylic
// case (glass panels with a bright edge and a soft inner light, layered),
// outlined pads with a hand-drawn double rim and small icons, a huge faint
// number behind, pink / cyan glows. Nothing here is wired in yet.
// FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockPads

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

const (
	gPink = 0xFF6FCF
	gCyan = 0x5FE8FF
)

// glass: an acrylic panel: dark see-through fill, a faint tint, a bright top
// edge and a hairline rim.
func glass(c *gfx.Canvas, x, y, w, h, r float64, tint uint32) {
	c.RoundRect(x, y, w, h, r, 0x121411, 0.78)
	c.RoundRect(x, y, w, h, r, tint, 0.06)
	c.RoundRectLine(x, y, w, h, r, 1, tint, 0.45)
	c.Line(x+r, y+1.2, x+w-r, y+1.2, 1, 0xFFFFFF, 0.16) // the light on the top edge
	c.Line(x+1.2, y+r, x+1.2, y+h*0.45, 1, 0xFFFFFF, 0.08)
}

// bigBack: a huge faint dot-matrix number behind everything (the "90").
func bigBack(c *gfx.Canvas, x, y float64, s string) {
	c.Dots(x, y, s, dotGlyph, 11, 0x2A2C26, 0)
}

// padRim: a pad outlined twice, slightly offset, like the drawn reference.
func padRim(c *gfx.Canvas, x, y, w, h float64, col uint32, al float64) {
	c.RoundRectLine(x, y, w, h, 6, 1.3, col, al)
	c.RoundRectLine(x+1.4, y+1.1, w-2.6, h-2.4, 5, 0.7, col, al*0.45)
}

// fine wave inside a box (light, thin, scaled to its own peak)
func fineEnv(c *gfx.Canvas, x0, x1, ym, h float64, env []float64, col uint32, al float64) {
	pk := 1e-6
	for _, v := range env {
		pk = math.Max(pk, v)
	}
	n := int(x1 - x0)
	for k := 0; k < n; k++ {
		i0, i1 := k*len(env)/n, max(k*len(env)/n+1, (k+1)*len(env)/n)
		v := 0.0
		for i := i0; i < i1 && i < len(env); i++ {
			v = math.Max(v, env[i])
		}
		v /= pk
		x := x0 + float64(k) + 0.5
		c.Line(x, ym-v*h, x, ym+v*h, 0.8, col, al)
	}
}

type mpad struct {
	name   string
	env    []float64
	slices int
	icon   int // 0 none, 1 drum, 2 mic, 3 loop
	play   float64
}

func padIcon(c *gfx.Canvas, ic int, x, y float64, col uint32) {
	switch ic {
	case 1: // a drum: a ring and a hit
		c.Ring(x, y, 3.2, 1, col, 1)
		c.Circle(x, y, 1.2, col, 1)
	case 2: // the inputs: a jack
		c.RoundRect(x-1.2, y-4, 2.4, 6, 1, col, 1)
		c.Circle(x, y+3.2, 1.6, col, 1)
	case 3: // a loop
		c.Arc(x, y, 3.4, 1, 0.3, 5.6, col, 1)
		c.Poly([]float64{x + 2.6, x + 5, x + 4.8}, []float64{y - 3.6, y - 3.2, y - 0.8}, col, 1)
	}
}

// pads16: the 4x4 grid in [x0..] with glass, rims, previews.
func pads16(c *gfx.Canvas, x0, y0, pw, ph, gap float64, pads []mpad, sel int) {
	for i := 0; i < 16; i++ {
		col, row := i%4, 3-i/4
		x, y := x0+float64(col)*(pw+gap), y0+float64(row)*(ph+gap)
		p := pads[i]
		pc := smpCol(i)
		if p.env == nil { // empty: a dashed rim and a quiet plus
			for k := 0.0; k < pw-8; k += 6 {
				c.Line(x+4+k, y, x+4+k+3, y, 0.8, uFaint, 1)
				c.Line(x+4+k, y+ph, x+4+k+3, y+ph, 0.8, uFaint, 1)
			}
			for k := 0.0; k < ph-8; k += 6 {
				c.Line(x, y+4+k, x, y+4+k+3, 0.8, uFaint, 1)
				c.Line(x+pw, y+4+k, x+pw, y+4+k+3, 0.8, uFaint, 1)
			}
			c.Line(x+pw/2-3, y+ph/2, x+pw/2+3, y+ph/2, 1, uFaint, 1)
			c.Line(x+pw/2, y+ph/2-3, x+pw/2, y+ph/2+3, 1, uFaint, 1)
			if i == sel {
				padRim(c, x, y, pw, ph, gCyan, 0.9)
			}
			continue
		}
		c.RoundRect(x, y, pw, ph, 6, 0x121411, 0.85)
		if p.play > 0 {
			c.Glow(x+pw/2, y+ph/2, pw*0.8, pc, 0.45*p.play)
			c.RoundRect(x, y, pw, ph, 6, pc, 0.22*p.play)
		}
		padRim(c, x, y, pw, ph, pc, 0.85)
		if i == sel {
			c.RoundRectLine(x-2, y-2, pw+4, ph+4, 7, 1, uText, 0.8)
		}
		c.Text(x+4, y+6, fmt.Sprintf("%02d", i+1), gfx.Small, pc, gfx.Left|gfx.Mid)
		padIcon(c, p.icon, x+pw-7, y+7, pc)
		fineEnv(c, x+4, x+pw-4, y+ph*0.62, ph*0.24, p.env, 0xE8E4D8, 0.85)
		if p.slices > 0 { // contained slices: ticks under the wave and the count
			for k := 1; k < p.slices; k++ {
				sx := x + 4 + (pw-8)*float64(k)/float64(p.slices)
				c.Line(sx, y+ph-6, sx, y+ph-3, 0.8, pc, 0.9)
			}
		}
	}
}

func demoPads() []mpad {
	ps := make([]mpad, 16)
	ps[0] = mpad{name: "JU VERLINE", env: waveCols(breakWave(9600, 3, 1), 120), slices: 8, icon: 3, play: 1}
	ps[1] = mpad{name: "KICK 07", env: waveCols(breakWave(2400, 0.4, 2), 40), icon: 1}
	ps[2] = mpad{name: "SNARE", env: waveCols(breakWave(2400, 0.3, 4), 40), icon: 1}
	ps[4] = mpad{name: "TAKE 007", env: waveCols(breakWave(9600, 6, 5), 120), slices: 12, icon: 3}
	ps[5] = mpad{name: "VOX IN", env: waveCols(breakWave(4800, 1.2, 7), 60), icon: 2}
	ps[8] = mpad{name: "HAT", env: waveCols(breakWave(2400, 0.2, 9), 30), icon: 1}
	return ps
}

// ---------------------------------------------------------------- A: empty pads

func mockPadsEmpty(a *app, c *gfx.Canvas) {
	bigBack(c, 22, 70, "16")
	c.Text(10, 33, "PADS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(64, 34, "T8  ·  EMPTY", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pads16(c, 10, 46, 40, 30, 4, make([]mpad, 16), 0)
	glass(c, 196, 46, 116, 132, 10, gCyan)
	c.Text(254, 72, "16 EMPTY PADS", gfx.Label, uText, gfx.Center|gfx.Mid)
	c.Text(254, 92, "HIT A PAD", gfx.Small, uDim, gfx.Center|gfx.Mid)
	c.Text(254, 103, "TO FILL IT", gfx.Small, uDim, gfx.Center|gfx.Mid)
	for i, l := range []string{"LIBRARY", "INPUTS  1/4\"", "USB-C"} {
		y := 124 + float64(i)*20
		c.RoundRect(208, y, 92, 15, 7.5, gCyan, 0.08)
		c.RoundRectLine(208, y, 92, 15, 7.5, 0.8, gCyan, 0.35)
		c.Text(254, y+7.5, l, gfx.Small, uDim, gfx.Center|gfx.Mid)
	}
	a.uiBottom(c, "PADS: FILL  ·  PAD FX: BACK  ·  SEQ REC: RECORD HITS")
}

// ---------------------------------------------------------------- B: fill pad 1

func mockPadsFill(a *app, c *gfx.Canvas) {
	btnLegend(c, [3]string{"LIBRARY", "INPUTS", "USB-C"}, -1)
	bigBack(c, 14, 92, "01")
	c.Text(10, 56, "FILL PAD 01", gfx.Big, uText, gfx.Left|gfx.Mid)
	pads16(c, 10, 70, 22, 18, 3, make([]mpad, 16), 0)
	// three glass cards, one per screen button
	cards := []struct {
		t, s string
		ic  int
		col uint32
	}{{"LIBRARY", "WAV · TAKE · KIT", 3, pLilac}, {"INPUTS", "1/4\" JACKS", 2, gPink}, {"USB-C", "COMPUTER", 0, gCyan}}
	for i, cd := range cards {
		x := 118 + float64(i)*66
		glass(c, x, 66, 60, 110, 9, cd.col)
		c.Glow(x+30, 96, 22, cd.col, 0.18)
		switch i {
		case 0:
			c.RoundRect(x+20, 88, 20, 14, 2, cd.col, 0.9)
			c.RoundRect(x+20, 85, 9, 4, 1.5, cd.col, 0.9)
		case 1:
			padIcon(c, 2, x+30, 95, cd.col)
		case 2:
			c.RoundRectLine(x+22, 90, 16, 10, 5, 1.4, cd.col, 1)
			c.Line(x+26, 95, x+34, 95, 1.4, cd.col, 1)
		}
		c.Text(x+30, 122, cd.t, gfx.Label, uText, gfx.Center|gfx.Mid)
		c.Text(x+30, 138, cd.s, gfx.Small, uDim, gfx.Center|gfx.Mid)
		if i == 1 { // a live input meter on the inputs card
			for k := 0; k < 2; k++ {
				c.RoundRect(x+8, 152+float64(k)*6, 44, 3, 1.5, uPanel2, 1)
				c.RoundRect(x+8, 152+float64(k)*6, 44*[]float64{0.62, 0.48}[k], 3, 1.5, gPink, 0.9)
			}
		}
	}
	c.Text(10, 168, "THE WHOLE SOUND", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(10, 179, "LANDS ON PAD 01", gfx.Small, uDim, gfx.Left|gfx.Mid)
	a.uiBottom(c, "SCREEN BUTTONS: WHERE FROM  ·  ANY OTHER PAD: CANCEL")
}

// ---------------------------------------------------------------- C: pads loaded

func mockPadsLoaded(a *app, c *gfx.Canvas) {
	ps := demoPads()
	bigBack(c, 22, 70, "01")
	c.Text(10, 33, "PADS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(64, 34, "T8  ·  6 OF 16", gfx.Small, uDim, gfx.Left|gfx.Mid)
	pads16(c, 10, 46, 40, 30, 4, ps, 0)
	// the selected pad: its reel of slices, on glass
	glass(c, 196, 46, 116, 132, 10, pLilac)
	const cx, cy = 254.0, 104.0
	ang0 := float64(time.Now().UnixMilli()%1) + 0.6
	for i := 0; i < 8; i++ {
		ang := -math.Pi/2 + (float64(i)+0.5)*2*math.Pi/8 + ang0
		r := 7 + 6*math.Abs(math.Sin(float64(i)*1.7))
		x, y := cx+30*math.Cos(ang), cy+30*math.Sin(ang)
		col := []uint32{pBlue, pLilac, gPink, pCream}[i%4]
		if i == 2 {
			c.Glow(x, y, r+8, col, 0.5)
			c.Circle(x, y, r, col, 0.85)
			c.Text(x, y, "3", gfx.Label, uBG, gfx.Center|gfx.Mid)
		} else {
			c.Ring(x, y, r, 1, col, 0.85)
		}
	}
	c.Ring(cx, cy, 12, 1, uFaint, 1)
	c.Dots(cx-8.5, cy-5, "03", dotGlyph, 1.5, uLime, 0)
	c.Text(254, 58, "JU VERLINE", gfx.Label, uText, gfx.Center|gfx.Mid)
	c.Text(254, 148, "3.0 S  ·  8 SLICES", gfx.Small, uDim, gfx.Center|gfx.Mid)
	c.Text(254, 162, "PUSH  OPEN THE PAD", gfx.Small, pLilac, gfx.Center|gfx.Mid)
	mockKnobs(c, [3]string{"LEVEL", "PITCH", "TO TRACK"}, [3]string{"80", "0", "T8"}, [3]float64{0.8, 0.5, 1}, -1)
	a.uiBottom(c, "PADS PLAY  ·  PUSH: TRIM / SLICE  ·  ERASE + PAD: EMPTY IT")
}

// ---------------------------------------------------------------- D: trim inside a pad

func mockPadTrim(a *app, c *gfx.Canvas) {
	bigBack(c, 210, 92, "01")
	c.Text(10, 33, "PAD 01", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"TRIM", "SLICE", "ENV"}, 0, uText)
	w := breakWave(48000, 3, 1)
	// the whole sound, with a lens where the big view is
	glass(c, 8, 46, 304, 24, 6, pBlue)
	all := waveCols(w, 296)
	fineEnv(c, 12, 308, 58, 9, all, 0xE8E4D8, 0.55)
	in, out := 0.18, 0.74
	lx0, lx1 := 12+296*0.12, 12+296*0.30
	c.RoundRect(lx0, 48, lx1-lx0, 20, 3, gCyan, 0.12)
	c.RoundRectLine(lx0, 48, lx1-lx0, 20, 3, 1, gCyan, 0.8)
	c.Line(12+296*in, 47, 12+296*in, 69, 1.2, knobCols[0], 1)
	c.Line(12+296*out, 47, 12+296*out, 69, 1.2, knobCols[2], 1)
	// the zoomed view: real sample lines, the IN flag riding
	glass(c, 8, 76, 304, 96, 10, gCyan)
	zw := w[int(0.12*48000):int(0.30*48000)]
	n := 296
	prevY := 0.0
	for k := 0; k < n; k++ {
		v := zw[k*len(zw)/n]
		y := 124 - v*40
		if k > 0 {
			c.Line(12+float64(k-1)+0.5, prevY, 12+float64(k)+0.5, y, 1, 0xE8E4D8, 0.9)
		}
		prevY = y
	}
	c.Line(12, 124, 308, 124, 0.6, uFaint, 1)
	ix := 12 + 296*(in-0.12)/0.18
	c.Rect(12, 78, ix-12, 92, uBG, 0.55)
	c.Glow(ix, 124, 16, knobCols[0], 0.2)
	c.Line(ix, 90, ix, 168, 1.4, knobCols[0], 1)
	c.RoundRect(ix-28, 80, 56, 12, 6, knobCols[0], 1)
	c.Text(ix, 86, "IN 0.540 S", gfx.Small, uBG, gfx.Center|gfx.Mid)
	c.Text(306, 164, "x17", gfx.Small, uDim, gfx.Right|gfx.Mid)
	mockKnobs(c, [3]string{"IN", "OUT", "ZOOM"}, [3]string{"0.540 S", "2.220 S", "x17"}, [3]float64{in, out, 0.5}, 0)
	a.uiBottom(c, "DATA: ZOOM  ·  PUSH: FINE  ·  LEFT ▶ FROM IN  ·  RIGHT UP TO OUT ▶")
}

// ---------------------------------------------------------------- E: tap the slices in

func mockPadSlice(a *app, c *gfx.Canvas) {
	bigBack(c, 210, 92, "06")
	c.Text(10, 33, "PAD 01", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"TRIM", "SLICE", "ENV"}, 1, uText)
	w := breakWave(48000, 3, 1)
	glass(c, 8, 46, 304, 126, 10, gPink)
	env := waveCols(w, 296)
	head := 0.63
	for k := 0; k < 296; k++ {
		x := 12 + float64(k) + 0.5
		v := env[k]
		al := 0.9
		col := uint32(0xE8E4D8)
		if float64(k)/296 > head {
			al, col = 0.28, 0xB0AEA6
		}
		c.Line(x, 112-v*44, x, 112+v*44, 1, col, al)
	}
	cuts := []float64{0.02, 0.135, 0.255, 0.33, 0.45, 0.575}
	for i, f := range cuts {
		x := 12 + 296*f
		col := smpCol(i)
		c.Line(x, 56, x, 168, 1.1, col, 0.95)
		c.RoundRect(x-1, 52, 13, 11, 3, col, 1)
		c.Text(x+5.5, 57.5, fmt.Sprintf("%d", i+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
	}
	hx := 12 + 296*head
	c.Glow(hx, 112, 22, gPink, 0.25)
	c.Line(hx, 50, hx, 170, 1.4, gPink, 1)
	c.RoundRect(18, 150, 120, 16, 8, gPink, 0.18)
	c.Circle(29, 158, 3.2, gPink, 1)
	c.Text(37, 158, "TAPPING  ·  SLICE 7 NEXT", gfx.Small, uText, gfx.Left|gfx.Mid)
	mockKnobs(c, [3]string{"NUDGE", "SNAP", "SPEED"}, [3]string{"-4 MS", "HITS", "1/2"}, [3]float64{0.45, 0.66, 0.5}, -1)
	a.uiBottom(c, "TAP PAD 01 ON EACH HIT  ·  ERASE: LAST SLICE  ·  STOP: DONE")
}

// ---------------------------------------------------------------- F: a slice's envelope

func mockPadEnv(a *app, c *gfx.Canvas) {
	bigBack(c, 210, 92, "04")
	c.Text(10, 33, "SLICE 04", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 310, 33, []string{"TRIM", "SLICE", "ENV"}, 2, uText)
	// the slices as a strip of chips, 4 picked
	for i := 0; i < 8; i++ {
		x := 10 + float64(i)*38
		col := smpCol(i)
		if i == 3 {
			c.RoundRect(x, 46, 34, 13, 6.5, col, 0.9)
			c.Text(x+17, 52.5, "4", gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else {
			c.RoundRectLine(x, 46, 34, 13, 6.5, 0.8, col, 0.6)
			c.Text(x+17, 52.5, fmt.Sprintf("%d", i+1), gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
	}
	glass(c, 8, 64, 304, 108, 10, pLilac)
	// the slice under the envelope, then the envelope as layered glass
	env := waveCols(breakWave(12000, 0.6, 3), 280)
	x0, x1, base, top := 20.0, 300.0, 160.0, 76.0
	a1, d1, s1, r1 := 0.08, 0.22, 0.55, 0.85
	X := func(f float64) float64 { return x0 + (x1-x0)*f }
	Y := func(v float64) float64 { return base - (base-top)*v }
	xs := []float64{X(0), X(a1), X(d1), X(r1), X(1)}
	ys := []float64{Y(0), Y(1), Y(s1), Y(s1), Y(0)}
	for k := 0; k < 280; k++ {
		f := float64(k) / 280
		g := 0.0
		switch {
		case f < a1:
			g = f / a1
		case f < d1:
			g = 1 - (1-s1)*(f-a1)/(d1-a1)
		case f < r1:
			g = s1
		default:
			g = s1 * (1 - (f-r1)/(1-r1))
		}
		x := X(f)
		v := env[k] * 34
		c.Line(x, 118-v, x, 118+v, 1, 0xE8E4D8, 0.18)
		c.Line(x, 118-v*g, x, 118+v*g, 1, 0xE8E4D8, 0.75)
	}
	c.FillUnder(xs, ys, base, pLilac, 0.16)
	c.FillUnder(xs[:3], ys[:3], base, gCyan, 0.10)
	c.Polyline(xs, ys, 1.6, pLilac, 1)
	for i, h := range [][2]float64{{xs[1], ys[1]}, {xs[2], ys[2]}, {xs[3], ys[3]}} {
		c.Glow(h[0], h[1], 10, knobCols[i], 0.4)
		c.Circle(h[0], h[1], 4, knobCols[i], 1)
	}
	c.Text(X(a1), ys[1]-10, "A", gfx.Small, knobCols[0], gfx.Center|gfx.Mid)
	c.Text(X(d1)+8, ys[2]-9, "D · S", gfx.Small, knobCols[1], gfx.Left|gfx.Mid)
	c.Text(X(r1)+8, ys[3]-9, "R", gfx.Small, knobCols[2], gfx.Left|gfx.Mid)
	mockKnobs(c, [3]string{"ATTACK", "DECAY", "RELEASE"}, [3]string{"12 MS", "90 MS", "160 MS"}, [3]float64{0.2, 0.4, 0.6}, 1)
	a.uiBottom(c, "SHIFT + K2: SUSTAIN  ·  DATA: OTHER SLICE  ·  PADS PLAY THE SLICES")
}

func TestMockPads(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	a.proj.Track = 7
	a.preset = &a.proj.Tracks[7].Sound
	shots := []struct {
		name string
		f    func(*app, *gfx.Canvas)
	}{
		{"A PADS empty", mockPadsEmpty},
		{"B PADS fill pad 01", mockPadsFill},
		{"C PADS loaded", mockPadsLoaded},
		{"D PAD trim", mockPadTrim},
		{"E PAD slice tap-in", mockPadSlice},
		{"F SLICE envelope", mockPadEnv},
	}
	for _, s := range shots {
		c := a.scr.C
		c.Clear(uBG)
		a.statusAt = time.Time{}
		a.uiHeader(c)
		s.f(a, c)
		savePNG(a, filepath.Join(dir, s.name+".png"))
	}
}

func mockDir(t *testing.T) string {
	if d := getenvMock(); d != "" {
		return d
	}
	t.Skip("set FMSTEP_MOCK")
	return ""
}

func getenvMock() string { return os.Getenv("FMSTEP_MOCK") }
