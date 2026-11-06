package main

// Design round 3: shading / flow studies for the knob curve, switching the
// mixer's send between A and B, and the panels for the sampler infographic.
// FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockR3

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/gfx"
)

// curveY: the Catmull-Rom curve through the three knob levels (as curveLayer).
func curveY(lv [3]float64) func(x float64) float64 {
	const top, bot = 42.0, 136.0
	Y := func(n float64) float64 { return bot - (bot-top)*n }
	pts := []float64{Y(lv[0]), Y(lv[0]), Y(lv[1]), Y(lv[2]), Y(lv[2])}
	px := []float64{-28, 62, 160, 258, 348}
	return func(x float64) float64 {
		i := 0
		for i < len(px)-2 && x > px[i+1] {
			i++
		}
		t := (x - px[i]) / (px[i+1] - px[i])
		p0, p1, p2 := pts[max(0, i-1)], pts[i], pts[i+1]
		p3 := pts[min(len(pts)-1, i+2)]
		return math.Max(top, math.Min(bot, 0.5*(2*p1+(-p0+p2)*t+(2*p0-5*p1+4*p2-p3)*t*t+(-p0+3*p1-3*p2+p3)*t*t*t)))
	}
}

func curveCol(x float64) uint32 {
	cx := [3]float64{62, 160, 258}
	var w [3]float64
	sum := 0.0
	for k := 0; k < 3; k++ {
		d := (x - cx[k]) / 60
		w[k] = math.Exp(-d * d)
		sum += w[k]
	}
	return gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], w[1]/(w[0]+w[1])), knobCols[2], w[2]/sum)
}

// shade styles
const (
	shLayers = iota
	shDots
	shContour
	shJuice
)

func shadeStudy(a *app, c *gfx.Canvas, style int, name string, lv [3]float64, level float64) {
	c.Text(10, 31, "AMP", gfx.Big, uText, gfx.Left|gfx.Mid)
	pill(c, 312, 32, name, uLime, true)
	c.RoundRect(8, 40, 304, 98, 10, uPanel, 1)
	const top, bot, x0, x1 = 42.0, 136.0, 12.0, 308.0
	y := curveY(lv)
	switch style {
	case shLayers: // three translucent sheets, each a little lower and fainter: depth
		for l := 2; l >= 0; l-- {
			off := float64(l) * 9
			for x := x0; x < x1; x++ {
				yy := math.Min(bot, y(x+0.5)+off)
				c.Line(x+0.5, yy, x+0.5, bot-1, 1, curveCol(x), 0.12+0.06*float64(2-l))
			}
		}
	case shDots: // the panel's dot grid lights up under the curve, brightest at the edge
		for gy := top + 4; gy < bot; gy += 6 {
			for gx := x0 + 4; gx < x1; gx += 6 {
				d := gy - y(gx)
				if d < 0 {
					c.Circle(gx, gy, 0.6, uFaint, 0.6)
					continue
				}
				e := math.Max(0.25, 1-d/70)
				c.Circle(gx, gy, 0.8+1.1*e, curveCol(gx), 0.35+0.55*e)
			}
		}
	case shContour: // flow lines under the curve, fading down, drifting slowly
		for k := 0; k < 7; k++ {
			ph := float64(k)*11 + 4*math.Sin(float64(k)*1.3)
			var xs, ys []float64
			for x := x0; x < x1; x += 2 {
				yy := y(x) + ph + 2*math.Sin(x/23+float64(k))
				if yy < bot-2 {
					xs, ys = append(xs, x), append(ys, yy)
				}
			}
			for i := 1; i < len(xs); i++ {
				c.Line(xs[i-1], ys[i-1], xs[i], ys[i], 1, curveCol(xs[i]), 0.6*math.Pow(0.72, float64(k)))
			}
		}
	case shJuice: // the fill swells with the sound: a level-driven tide under the curve
		for x := x0; x < x1; x++ {
			yy := y(x + 0.5)
			tide := level * 10 * (0.6 + 0.4*math.Sin(x/17))
			c.Line(x+0.5, yy-tide*0.3, x+0.5, bot-1, 1, curveCol(x), 0.16)
			c.Line(x+0.5, yy+8, x+0.5, bot-1, 1, curveCol(x), 0.10+0.25*level)
		}
		for _, k := range []int{0, 1, 2} {
			cx := []float64{62, 160, 258}[k]
			c.Glow(cx, y(cx), 14+12*level, knobCols[k], 0.25+0.35*level)
		}
	}
	var xs, ys []float64
	for x := x0; x < x1; x++ {
		xs, ys = append(xs, x+0.5), append(ys, y(x+0.5))
	}
	for i := 1; i < len(xs); i++ {
		c.Line(xs[i-1], ys[i-1], xs[i], ys[i], 1.6, curveCol(xs[i]), 1)
	}
	for k := 0; k < 3; k++ {
		cx := []float64{62, 160, 258}[k]
		c.Circle(cx, y(cx), 3, knobCols[k], 1)
	}
	for k := 0; k < 3; k++ {
		pr := a.cur(k)
		uiKnob(c, 62+float64(k)*98, 169, 18.5, lv[k], -1, pr.label, pct(lv[k]), tcol(a.proj.Track), k == 1, false)
		c.RoundRect(62+float64(k)*98-7, 204, 14, 2.6, 1.3, knobCols[k], 0.8)
	}
	a.uiBottom(c, "SHADING STUDY  ·  "+name)
}

// the mixer with the send switch: tabs on top, each strip's send in that send's colour
func mixSend(a *app, c *gfx.Canvas, which int) {
	a.mode = modeTracks
	a.ui.mixer = true
	a.drawTracksV2(c)
	a.mode = modePlay
	c.RoundRect(150, 22, 166, 18, 4, uBG, 1) // (cover the header hint in the mock)
	names := []string{"A  DUB DELAY", "B  REVERB"}
	cols := []uint32{knobCols[2], pLilac}
	segTabs(c, 312, 32, names, which, cols[which])
	amts := [][]float64{{0, 0.5, 0, 0, 0.8, 0.1, 0, 0.3}, {0.2, 0.3, 0.1, 0.2, 0.4, 0.6, 0.2, 0.1}}
	for t := 0; t < 8; t++ {
		cx := 6 + float64(t)*38.5 + 17.5
		v := amts[which][t]
		x := cx - 17.5
		for i := 0; i < 2; i++ { // both sends as bars; the one K3 drives is thick and lit
			yy := 172 + float64(i)*5
			c.RoundRect(x+5, yy-2.5, 25, 5, 2.5, uPanel, 1)
			vv := amts[i][t]
			if i == which {
				c.RoundRect(x+6, yy-2, 23, 4, 2, uBG, 1)
				c.RoundRect(x+6, yy-2, math.Max(4, 23*vv), 4, 2, cols[i], 1)
			} else {
				c.RoundRect(x+6, yy-0.5, math.Max(1, 23*vv), 1, 0.5, cols[i], 0.35)
			}
		}
		_ = v
	}
	a.uiBottom(c, fmt.Sprintf("K3: SEND %s  ·  PUSH DATA: SWITCH A / B  ·  PAD FX: THE RACK", []string{"A", "B"}[which]))
}

func TestMockR3(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	a.pageIdx = pageIndex("AMP")
	lv := [3]float64{0.25, 0.55, 0.84}
	shots := []struct {
		name string
		f    func(*gfx.Canvas)
	}{
		{"1 SHADE layers", func(c *gfx.Canvas) { shadeStudy(a, c, shLayers, "LAYERS", lv, 0) }},
		{"2 SHADE dots", func(c *gfx.Canvas) { shadeStudy(a, c, shDots, "DOTS", lv, 0) }},
		{"3 SHADE contour", func(c *gfx.Canvas) { shadeStudy(a, c, shContour, "FLOW LINES", lv, 0) }},
		{"4 SHADE juice", func(c *gfx.Canvas) { shadeStudy(a, c, shJuice, "JUICE (LOUD)", lv, 0.9) }},
		{"5 MIXER send A", func(c *gfx.Canvas) { mixSend(a, c, 0) }},
		{"6 MIXER send B", func(c *gfx.Canvas) { mixSend(a, c, 1) }},
		{"7 MODEL", func(c *gfx.Canvas) {
			a.pageIdx = pageIndex("MODEL")
			a.preset.P["engine"] = 14
			a.touched("engine")
			a.drawParamPage(c)
			a.pageIdx = pageIndex("AMP")
		}},
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
