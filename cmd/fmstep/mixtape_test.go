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
	// 0.9.29 trimming: DATA PUSH picks OUT, DATA moves it (the engine follows, no restart)
	a.onButton(controls.BtnDataPush, true)
	if !a.mt.selOut {
		t.Fatal("OUT not picked")
	}
	plays := strings.Count(out.String(), "MTPLAY")
	for i := 0; i < 30; i++ {
		a.mt.dataAt = time.Time{} // slow clicks (0.9.26: fast ones go further)
		a.mtDataTurn(127)         // 30 clicks back, 1 s each in a 60 s view
	}
	if o := a.mtOut(); o < 29.9 || o > 30.1 {
		t.Fatalf("OUT %v", o)
	}
	if !strings.Contains(out.String(), "MTLOOP 0.000 30.000 0") || strings.Count(out.String(), "MTPLAY") != plays {
		t.Fatalf("moving OUT should update the engine without restarting: %q", out.String()[len(out.String())-120:])
	}
	a.onButton(controls.BtnScreenMiddle, true) // pause
	if !a.mt.lpaused || !strings.HasSuffix(out.String(), "MTPPAUSE 1\n") {
		t.Fatal("pause")
	}
	a.onButton(controls.BtnStop, true) // STOP: done listening, back to the question
	if a.mt.playing || !a.mt.ask || !strings.HasSuffix(out.String(), "MTPLAYSTOP\n") {
		t.Fatal("stop listening")
	}
	// 0.9.29: the question stays when another view button is pressed
	a.onButton(controls.BtnSeq, true)
	if !a.mt.view || !a.mt.ask {
		t.Fatal("the KEEP question went away")
	}
	a.onButton(controls.BtnScreenRight, true) // KEEP (trimmed to 0-30 s)
	for i := 0; i < 300 && a.mt.saving; i++ {
		time.Sleep(10 * time.Millisecond)
		a.mtTick()
	}
	if a.mt.ask || a.mt.playing {
		t.Fatal("save")
	}
	if s := wavSeconds(path); s < 29.9 || s > 30.1 {
		t.Fatalf("saved take is %.2f s", s)
	}
	// 0.9.27: SAMPLE SELECT browses the card; a take goes onto a pad like any WAV
	a.onButton(controls.BtnSampleSelect, true)
	a.onButton(controls.BtnSampleSelect, false)
	if a.mt.view || a.smp.view != smpTape {
		t.Fatalf("browser: mt.view=%t smp=%d", a.mt.view, a.smp.view)
	}
	a.pui.bdir = dir
	a.browseScan()
	for k, e := range a.pui.bents {
		if e.path == path {
			a.pui.bidx = k
		}
	}
	if e, ok := a.browseSel(); !ok || e.path != path {
		t.Fatalf("take not listed: %+v", a.pui.bents)
	}
	a.smpKnob(0, 4) // START
	a.draw()
	a.onButton(controls.BtnDataPush, true)
	if !strings.Contains(out.String(), "PLOAD 0 ") || !strings.Contains(out.String(), path) {
		t.Fatalf("pload: %q", out.String()[len(out.String())-200:])
	}
	a.smpLeave()
	// DELETE: ERASE twice
	a.mtShow(true)
	a.mt.ask = true
	a.onButton(controls.BtnErase, true)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted on the first press")
	}
	a.onButton(controls.BtnErase, true)
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
