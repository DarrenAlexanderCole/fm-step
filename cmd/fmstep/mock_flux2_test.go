package main

// Sampler view tests. FMSTEP_MOCK=dir also renders the live TAPE / CHOP /
// SLICE screens (from simulated engine data) and the GRID / ROLL designs.

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/panel"
	"fmstep/internal/seq"
)

func newTestApp(t *testing.T) *app {
	a := &app{scr: panel.NewMemScreen(320, 240), held: map[int]int{}, user: t.TempDir(), dsp: &dsp{in: nopWC{io.Discard}}}
	a.proj = seq.Default()
	a.proj.Track = 4
	a.preset = &a.proj.Tracks[4].Sound
	a.char = map[string]float64{}
	for k, v := range charDefaults {
		a.char[k] = v
	}
	a.tourDone = true
	return a
}

// a level byte stream like the engine's: kicks on the beat, hats between
func fakeLevels(n int, perBeat float64, ph float64) [][2]uint8 {
	out := make([][2]uint8, n)
	for i := range out {
		b := float64(i)/perBeat + ph
		f := math.Mod(b, 1)
		lo := 0.35 + 0.6*math.Exp(-f*3)
		hi := 0.3 + 0.5*math.Exp(-math.Mod(b*2, 1)*6)
		out[i] = [2]uint8{uint8(lo * 255), uint8(hi * 255)}
	}
	return out
}

func TestSmpParse(t *testing.T) {
	var d smpData
	logf := func(string, ...any) {}
	gw := strings.Repeat("ff80", 256)
	for _, l := range []string{"GRAB 245106 2 94.00 1", "GW " + gw, "GT 3 0 1000 5000", "GRABEMPTY", "TW 0102a0b0", "GRABWAIT 1200", "GZ 10 500 " + strings.Repeat("4020", 128)} {
		if !d.parse(l, logf) {
			t.Fatalf("not parsed: %.20s", l)
		}
	}
	if d.parse("POS 1 2", logf) {
		t.Fatal("POS taken by the sampler parser")
	}
	if d.grabLen != 245106 || d.grabBars != 2 || d.grabSeq != 1 || len(d.gt) != 3 || d.gt[2] != 5000 || d.msg != "GRABEMPTY" || d.gw[3] != [2]uint8{255, 128} || len(d.tw) != 2 || d.tw[1] != 0xa0b0 || d.waitMs != 1200 || d.gzEnd != 500 || d.gz[0] != [2]uint8{0x40, 0x20} {
		t.Fatalf("parsed wrong: %+v", d.tw)
	}
}

func TestSmpSlices(t *testing.T) {
	a := newTestApp(t)
	a.smpInit()
	n := 245106
	s0, e0, _, r0 := a.smpSlice(0, n)
	s15, e15, _, _ := a.smpSlice(15, n)
	if s0 != 0 || r0 || e15 != n || e0 != n/16 || s15 != n*15/16 {
		t.Fatalf("16 even: %d %d %d %d", s0, e0, s15, e15)
	}
	a.dsp.smp.grabLen = n
	a.smp.chop = chop8 // 8: pads 9-16 = the same 8 reversed
	s9, e9, _, r9 := a.smpSlice(9, n)
	s1, e1, _, r1 := a.smpSlice(1, n)
	if s9 != s1 || e9 != e1 || !r9 || r1 {
		t.Fatalf("8 mode: pad 10 should be pad 2 reversed")
	}
	a.smp.edits[0] = sliceEdit{startFr: -2880, endFr: 2880, pitch: 3}
	s, e, p, _ := a.smpSlice(0, n)
	if s != 0 || e != n/8+60*48 || p != 3 {
		t.Fatalf("trims: %d %d %d", s, e, p)
	}
}

func TestMockFlux2(t *testing.T) {
	dir := os.Getenv("FMSTEP_MOCK")
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 6
	a.proj.G["tempo"] = 94
	// engine data: a rolling tape and a 2-bar grab
	perBar := 4 * 60 / 94.0 * 48000 / 1024
	tw := fakeLevels(int(perBar*6), perBar/4, 0)
	for _, p := range tw {
		a.dsp.smp.tw = append(a.dsp.smp.tw, uint16(p[0])<<8|uint16(p[1]))
	}
	copy(a.dsp.smp.gw[:], fakeLevels(256, 32, 0))
	for i := range a.dsp.smp.gwf { // fine peaks: a hit every 1/16, decaying, with small hats between
		ph := float64(i%128) / 128
		v := 255 * math.Exp(-ph*6)
		if i%32 == 16 {
			v = math.Max(v, 150)
		}
		a.dsp.smp.gwf[i] = uint8(math.Max(40, v))
	}
	a.dsp.smp.grabLen, a.dsp.smp.grabBars, a.dsp.smp.grabBpm, a.dsp.smp.grabSeq = 245106, 2, 94, 1
	shot := func(name string) {
		a.draw()
		if dir == "" {
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				v := a.scr.RGB(x, y)
				img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
			}
		}
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, img)
		f.Close()
	}
	a.smpEnter(smpTape)
	shot("1-TAPE")
	a.smpEnter(smpChop)
	a.smpSync(true)
	a.smp.hitAt[4], a.smp.held[4], a.smp.sel = time.Now().Add(-60*time.Millisecond), true, 4
	a.smp.press[4] = 0.6
	shot("2-CHOP")
	a.smp.nrHeld, a.smp.spin = true, 0.7
	shot("2b-CHOP-spin")
	a.smp.nrHeld = false
	a.smp.edits[4] = sliceEdit{startFr: 384, endFr: -576, pitch: -2}
	a.smpEnter(smpSlice)
	copy(a.dsp.smp.gz[:], fakeLevels(128, 64, 0.1))
	s, e, _, _ := a.smpSlice(4, a.dsp.smp.grabLen)
	a.dsp.smp.gzStart, a.dsp.smp.gzEnd = s, e
	shot("3-SLICE")
	// CHOP on BEATS (11 hits), recording armed
	a.smp.chop = chopBeats
	a.dsp.smp.gt = []int{0, 15000, 30000, 46000, 61000, 76000, 92000, 120000, 150000, 180000, 210000}
	a.sq.recArm = true
	a.smpEnter(smpChop)
	shot("6-CHOP-beats")
	a.sq.recArm = false
	// SLICE in MANUAL
	a.smpToMan(a.dsp.smp.grabLen)
	a.smp.sel = 3
	a.smpEnter(smpSlice)
	shot("7-SLICE-manual")
	// a kit of 5 sounds
	a.dsp.smp.grabHow = 3
	a.dsp.smp.kit = [][2]int{{0, 30000}, {31000, 50000}, {51000, 60000}, {61000, 90000}, {91000, 110000}}
	a.dsp.smp.grabLen = 111000
	a.smp.srcName = "dusty kit"
	a.smp.chop = chop16
	a.smpEnter(smpChop)
	shot("8-CHOP-kit")
	// the card browser
	a.smp.files = []cardEntry{{name: "amen break", info: "4.2 S"}, {name: "chop 001", info: "5.2 S"}, {name: "rhodes loop 92", info: "10.4 S"}, {name: "vinyl crackle", info: "8.0 S"}, {name: "dusty kit", kit: true, info: "KIT · 12"}, {name: "808 kit", kit: true, info: "KIT · 16"}}
	files := a.smp.files
	a.smpEnter(smpTape)
	a.smp.files, a.smp.fileIdx, a.smp.scanned = files, 2, time.Now()
	shot("9-CARD")
	// the grid editor designs
	for i, f := range []func(c *gfx.Canvas){func(c *gfx.Canvas) { mock2Grid(a, c) }, func(c *gfx.Canvas) { mock2Roll(a, c) }} {
		a.smpLeave()
		c := a.scr.C
		c.Clear(uBG)
		a.uiHeader(c)
		f(c)
		if dir != "" {
			img := image.NewRGBA(image.Rect(0, 0, 320, 240))
			for y := 0; y < 240; y++ {
				for x := 0; x < 320; x++ {
					v := a.scr.RGB(x, y)
					img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
				}
			}
			fo, _ := os.Create(filepath.Join(dir, fmt.Sprintf("%d-%s.png", 4+i, []string{"GRID", "ROLL"}[i])))
			png.Encode(fo, img)
			fo.Close()
		}
	}
}
