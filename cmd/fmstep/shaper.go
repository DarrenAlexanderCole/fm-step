package main

// SHAPER (0.9.26): where a pad's sound gets shaped. CHOP opens it. Hit a pad
// and its sound fills the card; the envelope rides on top as a faint line
// with four nodes and the wave follows it.
//
//   pads            play and pick the pad · an empty pad asks where its sound comes from
//   K1 · K2 · K3    ATTACK · DECAY (SHIFT: SUSTAIN) · RELEASE
//   left button     ONE SHOT / GATE / LOOP for this pad (empty pad: SAMPLE IN)
//   middle button   play the pad (empty pad: -)
//   right button    empty pad: SAMPLE SELECT (the card)
//   DATA            pick the pad
//
// Each pad's envelope and mode are kept in shaper.json next to character.json
// and sent to the engine (SLENV) at start, so they hold for any sound loaded.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"fmstep/internal/gfx"
)

// padEnv: knob positions 0..1 (see the ms helpers) and the play mode
type padEnv struct {
	A, D, S, R float64
	Mode       int
}

var defaultEnv = padEnv{A: 0, D: 0, S: 1, R: 0, Mode: 0}

func envAms(n float64) float64 { return 1.5 * math.Pow(2000/1.5, n) } // 1.5 ms .. 2 s
func envDms(n float64) float64 { // 0 .. 4 s
	if n <= 0.001 {
		return 0
	}
	return 5 * math.Pow(4000.0/5, n)
}
func envRms(n float64) float64 { return 5 * math.Pow(4000.0/5, n) } // 5 ms .. 4 s

func msText(ms float64) string {
	switch {
	case ms <= 0:
		return "0"
	case ms < 10:
		return fmt.Sprintf("%.1f MS", ms)
	case ms < 1000:
		return fmt.Sprintf("%d MS", int(ms+0.5))
	}
	return fmt.Sprintf("%.2f S", ms/1000)
}

func (a *app) shaperPath() string { return filepath.Join(a.user, "shaper.json") }

func (a *app) shaperLoad() {
	for i := range a.smp.env {
		a.smp.env[i] = defaultEnv
	}
	if b, err := os.ReadFile(a.shaperPath()); err == nil {
		var e [16]padEnv
		if json.Unmarshal(b, &e) == nil {
			for i := range e {
				e[i].A, e[i].D = clamp01(e[i].A), clamp01(e[i].D)
				e[i].S, e[i].R = clamp01(e[i].S), clamp01(e[i].R)
				e[i].Mode = max(0, min(2, e[i].Mode))
			}
			a.smp.env = e
		}
	}
	for i := range a.smp.env {
		a.shaperSend(i)
	}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func (a *app) shaperSave(now bool) {
	if !a.smp.envDirty || (!now && time.Since(a.smp.envSaved) < 2*time.Second) {
		return
	}
	b, _ := json.MarshalIndent(a.smp.env, "", "  ")
	if os.WriteFile(a.shaperPath(), b, 0644) == nil {
		a.smp.envDirty, a.smp.envSaved = false, time.Now()
	}
}

func (a *app) shaperSend(i int) {
	if a.dsp == nil {
		return
	}
	e := a.smp.env[i]
	a.dsp.send("SLENV %d %.1f %.1f %.3f %.1f %d", i, envAms(e.A), envDms(e.D), e.S, envRms(e.R), e.Mode)
}

func (a *app) shaperOpen() {
	a.smpInit()
	a.mt.view = false
	if a.mode == modeRack || a.mode == modeConfirmExit {
		a.setMode(modePlay)
	}
	a.smp.ask = -1
	if !a.smpGrabbed() || !a.padHasSound(a.smp.sel) {
		a.smp.ask = a.smp.sel
	}
	a.smpEnter(smpShaper)
}

func (a *app) padHasSound(i int) bool {
	n := a.dsp.smp.grabLen
	if n <= 0 || i < 0 || i > 15 {
		return false
	}
	s, e, _, _ := a.smpSlice(i, n)
	return e > s
}

// shaperButton: the screen buttons in SHAPER (down only)
func (a *app) shaperButton(note int) {
	i := a.smp.sel
	switch {
	case a.smp.ask >= 0 && note == 0: // SAMPLE IN: the recorder
		a.smp.ask = -1
		a.mtShow(true)
		a.say("SAMPLE IN  ·  SAMPLE RECORD: A TAKE  ·  THEN SAMPLE SELECT LOADS IT")
	case a.smp.ask >= 0 && note == 2: // SAMPLE SELECT: the card
		a.smp.ask = -1
		a.smpEnter(smpTape)
		a.say("SAMPLE SELECT  ·  DATA: PICK  ·  PUSH: LOAD")
	case a.smp.ask >= 0:
	case note == 0:
		a.smp.env[i].Mode = (a.smp.env[i].Mode + 1) % 3
		a.smp.envDirty = true
		a.shaperSend(i)
		a.say(fmt.Sprintf("PAD %d: %s", i+1, smpMotions[a.smp.env[i].Mode]))
	case note == 1:
		a.smpPad(i, 110, true)
		a.smp.btnHeld = true
	}
	a.dirty = true
}

func (a *app) shaperKnob(k, delta int) {
	i := a.smp.sel
	if a.smp.ask >= 0 {
		a.say("THIS PAD IS EMPTY  ·  LEFT: SAMPLE IN  ·  RIGHT: SAMPLE SELECT")
		return
	}
	e := &a.smp.env[i]
	d := float64(delta) / 110
	switch {
	case k == 0:
		e.A = clamp01(e.A + d)
	case k == 1 && a.shift:
		e.S = clamp01(e.S + d)
	case k == 1:
		e.D = clamp01(e.D + d)
	case k == 2:
		e.R = clamp01(e.R + d)
	}
	a.smp.envKnob, a.smp.envSus = k, k == 1 && a.shift
	a.smp.envDirty = true
	a.shaperSend(i)
	a.shaperSave(false)
}

// shaperPlaying: how far through pad i is (0..1), -1 if silent
func (a *app) shaperPlaying(i int) float64 {
	if a.smp.hitAt[i].IsZero() {
		return -1
	}
	dur := a.sliceDur(i)
	t := time.Since(a.smp.hitAt[i]).Seconds()
	mode := a.smp.env[i].Mode
	held := a.smp.held[i]
	if mode == 2 && held && dur > 0 {
		return math.Mod(t, dur) / dur
	}
	if dur <= 0 || t > dur || (mode >= 1 && !held && t > envRms(a.smp.env[i].R)/1000+0.03) {
		return -1
	}
	return t / dur
}

// envAt: the envelope level at t seconds into a sound dur long, released at off
func envAt(e padEnv, t, off float64) float64 {
	A, D, R := envAms(e.A)/1000, envDms(e.D)/1000, envRms(e.R)/1000
	var v float64
	switch {
	case t < A:
		v = t / A
	case t < A+D:
		v = 1 - (1-e.S)*(t-A)/D
	default:
		v = e.S
	}
	if t > off {
		v *= math.Max(0, 1-(t-off)/R)
	}
	return v
}

// shaperWave: the pad's peaks, interpolated to cols (0..1, normalised)
func (a *app) shaperWave(i, cols int) []float64 {
	n := a.dsp.smp.grabLen
	if n <= 0 {
		return nil
	}
	s, e, _, _ := a.smpSlice(i, n)
	if e <= s {
		return nil
	}
	d := &a.dsp.smp
	d.mu.Lock()
	f0, f1 := float64(s)*2048/float64(n), float64(e)*2048/float64(n)
	src := make([]float64, 0, 64)
	for p := int(f0); p <= int(math.Ceil(f1)) && p < 2048; p++ {
		src = append(src, lvl(d.gwf[p]))
	}
	d.mu.Unlock()
	if len(src) < 2 {
		return nil
	}
	out := make([]float64, cols)
	pk := 0.0
	for k := 0; k < cols; k++ {
		x := f0 + (f1-f0)*float64(k)/float64(cols-1) - math.Floor(f0)
		j := int(x)
		fr := x - float64(j)
		v := src[min(j, len(src)-1)]
		if j+1 < len(src) {
			v += (src[j+1] - v) * fr
		}
		out[k] = v
		pk = math.Max(pk, v)
	}
	if pk > 0 {
		for k := range out {
			out[k] /= pk
		}
	}
	return out
}

const (
	shBlue = 0x2F7BFF
	shLine = 0x7CC4FF
	shDeep = 0x5A3CFF
	shPink = 0xFF3FA4
	shLime = 0xD8FF4A
)

func (a *app) drawShaper(c *gfx.Canvas) {
	i := a.smp.sel
	e := a.smp.env[i]
	if a.smp.ask >= 0 {
		btnLegend(c, [3]string{"SAMPLE IN", "", "SAMPLE SELECT"}, -1)
	} else {
		btnLegend(c, [3]string{smpMotions[e.Mode], "PLAY", ""}, 0)
	}
	x0, y0, w, h := 8.0, 58.0, 304.0, 116.0
	if a.smp.ask < 0 {
		c.Text(10, 46, "SHAPER", gfx.Big, uText, gfx.Left|gfx.Mid)
		c.Text(96, 47, fmt.Sprintf("PAD %02d  ·  %s", i+1, a.smp.srcName), gfx.Small, uDim, gfx.Left|gfx.Mid)
	}
	glass(c, x0, y0, w, h, 10, 0x9AA0A6)
	for gy := y0 + 6; gy < y0+h-2; gy += 6 {
		for gx := x0 + 6; gx < x0+w-2; gx += 6 {
			c.Circle(gx, gy, 0.5, uDim, 0.12)
		}
	}
	if a.smp.ask >= 0 {
		c.RoundRect(40, 84, 240, 64, 12, uBG, 0.9)
		c.RoundRectLine(40, 84, 240, 64, 12, 1, 0x5FE8FF, 0.7)
		c.Text(160, 102, fmt.Sprintf("PAD %02d IS EMPTY", a.smp.ask+1), gfx.Title, uText, gfx.Center|gfx.Mid)
		c.Text(160, 124, "LEFT: SAMPLE IN  ·  RIGHT: FROM THE CARD", gfx.Small, uDim, gfx.Center|gfx.Mid)
		a.uiBottom(c, "ANY PAD WITH A SOUND: SHAPE IT")
		return
	}
	cols := int(w - 16)
	wave := a.shaperWave(i, cols)
	dur := a.sliceDur(i)
	mid, amp := y0+h/2+6, h/2-12
	ex := func(t float64) float64 { return x0 + 8 + t*(w-16) }
	R := envRms(e.R) / 1000
	off := dur
	switch {
	case e.Mode == 0:
		off = math.Max(0, dur-R)
	default: // gate / loop: drawn as if let go at 70%
		off = math.Max(envAms(e.A)/1000+envDms(e.D)/1000, dur*0.7)
	}
	if wave != nil && dur > 0 {
		top, bot := make([]float64, cols), make([]float64, cols)
		xs := make([]float64, cols)
		for k := 0; k < cols; k++ {
			t := float64(k) / float64(cols-1)
			g := envAt(e, t*dur, off)
			xs[k] = ex(t)
			c.Line(xs[k], mid-wave[k]*amp, xs[k], mid+wave[k]*amp*0.9, 1, uDim, 0.10) // the untouched sound
			top[k], bot[k] = mid-wave[k]*amp*g, mid+wave[k]*amp*0.9*g
			if bot[k]-top[k] >= 0.5 { // 0.9.27: coloured by the stage it's in (the knob colours)
				col := envCol(e, t*dur, off)
				c.Line(xs[k], top[k], xs[k], mid, 1, col, 0.30+0.35*g)
				c.Line(xs[k], mid, xs[k], bot[k], 1, col, 0.20+0.25*g)
			}
		}
		for k := 1; k < cols; k++ {
			col := envCol(e, float64(k)/float64(cols-1)*dur, off)
			c.Line(xs[k-1], top[k-1], xs[k], top[k], 1, gfx.Mix(col, 0xFFFFFF, 0.35), 0.9)
			c.Line(xs[k-1], bot[k-1], xs[k], bot[k], 1, col, 0.7)
		}
		// beat lines at the project tempo
		if bpm := a.proj.G["tempo"]; bpm > 0 {
			beat := 60 / bpm
			for b := 1; float64(b)*beat < dur && b < 64; b++ {
				x := ex(float64(b) * beat / dur)
				c.Line(x, y0+16, x, y0+h-12, 1, shLime, 0.16)
			}
		}
		if e.Mode == 2 { // LOOP: the whole pad goes round
			c.Line(ex(0), y0+10, ex(1), y0+10, 1.2, shPink, 0.8)
			c.Line(ex(0), y0+7, ex(0), y0+h-10, 1, shPink, 0.6)
			c.Line(ex(1), y0+7, ex(1), y0+h-10, 1, shPink, 0.6)
		}
		// the envelope: faint line, four nodes
		ey := func(v float64) float64 { return mid - amp - 4 + (1-v)*(amp*0.9) }
		var exs, eys []float64
		for k := 0; k <= 160; k++ {
			t := float64(k) / 160
			exs, eys = append(exs, ex(t)), append(eys, ey(envAt(e, t*dur, off)))
		}
		c.Polyline(exs, eys, 1, 0xFFFFFF, 0.35)
		A, D := envAms(e.A)/1000, envDms(e.D)/1000
		nodes := [4][2]float64{{A, 1}, {A + D, e.S}, {off, e.S}, {math.Min(dur, off+R), 0}}
		hot := -1
		if time.Since(a.ui.touchAt) < 2*time.Second && a.ui.touchKey == "smp"+fmt.Sprint(a.smp.envKnob) {
			hot = []int{0, 1, 3}[a.smp.envKnob]
			if a.smp.envSus {
				hot = 2
			}
		}
		for k, nd := range nodes {
			col, r := uint32(0xFFFFFF), 2.2
			if k == hot {
				col, r = knobCols[min(2, a.smp.envKnob)], 3
			}
			c.Circle(ex(math.Min(1, nd[0]/dur)), ey(nd[1]), r, col, 0.95)
		}
		if p := a.shaperPlaying(i); p >= 0 {
			px := ex(p)
			c.Line(px, y0+14, px, y0+h-10, 1.2, shLime, 0.9)
			c.Circle(px, y0+14, 2.2, shLime, 1)
		}
		c.Text(x0+10, y0+h-7, "0:00", gfx.Small, uDim, gfx.Left|gfx.Mid)
		info := fmt.Sprintf("%.2f S", dur)
		if bpm := a.proj.G["tempo"]; bpm > 0 {
			if beats := dur * bpm / 60; beats >= 0.9 {
				info += fmt.Sprintf("  ·  %.1f BEATS", beats)
			}
		}
		c.Text(x0+w-10, y0+h-7, info, gfx.Small, uDim, gfx.Right|gfx.Mid)
	}
	labels := [3]string{"ATTACK", "DECAY", "RELEASE"}
	vals := [3]string{msText(envAms(e.A)), msText(envDms(e.D)), msText(envRms(e.R))}
	norms := [3]float64{e.A, e.D, e.R}
	if a.shift {
		labels[1], vals[1], norms[1] = "SUSTAIN", fmt.Sprint(int(e.S*99+0.5)), e.S
	}
	act := -1
	if time.Since(a.ui.touchAt) < 2*time.Second && a.ui.touchKey == "smp"+fmt.Sprint(a.smp.envKnob) {
		act = a.smp.envKnob
	}
	mockKnobs(c, labels, vals, norms, act)
	a.uiBottom(c, "LEFT: ONE SHOT / GATE / LOOP  ·  SHIFT + K2: SUSTAIN")
}

// btnLegend: what the three buttons above the screen do (a lit chip = the one that's on)
func btnLegend(c *gfx.Canvas, labels [3]string, on int) {
	for i, l := range labels {
		if l == "" {
			continue
		}
		x := 54 + float64(i)*106
		w := gfx.Small.Width(l) + 14
		if i == on {
			c.RoundRect(x-w/2, 26, w, 12, 6, uLime, 1)
			c.Text(x, 32, l, gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else {
			c.RoundRectLine(x-w/2, 26, w, 12, 6, 1, uFaint, 1)
			c.Text(x, 32, l, gfx.Small, uDim, gfx.Center|gfx.Mid)
		}
	}
}

// envCol: the stage colour at t: ATTACK (K1), DECAY / SUSTAIN (K2), RELEASE (K3), blended across the joins
func envCol(e padEnv, t, off float64) uint32 {
	A, D := envAms(e.A)/1000, envDms(e.D)/1000
	R := envRms(e.R) / 1000
	switch {
	case t >= off:
		return gfx.Mix(knobCols[1], knobCols[2], math.Min(1, (t-off)/math.Max(0.004, R*0.3)))
	case t < A:
		return knobCols[0]
	case t < A+D:
		return gfx.Mix(knobCols[0], knobCols[1], math.Min(1, (t-A)/math.Max(0.004, D*0.5)))
	}
	return knobCols[1]
}
