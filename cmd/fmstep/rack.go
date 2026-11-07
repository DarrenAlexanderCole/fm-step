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
	"time"

	"fmstep/internal/gfx"
)

var rackFx = []string{"DUB DELAY", "REVERB", "SPACE ECHO", "CHORUS", "PHASER", "CRUSH"}

func init() {
	// 0.9.26: six effects, index/5 (0.9.25's send_a_fx / send_b_fx are migrated)
	charDefaults["rack_a"] = 0   // A: dub delay
	charDefaults["rack_b"] = 0.2 // B: reverb
	charDefaults["space_amt"] = 0.55
	charDefaults["rack_chorus_rate"] = 0.35
	charDefaults["rack_chorus_depth"] = 0.6
	charDefaults["rack_phaser_rate"] = 0.3
	charDefaults["rack_phaser_fb"] = 0.5
	charDefaults["rack_crush_bits"] = 0.4
	charDefaults["rack_crush_rate"] = 0.4
	// 0.9.27 MASTER: bus compressor and limiter (off until switched on)
	for k, v := range map[string]float64{"mc_on": 0, "mc_thresh": 0.6, "mc_ratio": 0.12, "mc_attack": 0.3, "mc_release": 0.2, "mc_makeup": 0.2,
		"lim_on": 0, "lim_ceiling": 0.95, "lim_release": 0.15} {
		charDefaults[k] = v
	}
}

func fxIdx(v float64) int { return max(0, min(len(rackFx)-1, int(math.Round(v*5)))) }

// migrateRack: a 0.9.25 character.json (send_a_fx / send_b_fx: 0, 0.5, 1)
func migrateRack(m map[string]float64) {
	for old, nw := range map[string]string{"send_a_fx": "rack_a", "send_b_fx": "rack_b"} {
		if v, ok := m[old]; ok {
			if _, has := m[nw]; !has {
				m[nw] = float64(max(0, min(2, int(math.Round(v*2))))) / 5
			}
		}
	}
}

var (
	pSendAFx = param{key: "rack_a", label: "EFFECT", max: 1, steps: 6, show: func(v float64) string { return rackFx[fxIdx(v)] }}
	pSendBFx = param{key: "rack_b", label: "EFFECT", max: 1, steps: 6, show: func(v float64) string { return rackFx[fxIdx(v)] }}
	pChRate  = cont("rack_chorus_rate", "RATE")
	pChDepth = cont("rack_chorus_depth", "DEPTH")
	pPhRate  = cont("rack_phaser_rate", "RATE")
	pPhFb    = cont("rack_phaser_fb", "RESONANCE")
	pCrBits  = cont("rack_crush_bits", "CRUSH")
	pCrRate  = cont("rack_crush_rate", "DOWNSAMPLE")
	pSpace   = cont("space_amt", "AMOUNT")
	pRvSize  = cont("reverb_size", "SIZE")
	pRvTone  = cont("reverb_tone", "TONE")
	pTDrive  = cont("tape_drive", "TAPE DRIVE")
	pGlue    = cont("glue", "GLUE")
	pTAge    = cont("tape_age", "AGE")
	pNone    = param{key: "", label: "", max: 1, show: func(float64) string { return "" }}
	pMcThr   = param{key: "mc_thresh", label: "THRESH", max: 1, show: func(v float64) string { return fmt.Sprintf("%.0f DB", -30+30*v) }}
	pMcRatio = param{key: "mc_ratio", label: "RATIO", max: 1, show: func(v float64) string { return fmt.Sprintf("%.1f:1", 1.5+8.5*v) }}
	pMcAtk   = param{key: "mc_attack", label: "ATTACK", max: 1, show: func(v float64) string { return fmt.Sprintf("%.0f MS", 1+99*v) }}
	pMcRel   = param{key: "mc_release", label: "RELEASE", max: 1, show: func(v float64) string { return fmt.Sprintf("%.0f MS", 30+970*v) }}
	pMcMk    = param{key: "mc_makeup", label: "MAKEUP", max: 1, show: func(v float64) string { return fmt.Sprintf("+%.1f DB", 12*v) }}
	pLimCeil = param{key: "lim_ceiling", label: "CEILING", max: 1, show: func(v float64) string { return fmt.Sprintf("%.1f DB", -6+6*v) }}
	pLimRel  = param{key: "lim_release", label: "RELEASE", max: 1, show: func(v float64) string { return fmt.Sprintf("%.0f MS", 10+490*v) }}
)

const rackSlots = 5 // A, B, TAPE, MASTER COMP, LIMITER

// rackParams: what K1-K3 do on the picked slot.
func (a *app) rackParams() [3]*param {
	fxParams := func(fx int) (*param, *param) {
		switch fx {
		case 0:
			return &pDlyDiv, &pFeedback
		case 1:
			return &pRvSize, &pRvTone
		case 3:
			return &pChRate, &pChDepth
		case 4:
			return &pPhRate, &pPhFb
		case 5:
			return &pCrBits, &pCrRate
		}
		return &pSpace, &pNone
	}
	switch a.ui.rackSlot {
	case 0:
		p2, p3 := fxParams(fxIdx(a.val("rack_a")))
		return [3]*param{&pSendAFx, p2, p3}
	case 1:
		p2, p3 := fxParams(fxIdx(a.val("rack_b")))
		return [3]*param{&pSendBFx, p2, p3}
	case 3:
		if a.shift {
			return [3]*param{&pMcRel, &pNone, &pMcMk}
		}
		return [3]*param{&pMcThr, &pMcRatio, &pMcAtk}
	case 4:
		return [3]*param{&pLimCeil, &pLimRel, &pNone}
	}
	return [3]*param{&pTDrive, &pGlue, &pTAge}
}

func (a *app) rackData(d int) {
	a.ui.rackSlot = max(0, min(rackSlots-1, a.ui.rackSlot+d))
	a.accum = [3]float64{}
	a.dirty = true
}

// sendFx: the effect a send feeds (0 A, 1 B), for the mixer.
func (a *app) sendFx(s int) string {
	if s == 0 {
		return rackFx[fxIdx(a.val("rack_a"))]
	}
	return rackFx[fxIdx(a.val("rack_b"))]
}

func (a *app) drawRack(c *gfx.Canvas) {
	c.Text(10, 33, "RACK", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(72, 34, "SENDS  ·  MASTER", gfx.Small, uDim, gfx.Left|gfx.Mid)
	slot := func(i int, y float64, tag, name, info string, col uint32, keys string) {
		glass(c, 8, y, 304, 27, 6, col)
		if i == a.ui.rackSlot {
			c.RoundRectLine(6, y-1.5, 308, 30, 7, 1.2, uText, 0.85)
		}
		c.RoundRect(14, y+5, 18, 17, 5, col, 0.9)
		c.Text(23, y+13.5, tag, gfx.Small, uBG, gfx.Center|gfx.Mid)
		c.Text(40, y+8.5, name, gfx.Small, uText, gfx.Left|gfx.Mid)
		c.Text(40, y+19.5, info, gfx.Small, uDim, gfx.Left|gfx.Mid)
		if keys == "" {
			return
		}
		// who sends here: a dot per track, sized by its send
		for t := 0; t < 8; t++ {
			v := a.proj.Tracks[t].Sound.P[keys]
			x := 204 + float64(t)*13
			c.Circle(x, y+13.5, 1.2+3.5*v, tcol(t), 0.25+0.75*math.Min(1, v*2))
		}
	}
	info := func(fx int) string {
		switch fx {
		case 0:
			return fmt.Sprintf("%s  ·  FEEDBACK %d", pDlyDiv.show(a.val("delay_div")), int(a.val("delay_feedback")*99+0.5))
		case 1:
			return fmt.Sprintf("SIZE %d  ·  TONE %d", int(a.val("reverb_size")*99+0.5), int(a.val("reverb_tone")*99+0.5))
		case 3:
			return fmt.Sprintf("RATE %s  ·  DEPTH %s", pct(a.val("rack_chorus_rate")), pct(a.val("rack_chorus_depth")))
		case 4:
			return fmt.Sprintf("RATE %s  ·  RESONANCE %s", pct(a.val("rack_phaser_rate")), pct(a.val("rack_phaser_fb")))
		case 5:
			return fmt.Sprintf("CRUSH %s  ·  DOWNSAMPLE %s", pct(a.val("rack_crush_bits")), pct(a.val("rack_crush_rate")))
		}
		return fmt.Sprintf("AMOUNT %d  ·  DOTTED 1/16", int(a.val("space_amt")*99+0.5))
	}
	fa, fb := fxIdx(a.val("rack_a")), fxIdx(a.val("rack_b"))
	slot(0, 42, "A", rackFx[fa], info(fa), knobCols[1], "delay_send")
	slot(1, 72, "B", rackFx[fb], info(fb), knobCols[2], "reverb_send")
	slot(2, 102, "T", "TAPE + GLUE", fmt.Sprintf("DRIVE %d  ·  GLUE %d  ·  AGE %d", pct2(a.val("tape_drive")), pct2(a.val("glue")), pct2(a.val("tape_age"))), pYellow, "")
	slot(3, 132, "C", "MASTER COMP", fmt.Sprintf("%s  ·  %s  ·  %s", pMcThr.show(a.val("mc_thresh")), pMcRatio.show(a.val("mc_ratio")), pMcAtk.show(a.val("mc_attack"))), 0x5FE8FF, "")
	slot(4, 162, "L", "LIMITER", fmt.Sprintf("CEILING %s  ·  RELEASE %s", pLimCeil.show(a.val("lim_ceiling")), pLimRel.show(a.val("lim_release"))), uRed, "")
	// switches and gain reduction for the master blocks
	a.dsp.pads.mu.Lock()
	gr, grAt := a.dsp.pads.mgr, a.dsp.pads.mgrAt
	a.dsp.pads.mu.Unlock()
	for k, key := range []string{"mc_on", "lim_on"} {
		y := 132 + float64(k)*30
		on := a.val(key) > 0.5
		col := []uint32{0x5FE8FF, uRed}[k]
		c.RoundRect(204, y+7.5, 22, 12, 6, uPanel2, 1)
		if on {
			c.RoundRect(204, y+7.5, 22, 12, 6, col, 0.9)
			c.Circle(220, y+13.5, 4.5, uBG, 1)
		} else {
			c.Circle(210, y+13.5, 4.5, uDim, 1)
		}
		db := 0.0
		if on && time.Since(grAt) < 400*time.Millisecond && gr[k] > 0 {
			db = -20 * math.Log10(gr[k])
		}
		for j := 0; j < 10; j++ {
			lit := float64(j) < db*1.5
			cc, al := uint32(uFaint), 0.6
			if lit {
				cc, al = col, 1
			}
			c.RoundRect(236+float64(j)*7, y+8.5, 5, 10, 1, cc, al)
		}
	}
	a.animate(time.Now().Add(150 * time.Millisecond))
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
	hint := "DATA: SLOT  ·  K1: WHICH EFFECT  ·  KNOB FX: MIXER SENDS"
	switch a.ui.rackSlot {
	case 3:
		hint = "PUSH: ON / OFF  ·  SHIFT: RELEASE · MAKEUP"
	case 4:
		hint = "PUSH: ON / OFF  ·  THE LAST WORD BEFORE THE OUTPUT"
	}
	a.uiBottom(c, hint)
}

// glass: an acrylic panel: see-through fill, a faint tint, a bright top edge.
func glass(c *gfx.Canvas, x, y, w, h, r float64, tint uint32) {
	c.RoundRect(x, y, w, h, r, 0x121411, 0.78)
	c.RoundRect(x, y, w, h, r, tint, 0.06)
	c.RoundRectLine(x, y, w, h, r, 1, tint, 0.45)
	c.Line(x+r, y+1.2, x+w-r, y+1.2, 1, 0xFFFFFF, 0.16)
	c.Line(x+1.2, y+r, x+1.2, y+h*0.45, 1, 0xFFFFFF, 0.08)
}

func pct2(v float64) int { return int(v*99 + 0.5) }

// rackPush: DATA push on the RACK switches the master comp / limiter
func (a *app) rackPush() {
	key := map[int]string{3: "mc_on", 4: "lim_on"}[a.ui.rackSlot]
	if key == "" {
		return
	}
	v := 1.0
	if a.val(key) > 0.5 {
		v = 0
	}
	a.setVal(key, v)
	a.saveCharacter()
	a.say(map[string]string{"mc_on": "MASTER COMP", "lim_on": "LIMITER"}[key] + map[bool]string{true: " ON", false: " OFF"}[v > 0.5])
	a.dirty = true
}
