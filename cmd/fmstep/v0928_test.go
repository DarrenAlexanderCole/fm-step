package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/seq"
)

func TestSixteenTracks(t *testing.T) {
	// an 8-track project (0.9.27) loads with 9-16 as empty sample tracks
	old := seq.Default()
	b, _ := json.Marshal(old)
	var m map[string]any
	json.Unmarshal(b, &m)
	m["tracks"] = m["tracks"].([]any)[:8]
	b, _ = json.Marshal(m)
	var p seq.Project
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Tracks[10].Sound.Name != "BANK C" || p.Tracks[10].Note != 36 || p.Tracks[3].Sound.Name != old.Tracks[3].Sound.Name {
		t.Fatalf("migration: %q %d", p.Tracks[10].Sound.Name, p.Tracks[10].Note)
	}
	p.Tracks[12].PadMute = 1 << 3
	b2, _ := json.Marshal(&p)
	var q seq.Project
	json.Unmarshal(b2, &q)
	if q.Tracks[12].PadMute != 8 {
		t.Fatal("pad mutes not kept")
	}
	if cmds := strings.Join(q.Commands(), "\n"); !strings.Contains(cmds, "PMUTE 67 1") || !strings.Contains(cmds, "M 15 0") {
		t.Fatal("commands for 16 tracks / pad mutes")
	}

	a := newTestApp(t)
	c := &capture{}
	a.dsp = &dsp{in: c}
	// SEQ: TRACKS -> STEP -> TRACKS ; KNOB FX: MIXER -> RACK -> MIXER
	a.onButton(controls.BtnSeq, true)
	if a.mode != modeTracks || a.ui.mixer {
		t.Fatal("SEQ: TRACKS")
	}
	a.onButton(controls.BtnSeq, true)
	if a.mode != modeStep {
		t.Fatal("SEQ again: STEP EDIT")
	}
	a.onButton(controls.BtnSeq, true)
	if a.mode != modeTracks {
		t.Fatal("SEQ again: TRACKS")
	}
	a.onButton(controls.BtnKnobFX, true)
	if a.mode != modeTracks || !a.ui.mixer {
		t.Fatal("KNOB FX: MIXER")
	}
	a.onPad(10, 100, true) // pad 11: track 11 (bank C)
	if a.proj.Track != 10 || a.pads.bank != 2 {
		t.Fatalf("pad 11 picks track 11: %d bank %d", a.proj.Track, a.pads.bank)
	}
	a.onButton(controls.BtnKnobFX, true)
	if a.mode != modeRack {
		t.Fatal("KNOB FX again: RACK")
	}
	a.onButton(controls.BtnKnobFX, true)
	if a.mode != modeTracks || !a.ui.mixer {
		t.Fatal("KNOB FX again: MIXER")
	}
	// SAMPLE on a sample track: the SHAPER; on a synth track: the pages
	a.onButton(controls.BtnSample, true)
	if a.smp.view != smpShaper {
		t.Fatal("SAMPLE on track 11: SHAPER")
	}
	a.smpLeave()
	a.selectTrack(2)
	a.onButton(controls.BtnSample, true)
	if a.mode != modePlay || a.smp.view != smpOff {
		t.Fatal("SAMPLE on track 3: the pages")
	}
	// the pads on a sample track's play view play its bank (note 36 + pad)
	a.selectTrack(9)
	c.lines = nil
	a.onPad(4, 100, true)
	if !strings.Contains(strings.Join(c.lines, ""), "N 40 ") {
		t.Fatalf("sample track pad: %q", c.lines)
	}
	a.onPad(4, 0, false)
	// MUTE (0.9.29): track mutes -> track solos -> pad mutes -> pad solos -> track mutes
	a.onButton(controls.BtnMute, true)
	a.onPad(13, 100, true)
	if !a.proj.Tracks[13].Muted {
		t.Fatal("track mute 14")
	}
	a.onButton(controls.BtnMute, true)
	a.onPad(0, 100, true)
	if !a.sq.solo[0] {
		t.Fatal("solo 1")
	}
	a.onButton(controls.BtnMute, true)
	c.lines = nil
	a.onPad(3, 100, true) // pad 4 of track 10 (bank B, the selected sample track)
	if a.proj.Tracks[9].PadMute != 8 || !strings.Contains(strings.Join(c.lines, ""), "PMUTE 19 1") {
		t.Fatalf("pad mute: %d %q", a.proj.Tracks[9].PadMute, c.lines)
	}
	a.onButton(controls.BtnMute, true)
	c.lines = nil
	a.onPad(6, 100, true)
	if a.sq.padSolo[9] != 64 || !strings.Contains(strings.Join(c.lines, ""), "PSOLO 22 1") {
		t.Fatalf("pad solo: %d %q", a.sq.padSolo[9], c.lines)
	}
	a.onButton(controls.BtnMute, true)
	if a.sq.muteMode != muteTracks {
		t.Fatal("MUTE wraps to TRACK MUTES")
	}
	a.shift = true
	a.onButton(controls.BtnMute, true)
	a.shift = false
	if a.proj.Tracks[13].Muted || a.proj.Tracks[9].PadMute != 0 || a.sq.solo[0] || a.sq.padSolo[9] != 0 {
		t.Fatal("SHIFT + MUTE clears")
	}
	// the sampler records onto its bank's track
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	wav := filepath.Join(sd, "x.wav")
	writeBreak(t, wav, 1)
	a.pads.bank = 3
	a.padAssign(a.slotIdx(5), wav, 0, 1)
	a.samplerOpen(smpBanks)
	a.sq.recArm, a.sq.playing = true, true
	c.lines = nil
	a.onPad(5, 100, true)
	a.onPad(5, 0, false)
	a.sq.recArm, a.sq.playing = false, false
	if j := strings.Join(c.lines, ""); !strings.Contains(j, "U 11\nR 41 ") || !strings.Contains(j, "U 11\nr 41\n") {
		t.Fatalf("pad record onto track 12: %q", c.lines)
	}
	if a.proj.Tracks[11].Note != 41 {
		t.Fatal("step note on the sample track")
	}
}

func TestKnobPickup(t *testing.T) {
	a := testApp()
	a.pageIdx = pageIndex("SOUND") // K1 = harmonics
	a.preset.P["harmonics"] = 0.5
	a.onKnob(0, 120) // first report: where the knob is
	a.onKnob(0, 110) // far above 0.5, moving down, not there yet
	if a.preset.P["harmonics"] != 0.5 {
		t.Fatal("moved before picking up")
	}
	a.onKnob(0, 60) // passes 0.5: picked up, follows the knob
	if v := a.preset.P["harmonics"]; v < 0.46 || v > 0.48 {
		t.Fatalf("after pick-up %v", v)
	}
	a.onKnob(0, 0)
	if a.preset.P["harmonics"] > 0.001 {
		t.Fatal("not the whole way down")
	}
	a.onKnob(0, 127)
	if a.preset.P["harmonics"] < 0.999 {
		t.Fatal("not the whole way up")
	}
}

// FMSTEP_MOCK=dir: 0.9.28 screens as built
func TestShots0928(t *testing.T) {
	dir := mockDir(t)
	a := newTestApp(t)
	a.sq.playing, a.sq.posPat, a.sq.posStep = true, 0, 5
	sd := t.TempDir()
	t.Setenv("FMSTEP_SD", sd)
	wav := filepath.Join(sd, "b.wav")
	writeBreak(t, wav, 2)
	a.padsInit()
	for p := 0; p < 12; p++ {
		a.padAssign(32+p, wav, float64(p)*0.15, 0.15)
	}
	a.proj.Tracks[13].Muted = true
	a.proj.Tracks[10].PadMute = 1<<3 | 1<<9
	shot := func(name string, f func()) {
		f()
		a.draw()
		savePNG(a, filepath.Join(dir, name+".png"))
	}
	shot("1 TRACKS", func() { a.setMode(modeTracks); a.ui.mixer = false; a.selectTrack(10) })
	shot("2 MIXER 1-8", func() { a.ui.mixer = true; a.selectTrack(4) })
	shot("3 MIXER 9-16", func() { a.selectTrack(10) })
	shot("4 MUTE tracks", func() { a.sq.muteMode = muteTracks })
	shot("5 MUTE pads", func() { a.sq.muteMode = mutePads })
	shot("6 SOLO", func() { a.sq.muteMode = muteSolo; a.sq.solo[4] = true })
	a.sq.muteMode, a.sq.solo[4] = muteOff, false
	shot("7 SAMPLE TRACK", func() { a.setMode(modePlay); a.ui.scope = 0 })
	shot("8 PICKUP", func() {
		a.samplerOpen(smpShaper)
		a.smp.sel, a.smp.ask, a.pui.page = 2, -1, pageTune
		a.pslot(34).Pitch = -5
		a.kp[0] = knobPickState{key: "x", picked: false, pos: 0.8, n: 0.4, at: time.Now()}
	})
	a.smpLeave()
	a.kp[0] = knobPickState{}
	for i := 0; i < 20; i++ {
		a.ui.hits = append(a.ui.hits, hit{track: i % 16, note: 40 + i, vel: 0.8, at: time.Now()})
	}
	for b := range a.ui.spec.show {
		a.ui.spec.show[b] = -10 - float64(b)*0.8
	}
	a.ui.spec.at = time.Now()
	a.selectTrack(4)
	shot("9 MELT", func() {
		a.setMode(modePlay)
		a.ui.scope = 4
		for i := 0; i < 40; i++ {
			a.vis.at = time.Now().Add(-40 * time.Millisecond)
			a.drawMeltV2(a.scr.C)
		}
	})
	shot("10 COLLAGE", func() {
		a.ui.scope = 5
		for i := 0; i < 25; i++ {
			a.vis.at = time.Now().Add(-40 * time.Millisecond)
			a.drawCollageV2(a.scr.C)
		}
	})
	_ = os.Getenv
}
