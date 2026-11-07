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
	"fmt"
	"math"

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

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

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

const (
	shBlue = 0x2F7BFF
	shLine = 0x7CC4FF
	shDeep = 0x5A3CFF
	shPink = 0xFF3FA4
	shLime = 0xD8FF4A
)

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
