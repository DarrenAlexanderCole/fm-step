package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/controls"
)

// a grab of 2 s with 16 even slices and an 8-hit break in the fine peaks
func fakeGrab(a *app) {
	a.smpInit()
	d := &a.dsp.smp
	d.grabLen, d.grabHow, d.grabSeq = 96000*2, 2, 1
	for p := 0; p < 2048; p++ {
		ph := math.Mod(float64(p)/2048*64, 1)
		v := math.Exp(-ph*4.5)*0.85 + 0.12*math.Exp(-ph*1.5) + 0.05*math.Sin(float64(p)*1.7)
		d.gwf[p] = uint8(math.Max(0, math.Min(1, v)) * 255)
	}
	a.smp.grabSeq = 1
	a.smp.chop = chop16
	a.smp.srcName = "BREAK 09"
}

func TestShaper0926(t *testing.T) {
	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	a.onButton(controls.BtnChop, true) // CHOP: the SHAPER, nothing loaded: the pad asks
	if a.smp.view != smpShaper || a.smp.ask != 0 {
		t.Fatalf("view %d ask %d", a.smp.view, a.smp.ask)
	}
	a.onButton(controls.BtnScreenRight, true) // SAMPLE SELECT
	if a.smp.view != smpTape {
		t.Fatal("right button should open the card")
	}
	fakeGrab(a)
	a.shaperOpen()
	a.onPad(4, 100, true)
	a.onPad(4, 0, false)
	if a.smp.sel != 4 || a.smp.ask != -1 {
		t.Fatalf("sel %d ask %d", a.smp.sel, a.smp.ask)
	}
	c.lines = nil
	a.shaperKnob(0, 40) // attack
	a.onButton(controls.BtnScreenLeft, true)
	j := strings.Join(c.lines, "")
	if !strings.Contains(j, "SLENV 4 ") || !strings.HasSuffix(strings.TrimSpace(j), " 1") {
		t.Fatalf("env: %q", c.lines)
	}
	a.shaperSave(true)
	b := newTestApp(t)
	b.user = a.user
	b.shaperLoad()
	if b.smp.env[4].Mode != 1 || b.smp.env[4].A <= 0 {
		t.Fatalf("not kept: %+v", b.smp.env[4])
	}
}

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

// FMSTEP_MOCK=dir: the 0.9.26 screens as built
func TestShots0926(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 22
	fakeGrab(a)
	shot := func(name string, f func()) {
		f()
		a.draw()
		savePNG(a, filepath.Join(dir, name+".png"))
	}
	a.smp.env[4] = padEnv{A: 0.45, D: 0.55, S: 0.5, R: 0.45, Mode: 0}
	shot("1 SHAPER", func() {
		a.shaperOpen()
		a.smp.sel, a.smp.ask = 4, -1
		a.ui.touchKey, a.ui.touchAt, a.smp.envKnob = "smp0", time.Now(), 0
		a.smp.hitAt[4] = time.Now().Add(-500 * time.Millisecond)
		a.smp.held[4] = true
	})
	shot("2 SHAPER gate SHIFT", func() {
		a.smp.env[4].Mode = 1
		a.shift = true
		a.util.shiftAt = time.Now()
		a.smp.envKnob, a.smp.envSus = 1, true
		a.ui.touchKey = "smp1"
	})
	a.shift = false
	shot("3 SHAPER empty pad", func() { a.smp.sel, a.smp.ask = 9, 9 })
	a.smpLeave()
	shot("4 SHIFT sheet", func() {
		a.shift, a.util.shiftUsed = true, false
		a.util.shiftAt = time.Now().Add(-time.Second)
		a.util.fullLevel = true
	})
	a.shift = false
	shot("5 PROJECT menu", func() {
		a.util.menu, a.util.menuIdx = true, 4
		a.util.ask, a.util.askAt = projectMenu[4].label, time.Now()
	})
	a.util.menu = false
	shot("6 COMP FX", func() {
		a.mt.view, a.mt.compPage, a.mt.monitor = true, true, true
		a.char["mt_comp"], a.char["mt_senda"] = 2.0/3, 0.35
		a.dsp.mt.gr, a.dsp.mt.grAt = 0.5, time.Now()
	})
	a.mt.view, a.mt.compPage = false, false
	shot("7 RACK phaser", func() {
		a.setMode(modeRack)
		a.char["rack_b"] = 0.8
		a.ui.rackSlot = 1
	})
	tk := filepath.Join(t.TempDir(), "take 001.wav")
	writeTestTake(t, tk, 8)
	shot("8 MIXTAPE loop lock", func() {
		a.setMode(modePlay)
		a.mt.view, a.mt.ask, a.mt.path, a.mt.frames, a.mt.name = true, true, tk, 8*48000, "TAKE 001"
		a.mt.trimIn, a.mt.trimOut, a.mt.inLock, a.mt.selOutSet = 1, 7, true, true
		a.mt.loopIdx, a.mt.loopA = 4, 3.2
		a.proj.G["tempo"] = 120
	})
	fmt.Println("shots in", dir)
}
