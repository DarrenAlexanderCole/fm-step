package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/controls"
)

func writeTestTake(t *testing.T, path string, secs float64) {
	n := int(secs * 48000)
	b := make([]byte, 44+n*4)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n*4))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 192000)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*4))
	for i := 0; i < n; i++ { // the frame number in the left channel (mod 30000)
		binary.LittleEndian.PutUint16(b[44+4*i:], uint16(i%30000))
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMixtapeKnobsAndTrim(t *testing.T) {
	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.mt.view = true
	// before / while recording: K1 level, K2 pad, K3 comp -> engine, never the mix
	before := a.preset.P["harmonics"]
	a.ccKnown[16], a.lastCC[16] = true, 64
	a.onKnob(0, 72) // +8 ticks = +2 dB
	if a.preset.P["harmonics"] != before {
		t.Fatal("K1 on the recorder changed the sound")
	}
	if !strings.Contains(strings.Join(c.lines, ""), "MTGAIN 2.00") {
		t.Fatalf("no MTGAIN: %q", c.lines)
	}
	a.mtKnob(1, 6)
	a.mtKnob(2, 12)
	j := strings.Join(c.lines, "")
	if !strings.Contains(j, "MTGAIN -8.00") || !strings.Contains(j, "MTCOMP 2") {
		t.Fatalf("pad / comp: %q", c.lines)
	}
	// a finished take: K1 IN, K3 OUT, K2 loop; listening plays only that part
	tk := filepath.Join(t.TempDir(), "take 001.wav")
	writeTestTake(t, tk, 4)
	a.mt.ask, a.mt.path, a.mt.frames = true, tk, 4*48000
	a.mtKnob(0, 75) // 75 * 4 s / 300 = +1 s
	a.mtKnob(2, -75)
	if d := a.mt.trimIn - 1; d*d > 1e-6 || a.mtOut() < 2.99 || a.mtOut() > 3.01 {
		t.Fatalf("trim in %.3f out %.3f", a.mt.trimIn, a.mtOut())
	}
	a.proj.G["tempo"] = 120
	a.mtKnob(1, 6*4) // LOOP: 1/4, 1/2, 1 beat, 2 beats
	in, out, loop := a.mtLoopRange()
	if !loop || in != 1 || out-in < 0.99 || out-in > 1.01 { // 2 beats at 120 BPM
		t.Fatalf("loop %v %v %v", in, out, loop)
	}
	c.lines = nil
	a.mt.seekAt = time.Now().Add(-time.Second)
	a.mtTick()
	j = strings.Join(c.lines, "")
	if !strings.Contains(j, "MTLOOP 1.000 2.000 1") || !strings.Contains(j, "MTPLAY 1.00") ||
		strings.Index(j, "MTLOOP") > strings.Index(j, "MTPLAY") {
		t.Fatalf("listen: %q", c.lines)
	}
	if p := a.mtShownPos(2.5); p < 1.49 || p > 1.51 {
		t.Fatalf("shown position in the loop %.2f", p)
	}
	// DATA push: FINE
	a.onButton(controls.BtnDataPush, true)
	if !a.mt.fine {
		t.Fatal("no fine")
	}
	// SAVE writes IN-OUT
	a.mt.loopIdx = 0
	a.mtListenStop() // STOP: back to KEEP?
	a.mtScreen(controls.BtnScreenRight)
	for i := 0; i < 200 && a.mt.saving; i++ {
		time.Sleep(10 * time.Millisecond)
		a.mtTick()
	}
	if a.mt.ask || a.mt.saving {
		t.Fatal("not saved")
	}
	if s := wavSeconds(tk); s < 1.99 || s > 2.01 {
		t.Fatalf("trimmed take is %.3f s", s)
	}
	b, _ := os.ReadFile(tk)
	if v := binary.LittleEndian.Uint16(b[44:]); v != 48000%30000 {
		t.Fatalf("first frame %d, want frame 48000", v)
	}
	if pk, _ := os.ReadFile(peaksPath(tk)); len(pk) != 20 {
		t.Fatalf("peaks %d", len(pk))
	}
}

func TestSoloAndModels(t *testing.T) {
	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.mode = modeTracks
	a.shift = true
	a.onSeqPad(9, 100, true) // SHIFT + pad 10: solo T2
	a.shift = false
	j := strings.Join(c.lines, "")
	if !strings.Contains(j, "M 1 0") || !strings.Contains(j, "M 0 1") || !strings.Contains(j, "M 7 1") {
		t.Fatalf("solo: %q", c.lines)
	}
	if len(modelChoices) != 4 {
		t.Fatal("the MODEL knob should offer the 4 instruments")
	}
}
