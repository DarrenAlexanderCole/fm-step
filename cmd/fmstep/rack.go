package main

// The RACK (0.9.25, PAD FX): what the two sends feed and the master chain.
//
//   DATA turn        pick a slot: A, B, MASTER
//   K1               A / B: which effect (DUB DELAY, REVERB, SPACE ECHO) · MASTER: TAPE DRIVE
//   K2 · K3          that effect's settings
//   PAD FX / middle  back to play
//
// On the mixer, K3 sets the send that is showing (A or B); DATA push flips it.

import (
	"fmt"
	"math"

	"fmstep/internal/gfx"
)

var rackFx = []string{"DUB DELAY", "REVERB", "SPACE ECHO"}

func init() {
	charDefaults["send_a_fx"] = 0   // A: dub delay (0, 0.5, 1 = the three effects)
	charDefaults["send_b_fx"] = 0.5 // B: reverb
	charDefaults["space_amt"] = 0.55
}

func fxIdx(v float64) int { return max(0, min(2, int(math.Round(v*2)))) }

var (
	pSendAFx = param{key: "send_a_fx", label: "EFFECT", max: 1, steps: 3, show: func(v float64) string { return rackFx[fxIdx(v)] }}
	pSendBFx = param{key: "send_b_fx", label: "EFFECT", max: 1, steps: 3, show: func(v float64) string { return rackFx[fxIdx(v)] }}
	pSpace   = cont("space_amt", "AMOUNT")
	pRvSize  = cont("reverb_size", "SIZE")
	pRvTone  = cont("reverb_tone", "TONE")
	pTDrive  = cont("tape_drive", "TAPE DRIVE")
	pGlue    = cont("glue", "GLUE")
	pTAge    = cont("tape_age", "AGE")
	pNone    = param{key: "", label: "", max: 1, show: func(float64) string { return "" }}
)

// rackParams: what K1-K3 do on the picked slot.
func (a *app) rackParams() [3]*param {
	fxParams := func(fx int) (*param, *param) {
		switch fx {
		case 0:
			return &pDlyDiv, &pFeedback
		case 1:
			return &pRvSize, &pRvTone
		}
		return &pSpace, &pNone
	}
	switch a.ui.rackSlot {
	case 0:
		p2, p3 := fxParams(fxIdx(a.val("send_a_fx")))
		return [3]*param{&pSendAFx, p2, p3}
	case 1:
		p2, p3 := fxParams(fxIdx(a.val("send_b_fx")))
		return [3]*param{&pSendBFx, p2, p3}
	}
	return [3]*param{&pTDrive, &pGlue, &pTAge}
}

func (a *app) rackData(d int) {
	a.ui.rackSlot = max(0, min(2, a.ui.rackSlot+d))
	a.accum = [3]float64{}
	a.dirty = true
}

// sendFx: the effect a send feeds (0 A, 1 B), for the mixer.
func (a *app) sendFx(s int) string {
	if s == 0 {
		return rackFx[fxIdx(a.val("send_a_fx"))]
	}
	return rackFx[fxIdx(a.val("send_b_fx"))]
}

func (a *app) drawRack(c *gfx.Canvas) {
	c.Text(10, 33, "RACK", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(72, 34, "SENDS  ·  MASTER", gfx.Small, uDim, gfx.Left|gfx.Mid)
	slot := func(i int, y float64, tag, name, info string, col uint32, keys string) {
		glass(c, 8, y, 304, 40, 8, col)
		if i == a.ui.rackSlot {
			c.RoundRectLine(6, y-2, 308, 44, 9, 1.2, uText, 0.85)
		}
		c.RoundRect(16, y+8, 26, 24, 6, col, 0.9)
		c.Text(29, y+20, tag, gfx.Label, uBG, gfx.Center|gfx.Mid)
		c.Text(52, y+14, name, gfx.Label, uText, gfx.Left|gfx.Mid)
		c.Text(52, y+28, info, gfx.Small, uDim, gfx.Left|gfx.Mid)
		if keys == "" {
			return
		}
		// who sends here: a dot per track, sized by its send
		for t := 0; t < 8; t++ {
			v := a.proj.Tracks[t].Sound.P[keys]
			x := 204 + float64(t)*13
			c.Circle(x, y+20, 1.2+4*v, tcol(t), 0.25+0.75*math.Min(1, v*2))
		}
	}
	info := func(fx int) string {
		switch fx {
		case 0:
			return fmt.Sprintf("%s  ·  FEEDBACK %d", pDlyDiv.show(a.val("delay_div")), int(a.val("delay_feedback")*99+0.5))
		case 1:
			return fmt.Sprintf("SIZE %d  ·  TONE %d", int(a.val("reverb_size")*99+0.5), int(a.val("reverb_tone")*99+0.5))
		}
		return fmt.Sprintf("AMOUNT %d  ·  DOTTED 1/16", int(a.val("space_amt")*99+0.5))
	}
	fa, fb := fxIdx(a.val("send_a_fx")), fxIdx(a.val("send_b_fx"))
	slot(0, 44, "A", rackFx[fa], info(fa), knobCols[2], "delay_send")
	slot(1, 90, "B", rackFx[fb], info(fb), pLilac, "reverb_send")
	slot(2, 136, "M", "MASTER: TAPE + GLUE", fmt.Sprintf("DRIVE %d  ·  GLUE %d  ·  AGE %d", int(a.val("tape_drive")*99+0.5), int(a.val("glue")*99+0.5), int(a.val("tape_age")*99+0.5)), pYellow, "")
	ps := a.rackParams()
	var labels, vals [3]string
	var norms [3]float64
	for k := 0; k < 3; k++ {
		if ps[k].key == "" {
			continue
		}
		v := a.val(ps[k].key)
		labels[k], vals[k], norms[k] = ps[k].label, ps[k].show(v), ps[k].norm(v)
	}
	kk, _ := a.turning()
	mockKnobs(c, labels, vals, norms, kk)
	a.uiBottom(c, "DATA: SLOT  ·  K1: WHICH EFFECT  ·  MIXER K3 SENDS (PUSH DATA THERE: A / B)")
}

// glass: an acrylic panel: see-through fill, a faint tint, a bright top edge.
func glass(c *gfx.Canvas, x, y, w, h, r float64, tint uint32) {
	c.RoundRect(x, y, w, h, r, 0x121411, 0.78)
	c.RoundRect(x, y, w, h, r, tint, 0.06)
	c.RoundRectLine(x, y, w, h, r, 1, tint, 0.45)
	c.Line(x+r, y+1.2, x+w-r, y+1.2, 1, 0xFFFFFF, 0.16)
	c.Line(x+1.2, y+r, x+1.2, y+h*0.45, 1, 0xFFFFFF, 0.08)
}
