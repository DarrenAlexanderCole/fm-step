package main

// COMP + FX (0.9.26): the recorder's right button while recording or before
// a take (SHIFT + pad 5 COMPRESSOR opens it too). What the take goes through.
//
//   K1 COMP     OFF · GLUE · PUNCH · LIMIT
//   K2 AMOUNT   how hard (the threshold, +-12 dB around the type's own)
//   K3 SEND A   the mix into RACK send A's effect (while this page is up or a take records)

import (
	"fmt"
	"math"
	"time"

	"fmstep/internal/gfx"
)

// comp curve: output dB for input dB (the engine's types, AMOUNT applied)
func compOut(cm int, amt, in float64) float64 {
	if cm <= 0 {
		return in
	}
	thr := []float64{0, -18, -14, -3}[cm] + 12*(1-2*amt)
	ratio := []float64{1, 2, 4, 20}[cm]
	mk := []float64{0, 3, 5, 0}[cm]
	out := in
	if in > thr {
		out = thr + (in-thr)/ratio
	}
	return math.Min(0, out+mk)
}

func (a *app) drawMtComp(c *gfx.Canvas) {
	cm, amt, sa := a.mtCompIdx(), a.char["mt_camt"], a.char["mt_senda"]
	c.Text(10, 50, "COMP + FX", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(112, 51, "ON THE TAKE", gfx.Small, uDim, gfx.Left|gfx.Mid)
	if a.mt.rec {
		blink := int(time.Since(a.mt.started).Seconds()*2)%2 == 0
		c.RoundRect(244, 44, 68, 13, 6, uBG, 0.9)
		if blink {
			c.Circle(252, 50.5, 3, 0xFF3B30, 1)
		}
		c.Text(306, 50.5, "REC "+clock(float64(a.mt.frames)/48000), gfx.Small, uText, gfx.Right|gfx.Mid)
	}
	// the curve
	glass(c, 8, 62, 150, 112, 8, shPink)
	x0, y0, w, h := 18.0, 72.0, 130.0, 92.0
	c.Line(x0, y0+h, x0+w, y0, 0.8, uDim, 0.5) // 1:1
	var xs, ys []float64
	for k := 0; k <= 64; k++ {
		in := -40 + 40*float64(k)/64
		o := compOut(cm, amt, in)
		xs = append(xs, x0+w*float64(k)/64)
		ys = append(ys, y0+h*(-o/40))
	}
	c.Polyline(xs, ys, 1.6, shPink, 0.95)
	c.Text(24, 76, mtComps[cm], gfx.Small, uText, gfx.Left|gfx.Mid)
	// gain reduction
	glass(c, 164, 62, 148, 52, 8, shPink)
	c.Text(172, 74, "GAIN REDUCTION", gfx.Small, uDim, gfx.Left|gfx.Mid)
	d := &a.dsp.mt
	d.mu.Lock()
	gr, at := d.gr, d.grAt
	d.mu.Unlock()
	db := 0.0
	if time.Since(at) < 300*time.Millisecond && gr > 0 {
		db = math.Min(0, 20*math.Log10(gr))
	}
	for i := 0; i < 16; i++ {
		x := 172 + float64(i)*7
		on := float64(i) < -db/12*16
		col, al := uint32(uFaint), 0.6
		if on {
			col, al = shPink, 1
		}
		c.RoundRect(x, 84, 5, 14, 1.5, col, al)
	}
	c.Text(306, 91, fmt.Sprintf("%.1f", db), gfx.Small, uText, gfx.Right|gfx.Mid)
	if !a.mt.rec && !a.mt.monitor {
		c.Text(172, 106, "LEFT: MONITOR TO HEAR IT", gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
	// send A
	glass(c, 164, 120, 148, 54, 8, knobCols[2])
	c.Text(172, 132, "SEND TO RACK A", gfx.Small, uDim, gfx.Left|gfx.Mid)
	c.Text(172, 148, a.sendFx(0), gfx.Label, uText, gfx.Left|gfx.Mid)
	c.RoundRect(172, 160, 132, 4, 2, uFaint, 0.8)
	c.RoundRect(172, 160, 132*sa, 4, 2, knobCols[2], 1)
	labels := [3]string{"COMP", "AMOUNT", "SEND A"}
	vals := [3]string{mtComps[cm], fmt.Sprint(int(amt*99 + 0.5)), fmt.Sprint(int(sa*99 + 0.5))}
	norms := [3]float64{float64(cm) / 3, amt, sa}
	act := -1
	if time.Since(a.mt.knobAt) < 2*time.Second {
		act = a.mt.knobK
	}
	mockKnobs(c, labels, vals, norms, act)
	a.animate(time.Now().Add(150 * time.Millisecond))
	a.uiBottom(c, "RIGHT: BACK TO THE TAPE  ·  LEFT: MONITOR")
}
