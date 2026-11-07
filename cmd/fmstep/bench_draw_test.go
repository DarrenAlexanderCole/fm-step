package main

import (
	"testing"
	"time"
)

func TestDrawCost(t *testing.T) {
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 5
	views := []struct {
		name string
		set  func()
	}{
		{"rack", func() { a.setMode(modeRack) }},
		{"params", func() { a.setMode(modePlay); a.ui.scope = 0 }},
		{"patterns", func() { a.setMode(modePatterns) }},
		{"steps", func() { a.setMode(modeStep) }},
		{"mixer", func() { a.setMode(modeTracks); a.ui.mixer = true }},
		{"rack", func() { a.setMode(modeRack) }},
		{"melt", func() { a.setMode(modePlay); a.ui.scope = 4 }},
		{"collage", func() { a.setMode(modePlay); a.ui.scope = 5 }},
		{"dots", func() { a.setMode(modePlay); a.ui.scope = 3 }},
	}
	for _, v := range views {
		v.set()
		a.draw()
		t0 := time.Now()
		for i := 0; i < 20; i++ {
			a.draw()
		}
		t.Logf("%-9s %.2f ms", v.name, float64(time.Since(t0).Microseconds())/20/1000)
	}
}
