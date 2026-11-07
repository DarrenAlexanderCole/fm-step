package main

// TAPE FX (0.9.27): the take's own chain, never the mix you hear (unless
// MONITOR is on) and never the other tracks' busses. The recorder's right
// button opens it (SHIFT + pad 5 COMPRESSOR too).
//
//   DATA       pick the stage: COMP · EQ · DRIVE · WIDTH · SEND A
//   PUSH       that stage on / off
//   COMP       K1 THRESH · K2 RATIO · K3 MAKEUP  (SHIFT: K1 ATTACK · K3 RELEASE)
//   EQ         K1 LOW CUT · K2 MID · K3 AIR
//   DRIVE      K1 DRIVE · WIDTH K1 WIDTH · SEND A K1 the mix into RACK A
//
// The recording page's K3 COMP (OFF, GLUE, PUNCH, LIMIT) sets the COMP stage.

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
)

var mtStages = []string{"COMP", "EQ", "DRIVE", "WIDTH", "SEND A"}

func init() {
	for k, v := range map[string]float64{
		"mtfx_comp_on": 0, "mtfx_thresh": 0.65, "mtfx_ratio": 0.222, "mtfx_attack": 0.1, "mtfx_release": 0.133, "mtfx_makeup": 0.167,
		"mtfx_eq_on": 0, "mtfx_lowcut": 0.135, "mtfx_mid": 0.5, "mtfx_air": 0.5,
		"mtfx_drive_on": 0, "mtfx_drive": 0.3, "mtfx_width_on": 0, "mtfx_width": 0.5,
	} {
		charDefaults[k] = v
	}
}

// engine values from the knob positions
func mtThreshDb(n float64) float64 { return -40 + 40*n }
func mtRatio(n float64) float64    { return 1 + 19*n*n }
func mtAtkMs(n float64) float64    { return 0.1 + 99.9*n }
func mtRelMs(n float64) float64    { return 20 + 980*n }
func mtMakeup(n float64) float64   { return 18 * n }
func mtLowCut(n float64) float64   { return 20 * math.Pow(20, n) }
func mtEqDb(n float64) float64     { return -12 + 24*n }

func (a *app) mtFxSend() {
	if a.dsp == nil || a.char == nil {
		return
	}
	c := a.char
	vals := []float64{c["mtfx_comp_on"], mtThreshDb(c["mtfx_thresh"]), mtRatio(c["mtfx_ratio"]), mtAtkMs(c["mtfx_attack"]), mtRelMs(c["mtfx_release"]), mtMakeup(c["mtfx_makeup"]),
		c["mtfx_eq_on"], mtLowCut(c["mtfx_lowcut"]), mtEqDb(c["mtfx_mid"]), mtEqDb(c["mtfx_air"]),
		c["mtfx_drive_on"], c["mtfx_drive"], c["mtfx_width_on"], 2 * c["mtfx_width"]}
	var lines []string
	for i, v := range vals {
		lines = append(lines, fmt.Sprintf("MTFX %d %.4f", i, v))
	}
	a.dsp.lines(lines)
}

// mtCompPreset: the recording page's K3 COMP sets the COMP stage
func (a *app) mtCompPreset(p int) {
	c := a.char
	switch p {
	case 0:
		c["mtfx_comp_on"] = 0
		return
	case 1: // GLUE: -18 dB, 2:1, 10 ms, 300 ms, +3
		c["mtfx_thresh"], c["mtfx_ratio"], c["mtfx_attack"], c["mtfx_release"], c["mtfx_makeup"] = 0.55, 0.229, 0.099, 0.286, 0.167
	case 2: // PUNCH: -14 dB, 4:1, 25 ms, 150 ms, +5
		c["mtfx_thresh"], c["mtfx_ratio"], c["mtfx_attack"], c["mtfx_release"], c["mtfx_makeup"] = 0.65, 0.397, 0.25, 0.133, 0.278
	case 3: // LIMIT: -3 dB, 20:1, 1 ms, 100 ms, 0
		c["mtfx_thresh"], c["mtfx_ratio"], c["mtfx_attack"], c["mtfx_release"], c["mtfx_makeup"] = 0.925, 1, 0.009, 0.082, 0
	}
	c["mtfx_comp_on"] = 1
}

// mtFxParam: what knob k sets on the picked stage
func (a *app) mtFxParam(k int) string {
	t := [][3]string{
		{"mtfx_thresh", "mtfx_ratio", "mtfx_makeup"},
		{"mtfx_lowcut", "mtfx_mid", "mtfx_air"},
		{"mtfx_drive", "", ""},
		{"mtfx_width", "", ""},
		{"mt_senda", "", ""},
	}
	if a.mt.fxStage == 0 && a.shift {
		return [3]string{"mtfx_attack", "", "mtfx_release"}[k]
	}
	return t[a.mt.fxStage][k]
}

func (a *app) mtStageOn(i int) bool {
	switch i {
	case 0:
		return a.char["mtfx_comp_on"] > 0.5
	case 1:
		return a.char["mtfx_eq_on"] > 0.5
	case 2:
		return a.char["mtfx_drive_on"] > 0.5
	case 3:
		return a.char["mtfx_width_on"] > 0.5
	}
	return a.char["mt_senda"] > 0.005
}

func (a *app) mtFxToggle() {
	key := []string{"mtfx_comp_on", "mtfx_eq_on", "mtfx_drive_on", "mtfx_width_on", ""}[a.mt.fxStage]
	if key == "" {
		a.say("SEND A: K1 SETS HOW MUCH")
		return
	}
	a.char[key] = 1 - math.Round(a.char[key])
	if key == "mtfx_comp_on" && a.char[key] < 0.5 {
		a.char["mt_comp"] = 0
	}
	a.mtSendRec()
	a.saveCharacter()
	a.say(mtStages[a.mt.fxStage] + map[bool]string{true: " ON", false: " OFF"}[a.char[key] > 0.5])
	a.dirty = true
}

func (a *app) drawMtComp(c *gfx.Canvas) {
	ch := a.char
	c.Text(10, 50, "TAPE FX", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(86, 51, "ON THE TAKE ONLY", gfx.Small, uDim, gfx.Left|gfx.Mid)
	if a.mt.rec {
		blink := int(time.Since(a.mt.started).Seconds()*2)%2 == 0
		c.RoundRect(244, 44, 68, 13, 6, uBG, 0.9)
		if blink {
			c.Circle(252, 50.5, 3, 0xFF3B30, 1)
		}
		c.Text(306, 50.5, "REC "+clock(float64(a.mt.frames)/48000), gfx.Small, uText, gfx.Right|gfx.Mid)
	}
	cols := []uint32{knobCols[2], knobCols[0], knobCols[1], pLilac, pYellow}
	for i, name := range mtStages {
		x := 8 + float64(i)*61
		glass(c, x, 60, 57, 30, 6, cols[i])
		if i == a.mt.fxStage {
			c.RoundRectLine(x-2, 58, 61, 34, 7, 1.2, uText, 0.85)
		}
		c.Text(x+28.5, 70, name, gfx.Small, uText, gfx.Center|gfx.Mid)
		al := 0.25
		if a.mtStageOn(i) {
			al = 1
		}
		c.Circle(x+28.5, 82, 2.5, cols[i], al)
	}
	col := cols[a.mt.fxStage]
	glass(c, 8, 96, 304, 86, 8, col)
	x0, y0, w, h := 16.0, 102.0, 288.0, 74.0
	var labels, vals [3]string
	var norms [3]float64
	switch a.mt.fxStage {
	case 0: // the curve, and gain reduction
		thr, ratio, mk := mtThreshDb(ch["mtfx_thresh"]), mtRatio(ch["mtfx_ratio"]), mtMakeup(ch["mtfx_makeup"])
		var xs, ys []float64
		for k := 0; k <= 64; k++ {
			in := -48 + 48*float64(k)/64
			o := in
			if in > thr {
				o = thr + (in-thr)/ratio
			}
			o = math.Min(0, o+mk)
			xs = append(xs, x0+170*float64(k)/64)
			ys = append(ys, y0+h*(-o/48))
		}
		c.Line(x0, y0+h, x0+170, y0, 0.8, uDim, 0.5)
		c.Polyline(xs, ys, 1.6, col, 0.95)
		d := &a.dsp.mt
		d.mu.Lock()
		gr, at := d.gr, d.grAt
		d.mu.Unlock()
		db := 0.0
		if time.Since(at) < 300*time.Millisecond && gr > 0 {
			db = math.Min(0, 20*math.Log10(gr))
		}
		c.Text(200, y0+8, "GAIN REDUCTION", gfx.Small, uDim, gfx.Left|gfx.Mid)
		for i := 0; i < 12; i++ {
			on := float64(i) < -db/12*12
			cc, al := uint32(uFaint), 0.6
			if on {
				cc, al = col, 1
			}
			c.RoundRect(200+float64(i)*8, y0+20, 6, 14, 1.5, cc, al)
		}
		c.Text(200, y0+48, fmt.Sprintf("%.1f DB", db), gfx.Small, uText, gfx.Left|gfx.Mid)
		c.Text(200, y0+62, fmt.Sprintf("ATK %.0f MS · REL %.0f MS", mtAtkMs(ch["mtfx_attack"]), mtRelMs(ch["mtfx_release"])), gfx.Small, uDim, gfx.Left|gfx.Mid)
		labels = [3]string{"THRESH", "RATIO", "MAKEUP"}
		vals = [3]string{fmt.Sprintf("%.0f DB", thr), fmt.Sprintf("%.1f:1", ratio), fmt.Sprintf("+%.1f DB", mk)}
		norms = [3]float64{ch["mtfx_thresh"], ch["mtfx_ratio"], ch["mtfx_makeup"]}
		if a.shift {
			labels = [3]string{"ATTACK", "", "RELEASE"}
			vals = [3]string{fmt.Sprintf("%.0f MS", mtAtkMs(ch["mtfx_attack"])), "", fmt.Sprintf("%.0f MS", mtRelMs(ch["mtfx_release"]))}
			norms = [3]float64{ch["mtfx_attack"], 0, ch["mtfx_release"]}
		}
	case 1: // the EQ curve (log frequency)
		lc, mid, air := mtLowCut(ch["mtfx_lowcut"]), mtEqDb(ch["mtfx_mid"]), mtEqDb(ch["mtfx_air"])
		var xs, ys []float64
		for k := 0; k <= 120; k++ {
			f := 20 * math.Pow(1000, float64(k)/120)
			g := 0.0
			if f < lc {
				g -= 12 * math.Log2(lc/f)
			}
			g += mid * math.Exp(-math.Pow(math.Log2(f/1200)/1.1, 2))
			g += air / (1 + math.Pow(9000/f, 2))
			xs = append(xs, x0+w*float64(k)/120)
			ys = append(ys, y0+h/2-math.Max(-h/2, math.Min(h/2, g*2.6)))
		}
		c.Line(x0, y0+h/2, x0+w, y0+h/2, 0.6, uDim, 0.4)
		c.Polyline(xs, ys, 1.6, col, 0.95)
		labels = [3]string{"LOW CUT", "MID", "AIR"}
		vals = [3]string{fmt.Sprintf("%.0f HZ", lc), fmt.Sprintf("%+.1f DB", mid), fmt.Sprintf("%+.1f DB", air)}
		norms = [3]float64{ch["mtfx_lowcut"], ch["mtfx_mid"], ch["mtfx_air"]}
	case 2: // the drive curve
		dr := 1 + 8*ch["mtfx_drive"]
		var xs, ys []float64
		for k := 0; k <= 80; k++ {
			x := -1 + 2*float64(k)/80
			y := math.Tanh(x*dr) / math.Tanh(dr)
			xs = append(xs, 160+x*70)
			ys = append(ys, y0+h/2-y*h*0.45)
		}
		c.Polyline(xs, ys, 1.6, col, 0.95)
		labels, vals, norms = [3]string{"DRIVE", "", ""}, [3]string{fmt.Sprint(int(ch["mtfx_drive"]*99 + 0.5)), "", ""}, [3]float64{ch["mtfx_drive"], 0, 0}
	case 3: // width: the stereo image as two dots apart
		wd := 2 * ch["mtfx_width"]
		c.Circle(160-60*wd, y0+h/2, 6, col, 0.9)
		c.Circle(160+60*wd, y0+h/2, 6, col, 0.9)
		c.Line(160-60*wd, y0+h/2, 160+60*wd, y0+h/2, 1, col, 0.4)
		labels, vals, norms = [3]string{"WIDTH", "", ""}, [3]string{fmt.Sprintf("%.0f%%", wd*100), "", ""}, [3]float64{ch["mtfx_width"], 0, 0}
	case 4:
		sa := ch["mt_senda"]
		c.Text(x0+4, y0+16, "THE MIX INTO RACK A: "+a.sendFx(0), gfx.Label, uText, gfx.Left|gfx.Mid)
		c.Text(x0+4, y0+32, "WHILE THIS PAGE IS UP OR A TAKE RECORDS", gfx.Small, uDim, gfx.Left|gfx.Mid)
		c.RoundRect(x0+4, y0+50, 270, 5, 2.5, uFaint, 0.8)
		c.RoundRect(x0+4, y0+50, 270*sa, 5, 2.5, col, 1)
		labels, vals, norms = [3]string{"SEND A", "", ""}, [3]string{fmt.Sprint(int(sa*99 + 0.5)), "", ""}, [3]float64{sa, 0, 0}
	}
	if !a.mtStageOn(a.mt.fxStage) {
		c.Text(304, 108, "OFF · PUSH", gfx.Small, uDim, gfx.Right|gfx.Mid)
	}
	act := -1
	if time.Since(a.mt.knobAt) < 2*time.Second {
		act = a.mt.knobK
	}
	mockKnobs(c, labels, vals, norms, act)
	a.animate(time.Now().Add(150 * time.Millisecond))
	a.uiBottom(c, "DATA: STAGE  ·  PUSH: ON / OFF  ·  RIGHT: BACK TO THE TAPE")
}
