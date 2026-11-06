package main

// The knob curve (0.9.25): K1, K2 and K3 as one line, and its shading.

import (
	"math"

	"fmstep/internal/gfx"
)

const (
	cvTop, cvBot = 42.0, 136.0
	cvX0, cvX1   = 12.0, 308.0
)

var cvKnobX = [3]float64{62, 160, 258}

// cvY: the Catmull-Rom curve through the three knob levels (0..1).
func cvY(lv [3]float64) func(x float64) float64 {
	Y := func(n float64) float64 { return cvBot - (cvBot-cvTop)*n }
	pts := []float64{Y(lv[0]), Y(lv[0]), Y(lv[1]), Y(lv[2]), Y(lv[2])}
	px := []float64{-28, cvKnobX[0], cvKnobX[1], cvKnobX[2], 348}
	return func(x float64) float64 {
		i := 0
		for i < len(px)-2 && x > px[i+1] {
			i++
		}
		t := (x - px[i]) / (px[i+1] - px[i])
		p0, p1, p2 := pts[max(0, i-1)], pts[i], pts[i+1]
		p3 := pts[min(len(pts)-1, i+2)]
		y := 0.5 * (2*p1 + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t*t + (-p0+3*p1-3*p2+p3)*t*t*t)
		return math.Max(cvTop+1, math.Min(cvBot-1, y))
	}
}

// cvW: how much each knob owns x (soft, sums to 1).
func cvW(x float64) [3]float64 {
	var w [3]float64
	sum := 0.0
	for k := 0; k < 3; k++ {
		d := (x - cvKnobX[k]) / 60
		w[k] = math.Exp(-d * d)
		sum += w[k]
	}
	for k := range w {
		w[k] /= sum
	}
	return w
}

func cvCol(x float64) uint32 {
	w := cvW(x)
	return gfx.Mix(gfx.Mix(knobCols[0], knobCols[1], w[1]/(w[0]+w[1]+1e-9)), knobCols[2], w[2])
}

func cvGlow(x float64, gl [3]float64) float64 {
	w := cvW(x)
	return w[0]*gl[0] + w[1]*gl[1] + w[2]*gl[2]
}

// curveShade: DOTS 40% · LAYERS 20% · FLOW LINES 20% · JUICE 20%.
func curveShade(c *gfx.Canvas, lv, gl [3]float64, juice, t float64) {
	y := cvY(lv)
	// JUICE (20%): the fill swells with the sound
	for x := cvX0; x < cvX1; x++ {
		yy := y(x + 0.5)
		tide := juice * 6 * (0.6 + 0.4*math.Sin(x/17+t*1.3))
		c.Line(x+0.5, math.Max(cvTop, yy-tide*0.4), x+0.5, cvBot-1, 1, cvCol(x), 0.03+0.07*juice+0.05*cvGlow(x, gl))
	}
	// LAYERS (20%): two sheets a little lower, for depth
	for l := 1; l <= 2; l++ {
		off := float64(l) * 10
		for x := cvX0; x < cvX1; x += 2 {
			yy := y(x+1) + off
			if yy < cvBot-1 {
				c.Line(x+1, yy, x+1, cvBot-1, 2, cvCol(x), 0.035)
			}
		}
	}
	// FLOW LINES (20%): contour lines under the curve, drifting slowly
	for k := 1; k <= 4; k++ {
		ph := float64(k)*12 + 3*math.Sin(t*0.4+float64(k))
		px, py := -1.0, 0.0
		for x := cvX0; x < cvX1; x += 3 {
			yy := y(x) + ph + 2*math.Sin(x/23+float64(k)+t*0.6)
			if yy < cvBot-3 && px >= 0 && py < cvBot-3 {
				c.Line(px, py, x, yy, 0.8, cvCol(x), 0.28*math.Pow(0.7, float64(k)))
			}
			px, py = x, yy
		}
	}
	// DOTS (40%): the panel's dot grid lights up under the curve, brightest at the edge
	for gy := cvTop + 5; gy < cvBot-2; gy += 6 {
		for gx := cvX0 + 4; gx < cvX1-2; gx += 6 {
			d := gy - y(gx)
			if d < 0 {
				continue
			}
			e := math.Max(0.2, 1-d/70)
			g := cvGlow(gx, gl)
			c.Circle(gx, gy, 0.6+0.9*e+0.3*juice, cvCol(gx), 0.22+0.4*e+0.3*g)
		}
	}
	// the curve itself and the three knob nodes
	py := y(cvX0)
	for x := cvX0 + 1; x < cvX1; x++ {
		yy := y(x)
		c.Line(x-1, py, x, yy, 1.5, cvCol(x), 0.7+0.3*cvGlow(x, gl))
		py = yy
	}
	for k := 0; k < 3; k++ {
		yy := y(cvKnobX[k])
		if gl[k] > 0.3 {
			c.Glow(cvKnobX[k], yy, 10+8*juice, knobCols[k], 0.3*gl[k])
		}
		c.Circle(cvKnobX[k], yy, 2.6+gl[k], knobCols[k], 1)
	}
}
