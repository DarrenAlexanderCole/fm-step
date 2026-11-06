package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngine answers GZM from a synthetic break, like the engine would.
func fakeGZM(a *app, w []float64, out *bufWC) {
	s := out.String()
	i := strings.LastIndex(s, "GZM ")
	if i < 0 {
		return
	}
	var st, en int
	if _, err := fmtSscan(s[i+4:], &st, &en); err != nil {
		return
	}
	d := &a.dsp.smp
	d.gzmStart, d.gzmEnd = st, en
	for c := 0; c < trimCols; c++ {
		a0, b0 := st+(en-st)*c/trimCols, max(st+(en-st)*c/trimCols+1, st+(en-st)*(c+1)/trimCols)
		lo, hi := 1.0, -1.0
		for k := a0; k < b0 && k < len(w); k++ {
			lo, hi = math.Min(lo, w[k]), math.Max(hi, w[k])
		}
		if hi < lo {
			lo, hi = 0, 0
		}
		d.gzm[c] = [2]int8{int8(lo * 127), int8(hi * 127)}
	}
}

func TestTrim(t *testing.T) {
	a := newTestApp(t)
	out := &bufWC{}
	a.dsp = &dsp{in: out}
	a.smpInit()
	n := 4 * 48000
	w := breakWave(n, 4, 3)
	a.dsp.smp.grabLen, a.dsp.smp.grabSeq = n, 1
	a.dsp.smp.gw = [256][2]uint8{}
	for i := 0; i < 256; i++ {
		v := uint8(math.Min(255, 400*math.Abs(w[i*n/256])))
		a.dsp.smp.gw[i] = [2]uint8{v, v / 2}
	}
	a.smp.grabSeq = 1
	a.smp.view = smpSlice
	a.smp.sel = 4
	shots := os.Getenv("FMSTEP_MOCK")
	shot := func(name string) {
		a.drawV2()
		fakeGZM(a, w, out)
		a.drawV2()
		if shots != "" {
			savePNG(a, filepath.Join(shots, name+".png"))
		}
	}
	shot("SLICE 1 the slice")
	s0, e0, _, _ := a.smpSlice(4, n)
	if s0 != n*4/16 || e0 != n*5/16 {
		t.Fatalf("slice %d %d", s0, e0)
	}
	if !strings.Contains(out.String(), "GZM ") {
		t.Fatal("no GZM asked")
	}
	for k := 0; k < 20; k++ { // zoom in
		a.smpKnob(2, 2)
	}
	_, W := a.trimWindow(n, s0, e0)
	if W >= n/16 {
		t.Fatalf("zoom W=%d", W)
	}
	a.smpKnob(0, 1) // slow: one pixel
	s1, _, _, _ := a.smpSlice(4, n)
	if d := s1 - s0; d < 1 || d > W/trimCols+1 {
		t.Fatalf("slow step %d (W %d)", d, W)
	}
	shot("SLICE 2 zoomed on IN")
	a.smpKnob(0, -5) // fast: further
	s2, _, _, _ := a.smpSlice(4, n)
	if s1-s2 <= 5*W/trimCols {
		t.Fatalf("fast step %d", s1-s2)
	}
	for k := 0; k < 30; k++ {
		a.smpKnob(2, 3)
	}
	_, W = a.trimWindow(n, s2, e0)
	if W != trimCols {
		t.Fatalf("max zoom %d", W)
	}
	shot("SLICE 3 one sample per pixel")
	a.smpKnob(1, -1) // OUT, the view follows it
	if a.smp.focus != 1 {
		t.Fatal("focus")
	}
	a.smpKnob(2, -40)
	a.smpKnob(2, -40)
	shot("SLICE 4 OUT, zoomed out")
	a.shift = true
	a.smpKnob(0, 8)
	a.smpKnob(1, 8)
	shot("SLICE 5 SHIFT pitch reverse")
	a.shift = false
	if e := a.smp.edits[4]; e.pitch != 1 || !e.rev {
		t.Fatalf("shift edits %+v", e)
	}
	a.smpPreview(false)
	a.smpPreview(true)
	if !strings.Contains(out.String(), "SL 4 1 0 0") || !strings.Contains(out.String(), "SLC 31 ") || !strings.HasSuffix(out.String(), "SL 31 1 0 0\n") {
		t.Fatalf("previews: %q", out.String()[len(out.String())-80:])
	}
	a.smpData(1) // DATA: next slice, view resets
	shot("SLICE 6 next slice")
	if a.smp.sel != 5 || a.smp.zoomW != 0 {
		t.Fatalf("sel %d zoom %d", a.smp.sel, a.smp.zoomW)
	}
}

func fmtSscan(s string, a, b *int) (int, error) { return fmtSscanf(s, "%d %d", a, b) }

var fmtSscanf = fmt.Sscanf
