package main

// 0.9.29 SHAPES mock: FMSTEP_MOCK=dir go test ./cmd/fmstep -run TestMockShapes

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestMockShapes(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	bits := make([]shapeBit, 5)
	for i := range bits {
		bits[i] = shapeBit{y: shapesFloor, s: 14}
	}
	// a groove: kick (sub) on beats, snare (low mids) on 2 and 4, hats (highs) on 8ths
	band := func(tm float64) [5]float64 {
		beat := math.Mod(tm*2, 1)
		var e [5]float64
		e[0] = 0.9 * math.Exp(-beat*6)
		e[1] = 0.55*math.Exp(-beat*4) + 0.15
		if int(tm*2)%2 == 1 {
			e[2] = 0.8 * math.Exp(-beat*7)
		}
		e[3] = 0.35 + 0.1*math.Sin(tm*3)
		e[4] = 0.6 * math.Exp(-math.Mod(tm*4, 1)*9)
		return e
	}
	snaps := map[int]string{25: "SHAPES 1 on the kick", 37: "SHAPES 2 on the snare", 58: "SHAPES 3 between hits", 400: "SHAPES 4 silence"}
	for f := 0; f <= 400; f++ {
		tm := float64(f) * 0.02
		e := band(tm)
		if f > 200 {
			e = [5]float64{}
		}
		for i := range bits {
			shapesStep(&bits[i], e[i], 0.02)
		}
		if name, ok := snaps[f]; ok {
			c := a.scr.C
			c.Clear(uBG)
			a.statusAt = time.Time{}
			a.uiHeader(c)
			drawShapes(c, bits)
			a.uiBottom(c, "ONE SHAPE PER BAND  ·  LOWS LEFT, HIGHS RIGHT  ·  16 LEVELS: NEXT")
			savePNG(a, filepath.Join(dir, name+".png"))
		}
	}
}
