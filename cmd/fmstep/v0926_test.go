package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/controls"
)

func TestUtil0926(t *testing.T) {
	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	p := a.pat()
	p.Len = 16
	p.S[0][3].On = true
	p.S[0][3].SetLock("cutoff", 0.3)
	shiftPad := func(i int) {
		a.shift = true
		a.onPad(i, 100, true)
		a.onPad(i, 0, false)
		a.shift = false
	}
	shiftPad(2) // DOUBLE SEQ
	if p.Len != 32 || !p.S[0][19].On || len(p.S[0][19].Locks) != 1 {
		t.Fatalf("double: len %d", p.Len)
	}
	shiftPad(1) // HALF SEQ
	if p.Len != 16 {
		t.Fatal("half")
	}
	shiftPad(15) // PROJECT menu: CLEAR AUTOMATION · TRACK (asks twice)
	if !a.util.menu {
		t.Fatal("menu")
	}
	a.proj.Track = 0
	for i := 0; i < 4; i++ {
		a.utilMenuData(1)
	}
	a.onButton(controls.BtnDataPush, true)
	if len(a.pat().S[0][3].Locks) != 1 {
		t.Fatal("cleared on the first push")
	}
	a.onButton(controls.BtnDataPush, true)
	if len(a.pat().S[0][3].Locks) != 0 || a.util.menu {
		t.Fatal("automation not cleared")
	}
	a.utilUndo()
	if len(a.pat().S[0][3].Locks) != 1 {
		t.Fatal("undo")
	}
	shiftPad(5) // HALF SPEED
	if a.proj.G["tempo"] != math.Round(94.0/2) {
		t.Fatalf("tempo %v", a.proj.G["tempo"])
	}
}

func TestRackAndLoop0926(t *testing.T) {
	m := map[string]float64{"send_a_fx": 1, "send_b_fx": 0.5}
	migrateRack(m)
	if fxIdx(m["rack_a"]) != 2 || fxIdx(m["rack_b"]) != 1 {
		t.Fatalf("migrate %v", m)
	}
	a := newTestApp(t)
	tk := filepath.Join(t.TempDir(), "take 001.wav")
	writeTestTake(t, tk, 8)
	a.mt.view, a.mt.ask, a.mt.path, a.mt.frames = true, true, tk, 8*48000
	a.mt.trimIn, a.mt.trimOut = 1, 6
	a.proj.G["tempo"] = 120
	a.mt.playPos = 3 * 48000
	a.mtKnob(1, 6*3) // LOOP 1 BEAT from the playhead
	la, lb, loop := a.mtLoopRange()
	if !loop || a.mt.trimIn != 1 || a.mt.trimOut != 6 || la < 0 || lb-la < 0.49 || lb-la > 0.51 {
		t.Fatalf("loop %v %v in %v out %v", la, lb, a.mt.trimIn, a.mt.trimOut)
	}
	a.mtMoveMarker(10) // slides the loop, not IN
	if a.mt.trimIn != 1 {
		t.Fatal("IN moved")
	}
	a.mtKnob(1, -6*3) // loop off
	a.mtSelect(false)
	a.mtSelect(false) // pressed again: IN locked
	if !a.mt.inLock {
		t.Fatal("lock")
	}
	a.mtMoveMarker(5)
	if a.mt.trimIn != 1 {
		t.Fatal("locked IN moved")
	}
	a.mt.dataAt = time.Now().Add(-10 * time.Millisecond)
	if a.dataAccel() < 4 {
		t.Fatal("no acceleration")
	}
}

func TestPads0927(t *testing.T) {
	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	dir := filepath.Join(sd, "FMSTEP", "samples", "breaks")
	writeTestTake(t, filepath.Join(dir, "amen.wav"), 4)
	a.onButton(controls.BtnChop, true) // CHOP: SHAPER, A01 empty: it asks
	if a.smp.view != smpShaper || a.smp.ask != 0 {
		t.Fatalf("view %d ask %d", a.smp.view, a.smp.ask)
	}
	a.onButton(controls.BtnScreenRight, true) // SAMPLE SELECT
	if a.smp.view != smpTape {
		t.Fatal("right: the card")
	}
	a.browseScan()
	for k, e := range a.pui.bents {
		if e.name == "breaks" {
			a.pui.bidx = k
		}
	}
	a.onButton(controls.BtnDataPush, true) // into the folder
	if filepath.Base(a.pui.bdir) != "breaks" || len(a.pui.bents) != 2 {
		t.Fatalf("folder %s %+v", a.pui.bdir, a.pui.bents)
	}
	a.pui.bidx = 1
	a.onPad(4, 100, true) // the target pad: A05
	a.onPad(4, 0, false)
	a.smpKnob(2, 30) // TAKE less than all
	c.lines = nil
	a.onButton(controls.BtnDataPush, true) // onto A05
	j := strings.Join(c.lines, "")
	if !strings.Contains(j, "PLOAD 4 0.0000 ") || !strings.Contains(j, "amen.wav") || a.smp.view != smpShaper {
		t.Fatalf("load: %q view %d", c.lines, a.smp.view)
	}
	a.dsp.pads.parse("PLOADED 4 96000")
	a.padsTick()
	if a.pslot(4).frames != 96000 {
		t.Fatal("loaded")
	}
	c.lines = nil
	a.onPad(4, 100, true)
	a.onPad(4, 0, false)
	if j := strings.Join(c.lines, ""); !strings.Contains(j, "PN 4 0.787") || !strings.Contains(j, "PO 4") {
		t.Fatalf("play %q", c.lines)
	}
	a.smpKnob(0, 40) // ATTACK
	a.onButton(controls.BtnScreenLeft, true)
	if a.pslot(4).Env.Mode != 1 || a.pslot(4).Env.A <= 0 {
		t.Fatal("env")
	}
	a.onButton(controls.BtnChop, true) // TRIM
	a.onButton(controls.BtnScreenRight, true)
	a.pui.dataAt = time.Time{}
	a.smpData(127)
	if a.pslot(4).Out >= 1 {
		t.Fatal("trim OUT")
	}
	a.padsSave(true)
	b := newTestApp(t)
	b.user = a.user
	b.padsInit()
	if s := b.pslot(4); s.Path == "" || s.Env.Mode != 1 || s.Out >= 1 {
		t.Fatalf("kit not kept: %+v", s)
	}
	// SHIFT + push: chop over bank A
	a.samplerOpen(smpTape)
	a.pui.bidx = 1
	a.shift = true
	a.onButton(controls.BtnDataPush, true)
	a.shift = false
	if a.pslot(15).Path == "" || a.pslot(15).Start <= 0 || a.smp.view != smpBanks {
		t.Fatalf("chop: %+v", a.pslot(15))
	}
	// ERASE + pad twice empties it
	a.sq.eraseHeld = true
	a.onPad(15, 100, true)
	a.onPad(15, 100, true)
	a.sq.eraseHeld = false
	if !a.pslot(15).empty() {
		t.Fatal("erase")
	}
}

// FMSTEP_MOCK=dir: the 0.9.27 screens as built
func TestShots0927(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	wav := filepath.Join(sd, "FMSTEP", "samples", "breaks", "break 09.wav")
	writeBreak(t, wav, 2.4)
	writeBreak(t, filepath.Join(sd, "FMSTEP", "samples", "breaks", "amen 01.wav"), 6)
	os.MkdirAll(filepath.Join(sd, "FMSTEP", "samples", "jungle"), 0755)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	a.padsInit()
	for p := 0; p < 16; p++ {
		a.padAssign(p, wav, float64(p)*0.15, 0.15)
		a.pslot(p).frames = 7200
	}
	a.padAssign(4, wav, 0, 2.4)
	a.pslot(4).frames = 2.4 * 48000
	a.pslot(4).Env = padEnv{A: 0.45, D: 0.55, S: 0.5, R: 0.45}
	for p := 16; p < 27; p++ {
		a.padAssign(p, wav, 0, 0.3)
	}
	shot := func(name string, f func()) {
		f()
		a.draw()
		savePNG(a, filepath.Join(dir, name+".png"))
	}
	shot("1 ENV", func() {
		a.samplerOpen(smpShaper)
		a.smp.sel, a.smp.ask, a.pui.page = 4, -1, pageEnv
		a.ui.touchKey, a.ui.touchAt, a.smp.envKnob, a.smp.acc2 = "smp0", time.Now(), 0, 0
	})
	shot("2 TRIM", func() {
		a.pui.page = pageTrim
		s := a.pslot(4)
		s.In, s.Out = 0.18, 0.62
		a.pui.trimOut, a.pui.trimSet, a.pui.inLock = true, true, true
		a.pui.zoom = 1
	})
	shot("3 TUNE", func() {
		a.pui.page = pageTune
		a.pslot(4).Pitch, a.pslot(4).Fine = -5, 12
	})
	shot("4 EMPTY", func() { a.smp.ask = 9; a.pads.bank = 1 })
	a.pads.bank = 0
	shot("5 SAMPLE SELECT", func() {
		a.samplerOpen(smpTape)
		a.pui.bdir = filepath.Join(sd, "FMSTEP", "samples", "breaks")
		a.browseScan()
		a.pui.bidx = 1
		e, _ := a.browseSel()
		a.browseFile(e)
		a.pui.bstart, a.pui.bwin = 1.2, 2.5
	})
	shot("6 BANKS", func() { a.samplerOpen(smpBanks) })
	shot("7 SAMPLE IN", func() {
		a.samplerOpen(smpRecIn)
		a.pads.recOn, a.pads.recStart = true, time.Now().Add(-6*time.Second)
	})
	a.pads.recOn = false
	a.smpLeave()
	shot("8 RACK MASTER", func() {
		a.setMode(modeRack)
		a.ui.rackSlot = 3
		a.char["mc_on"], a.char["lim_on"] = 1, 1
		a.dsp.pads.mgr, a.dsp.pads.mgrAt = [2]float64{0.6, 0.85}, time.Now()
	})
	shot("9 MIXER", func() {
		a.setMode(modeTracks)
		a.ui.mixer = true
		a.proj.Tracks[2].Sound.P["delay_send"] = 0.5
		a.proj.Tracks[4].Sound.P["reverb_send"] = 0.6
	})
	shot("10 TAPE FX", func() {
		a.setMode(modePlay)
		a.mt.view, a.mt.compPage, a.mt.fxStage = true, true, 1
		a.char["mtfx_eq_on"], a.char["mtfx_mid"], a.char["mtfx_air"] = 1, 0.6, 0.65
	})
	a.mt.view, a.mt.compPage = false, false
	for i := 0; i < 12; i++ {
		a.ui.hits = append(a.ui.hits, hit{track: i % 8, note: 40 + i*3, vel: 0.8, at: time.Now().Add(-time.Duration(i*200) * time.Millisecond)})
	}
	for b := range a.ui.spec.show {
		a.ui.spec.show[b] = -20 - float64(b)
	}
	a.ui.spec.at = time.Now()
	shot("11 MELT", func() {
		a.ui.scope = 4
		for i := 0; i < 30; i++ {
			a.vis.at = time.Now().Add(-50 * time.Millisecond)
			a.drawMeltV2(a.scr.C)
		}
	})
	shot("12 COLLAGE", func() { a.ui.scope = 5 })
}

// a break-like WAV (16-bit stereo 48 kHz)
func writeBreak(t *testing.T, path string, secs float64) {
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
	for i := 0; i < n; i++ {
		tt := float64(i) / 48000
		ph := math.Mod(tt*4, 1)
		v := math.Exp(-ph*9)*math.Sin(tt*2*math.Pi*(60+200*math.Exp(-ph*30))) + 0.15*math.Exp(-math.Mod(tt*8, 1)*20)*math.Sin(float64(i)*1.3)
		s := int16(v * 20000)
		binary.LittleEndian.PutUint16(b[44+4*i:], uint16(s))
		binary.LittleEndian.PutUint16(b[46+4*i:], uint16(s))
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, b, 0644)
}
