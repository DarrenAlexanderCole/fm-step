package main

import (
	"bytes"
	"fmstep/internal/controls"
	"os"
	"strings"
	"testing"
)

type bufWC struct{ bytes.Buffer }

func (b *bufWC) Close() error { return nil }

// TestSmpCommands drives the sampler controls and checks what goes to the
// engine. FMSTEP_CMDS=file also writes the commands (for an engine render).
func TestSmpCommands(t *testing.T) {
	a := newTestApp(t)
	out := &bufWC{}
	a.dsp = &dsp{in: out}
	mark := func(s string) { out.WriteString("#" + s + "\n") }
	a.smpButton(controls.BtnChop, true) // PAD FX: the LIBRARY
	a.smpButton(controls.BtnChop, false)
	mark("W 0.1")
	a.smp.grabbing = true // as if PAD FX had loaded a file
	mark("W 3.0")
	// the engine answers GRAB + GW
	a.dsp.smp.parse("GRAB 245106 2 94.00 2", func(string, ...any) {})
	a.dsp.smp.parse("GW "+strings.Repeat("c0a0", 256), func(string, ...any) {})
	a.dsp.smp.parse("GT 4 0 50000 100000 150000", func(string, ...any) {})
	a.smpSync(false) // slices sent, view goes to CHOP
	if a.smp.view != smpChop {
		t.Fatalf("view %d after grab", a.smp.view)
	}
	mark("W 0.2")
	for _, p := range []int{0, 4, 2, 6} {
		a.smpPad(p, 120, true)
		mark("W 0.32")
		a.smpPad(p, 0, false)
	}
	// recording: SEQ REC + PLAY puts the hits on the chop track (T8)
	a.sq.recArm, a.sq.playing = true, true
	a.smpPad(3, 100, true)
	mark("W 0.2")
	a.smpPad(3, 0, false)
	a.sq.recArm, a.sq.playing = false, false
	a.smp.motion = 1 // gate + pressure
	a.smpCfg()
	a.smpPad(8, 127, true)
	for k := 0; k <= 10; k++ {
		a.onPressure(8, byte(k*12))
		mark("W 0.03")
	}
	a.smpPad(8, 0, false)
	mark("W 0.3")
	for k := 0; k <= 40; k++ { // scrub
		a.smpFader(k*3, k*3-3)
		mark("W 0.02")
	}
	a.smpButton(7, true) // SEQ: leave
	s := out.String()
	for _, want := range []string{"SLC 0 0 15319 0 0", "CHOPTRK 7", "CHOPCFG 0 0.400", "SCRUB 1", "N 36 0.945 7", "O 36 7",
		"U 7\nR 39 0.787\nU 4", "U 7\nr 39\nU 4", "CHOPCFG 1 0.400", "SLP 8", "SCRUBPOS", "O 44 7", "SCRUB 0"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if f := os.Getenv("FMSTEP_CMDS"); f != "" {
		os.WriteFile(f, []byte(strings.ReplaceAll(s, "#", "")), 0644)
	}
}
