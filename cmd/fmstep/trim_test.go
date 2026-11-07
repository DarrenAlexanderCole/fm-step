package main

import (
	"fmt"
	"math"
	"strings"
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

func fmtSscan(s string, a, b *int) (int, error) { return fmtSscanf(s, "%d %d", a, b) }

var fmtSscanf = fmt.Sscanf
