package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/controls"
)

func noLog(string, ...any) {}

// TestMixtape: a tap starts a take, a tap stops it, the engine's answer
// brings up DELETE · LISTEN · SAVE; SAVE puts it at the top of the LIBRARY
// and PAD FX loads 16 s from where K1 says.
func TestMixtape(t *testing.T) {
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	a := newTestApp(t)
	out := &bufWC{}
	a.dsp = &dsp{in: out}
	dir := a.mixtapeDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "take 006.wav"), []byte("x"), 0644)
	tap := func() { a.onButton(controls.BtnSampleRecord, true); a.onButton(controls.BtnSampleRecord, false) }
	tap()
	want := "MTREC " + filepath.Join(dir, "take 007.wav")
	if !a.mt.rec || !a.mt.view || !strings.Contains(out.String(), want) {
		t.Fatalf("start: rec=%t view=%t %q", a.mt.rec, a.mt.view, out.String())
	}
	a.dsp.mt.parse("MTR 2880000 0.5 0.4 0", noLog)
	a.mtTick()
	if a.mt.frames != 2880000 {
		t.Fatalf("frames %d", a.mt.frames)
	}
	a.draw()
	a.onButton(controls.BtnScreenMiddle, true) // pause the take
	if !a.mt.paused || !strings.HasSuffix(out.String(), "MTPAUSE 1\n") {
		t.Fatal("rec pause")
	}
	a.draw()
	a.onButton(controls.BtnScreenMiddle, true)
	if a.mt.paused || !strings.HasSuffix(out.String(), "MTPAUSE 0\n") {
		t.Fatal("rec resume")
	}
	// a held SAMPLE RECORD with a pad (saving) is not a tap
	a.onButton(controls.BtnSampleRecord, true)
	a.onPad(0, 100, true)
	a.onPad(0, 0, false)
	a.onButton(controls.BtnSampleRecord, false)
	if !a.mt.rec {
		t.Fatal("SAMPLE RECORD + pad stopped the take")
	}
	tap()
	if !strings.Contains(out.String(), "MTSTOP") || !a.mt.stopping {
		t.Fatal("stop")
	}
	// the engine finishes: write a real take so the LIBRARY can read it
	path := filepath.Join(dir, "take 007.wav")
	writeTestWav(t, path, 48000*60)
	os.MkdirAll(filepath.Join(dir, ".peaks"), 0755)
	os.WriteFile(peaksPath(path), make([]byte, 600), 0644)
	a.dsp.mt.parse("MTDONE 2880000 "+path, noLog)
	a.mtTick()
	if !a.mt.ask || a.mt.rec || a.mt.stopping {
		t.Fatalf("ask=%t rec=%t", a.mt.ask, a.mt.rec)
	}
	a.draw()
	a.onButton(controls.BtnScreenMiddle, true) // LISTEN
	if !strings.Contains(out.String(), "MTPLAY 0.00 "+path) || !a.mt.playing {
		t.Fatal("listen")
	}
	// listening: the transport
	a.dsp.mt.parse("MTP 480000 2880000", noLog)
	a.mt.playFrom = time.Time{}
	a.mtTick()
	a.onButton(controls.BtnScreenRight, true) // +5 s
	a.onButton(controls.BtnScreenRight, true) // +5 s
	if !a.mt.seekPending || a.mtPos() != 20 {
		t.Fatalf("nudge: %v", a.mtPos())
	}
	a.onButton(controls.BtnScreenMiddle, true) // pause
	if !a.mt.lpaused || !strings.HasSuffix(out.String(), "MTPPAUSE 1\n") {
		t.Fatal("pause")
	}
	a.mt.seekAt = time.Now().Add(-time.Second)
	a.mtTick() // the jump goes out once the pressing settles, still paused
	if !strings.HasSuffix(out.String(), "MTPLAY 20.00 "+path+"\nMTPPAUSE 1\n") {
		t.Fatalf("seek: %q", out.String()[len(out.String())-90:])
	}
	a.mtDataTurn(127) // jog back 1 s
	if a.mtPos() != 19 {
		t.Fatalf("jog %v", a.mtPos())
	}
	a.onButton(controls.BtnStop, true) // STOP: done listening, back to the question
	if a.mt.playing || !a.mt.ask || !strings.HasSuffix(out.String(), "MTPLAYSTOP\n") {
		t.Fatal("stop listening")
	}
	a.onButton(controls.BtnScreenRight, true) // SAVE
	if a.mt.ask || a.mt.playing {
		t.Fatal("save")
	}
	a.onButton(controls.BtnPadFX, true) // the LIBRARY, the take selected
	a.onButton(controls.BtnPadFX, false)
	if a.mt.view || a.smp.view != smpTape {
		t.Fatalf("library: mt.view=%t smp=%d", a.mt.view, a.smp.view)
	}
	e, ok := a.smpSelected()
	if !ok || !e.take || e.path != path || e.info != "1:00" {
		t.Fatalf("selected %+v (files %+v)", e, a.smp.files)
	}
	for i := 0; i < 10; i++ {
		a.smpKnob(0, 4) // fast: seconds
	}
	a.draw()
	a.onButton(controls.BtnPadFX, true)
	if !strings.Contains(out.String(), "LOADAT 40.000 "+path) {
		t.Fatalf("loadat: %q", out.String())
	}
	// DELETE needs two presses
	a.mtShow(true)
	a.mt.ask = true
	a.onButton(controls.BtnScreenLeft, true)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted on the first press")
	}
	a.onButton(controls.BtnScreenLeft, true)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("not deleted")
	}
	// a too-short take is thrown away
	tap()
	p8 := filepath.Join(dir, "take 007.wav")
	if !strings.Contains(out.String(), "MTREC "+p8) {
		t.Fatalf("number reuse: %q", out.String()[len(out.String())-80:])
	}
	tap()
	writeTestWav(t, p8, 1000)
	a.dsp.mt.parse("MTDONE 1000 "+p8, noLog)
	a.mtTick()
	if _, err := os.Stat(p8); err == nil || a.mt.ask {
		t.Fatal("short take kept")
	}
	// LV lines feed the mixer dots
	a.mtShow(false)
	a.mode, a.ui.mixer = modeTracks, true
	a.mtTick()
	if !a.mt.lvOn || !strings.Contains(out.String(), "LEVELS 1\n") {
		t.Fatal("levels on")
	}
	a.mode = modePlay
	a.mtTick()
	if !strings.HasSuffix(out.String(), "LEVELS 0\n") {
		t.Fatal("levels off")
	}
	a.mode = modeTracks
	a.mtTick()
	a.dsp.mt.parse("LV ff80400000000000f0", noLog)
	if l, _ := a.mixLevel(0); l < 0.99 {
		t.Fatalf("level %v", l)
	}
	time.Sleep(10 * time.Millisecond)
	a.draw()
}

func writeTestWav(t *testing.T, path string, frames int) {
	b := make([]byte, 44+frames*4)
	copy(b, "RIFF")
	put32 := func(o, v int) { b[o], b[o+1], b[o+2], b[o+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }
	put32(4, 36+frames*4)
	copy(b[8:], "WAVEfmt ")
	put32(16, 16)
	b[20], b[22] = 1, 2
	put32(24, 48000)
	put32(28, 192000)
	b[32], b[34] = 4, 16
	copy(b[36:], "data")
	put32(40, frames*4)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}
