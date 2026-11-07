package main

// 0.9.28: 16 tracks. 1-8 synths, 9-16 sample tracks (pad banks A-H).
//
//   SEQ          TRACKS (the 16 as the pads sit) → STEP EDIT of the selected track → TRACKS
//   KNOB FX      MIXER (8 strips a page, the selected track's page) → RACK → MIXER
//   SAMPLE       the selected track's sound: synth parameters (1-8) or the SHAPER (9-16)
//   MUTE         TRACK MUTES → PAD MUTES (a sample track's 16 pads) → SOLO → off
//                SHIFT + MUTE: all mutes, pad mutes and solos cleared

import (
	"fmt"
	"strings"
	"time"

	"fmstep/internal/gfx"
	"fmstep/internal/seq"
)

// the pad grid: pad i's box (pads 1-4 the bottom row)
func padBox(i int, y0 float64) (x, y, w, h float64) {
	col, row := i%4, 3-i/4
	return 10 + float64(col)*76, y0 + float64(row)*38, 74, 36
}

func (a *app) trackName(t int) string {
	if seq.IsSample(t) {
		n := 0
		for p := 0; p < 16; p++ {
			if !a.pads.s[(t-8)*16+p].empty() {
				n++
			}
		}
		return fmt.Sprintf("BANK %s · %d", bankName(t-8), n)
	}
	n := strings.ToUpper(a.proj.Tracks[t].Sound.Name)
	if len(n) > 11 {
		n = n[:11]
	}
	return n
}

// TRACKS: 16 boxes as the pads sit; a strip of the pattern's first 16 steps in each
func (a *app) drawTrackNotes(c *gfx.Canvas) {
	pi := a.proj.Sel
	if a.sq.playing && a.sq.posPat >= 0 {
		pi = a.sq.posPat
	}
	p := &a.proj.Pat[pi]
	c.Text(10, 33, "TRACKS", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(96, 34, fmt.Sprintf("1-8 SYNTHS  ·  9-16 SAMPLES  ·  P%d", pi+1), gfx.Small, uDim, gfx.Left|gfx.Mid)
	now := time.Now()
	for t := 0; t < seq.Tracks; t++ {
		x, y, w, h := padBox(t, 46)
		tr := &a.proj.Tracks[t]
		col := tcol(t)
		glass(c, x, y, w, h, 6, col)
		if t == a.proj.Track {
			c.RoundRectLine(x-2, y-2, w+4, h+4, 7, 1.2, uText, 0.85)
		}
		if hit := now.Sub(a.sq.flash[t]); hit < 300*time.Millisecond && !tr.Muted {
			c.RoundRect(x, y, w, h, 6, col, 0.25*(1-float64(hit)/float64(300*time.Millisecond)))
		}
		chip := col
		if tr.Muted {
			chip = uFaint
		}
		c.Circle(x+10, y+10, 5.5, chip, 1)
		c.Text(x+10, y+10.5, fmt.Sprint(t+1), gfx.Small, uBG, gfx.Center|gfx.Mid)
		nc := uint32(uText)
		if tr.Muted {
			nc = uDim
		}
		c.Text(x+6, y+27, a.trackName(t), gfx.Small, nc, gfx.Left|gfx.Mid)
		if tr.Muted {
			c.Text(x+w-4, y+27, "M", gfx.Small, uRed, gfx.Right|gfx.Mid)
		} else if a.sq.solo[t] {
			c.Text(x+w-4, y+27, "S", gfx.Small, shLime, gfx.Right|gfx.Mid)
		}
		for s := 0; s < 16 && s < p.Len; s++ {
			on := p.S[t][s].On
			al := 0.15
			if on {
				al = 0.9
			}
			if a.sq.playing && a.sq.posPat == pi && a.sq.posStep%16 == s {
				al = 1
				if !on {
					c.RoundRect(x+20+float64(s)*3.2, y+6, 2.4, 8, 1, uText, 0.6)
					continue
				}
			}
			c.RoundRect(x+20+float64(s)*3.2, y+7, 2.4, 6, 1, col, al)
		}
	}
	if a.sq.playing {
		a.animate(now.Add(120 * time.Millisecond))
	}
	a.uiBottom(c, fmt.Sprintf("PADS: PICK THE TRACK  ·  SEQ AGAIN: STEP EDIT OF TRACK %d", a.proj.Track+1))
}

// ---------------------------------------------------------------- MUTE

const (
	muteOff = iota
	muteTracks
	mutePads
	muteSolo
)

// the sample track whose pads PAD MUTES shows
func (a *app) muteSampleTrack() int {
	if seq.IsSample(a.proj.Track) {
		return a.proj.Track
	}
	return 8 + a.pads.bank
}

func (a *app) muteButton() {
	if a.shift {
		for t := range a.proj.Tracks {
			a.proj.Tracks[t].Muted = false
			a.proj.Tracks[t].PadMute = 0
			a.sq.solo[t] = false
		}
		a.sendMutes()
		a.sendPadMutes()
		a.sq.projDirty = true
		a.say("ALL MUTES, PAD MUTES AND SOLOS CLEARED")
	} else {
		a.sq.muteMode = (a.sq.muteMode + 1) % 4
		a.say([]string{"MUTES OFF", "TRACK MUTES  ·  PADS 1-16", fmt.Sprintf("PAD MUTES  ·  TRACK %d (BANK %s)", a.muteSampleTrack()+1, bankName(a.muteSampleTrack()-8)), "SOLO  ·  PADS 1-16"}[a.sq.muteMode])
	}
	a.paintPads()
	a.dirty = true
}

func (a *app) sendPadMutes() {
	for t := 8; t < seq.Tracks; t++ {
		for p := 0; p < 16; p++ {
			a.dsp.send("PMUTE %d %d", (t-8)*16+p, (a.proj.Tracks[t].PadMute>>p)&1)
		}
	}
}

// mutePad: a pad pressed while MUTE is on
func (a *app) mutePad(idx int) {
	switch a.sq.muteMode {
	case muteTracks:
		a.toggleMute(idx)
	case muteSolo:
		a.toggleSolo(idx)
	case mutePads:
		t := a.muteSampleTrack()
		tr := &a.proj.Tracks[t]
		tr.PadMute ^= 1 << idx
		on := (tr.PadMute>>idx)&1 == 1
		a.dsp.send("PMUTE %d %d", (t-8)*16+idx, b2i(on))
		a.sq.projDirty = true
		a.say(fmt.Sprintf("%s%02d %s", bankName(t-8), idx+1, map[bool]string{true: "MUTED IN THE SEQUENCE", false: "ON"}[on]))
	}
	a.paintPads()
	a.dirty = true
}

func (a *app) paintMutePads() {
	for i := 0; i < 16; i++ {
		var on bool
		c := trackColours[i]
		switch a.sq.muteMode {
		case muteTracks:
			on = !a.proj.Tracks[i].Muted
		case muteSolo:
			on = a.sq.solo[i]
		case mutePads:
			t := a.muteSampleTrack()
			c = trackColours[t]
			on = (a.proj.Tracks[t].PadMute>>i)&1 == 0 && !a.pads.s[(t-8)*16+i].empty()
		}
		if on {
			a.padColour(i, c[0], c[1], c[2])
		} else {
			r, g, b := scale7(c, 0.06)
			a.padColour(i, r, g, b)
		}
	}
}

func (a *app) drawMutes(c *gfx.Canvas) {
	page := a.sq.muteMode - 1
	c.Text(10, 33, "MUTE", gfx.Big, uText, gfx.Left|gfx.Mid)
	segTabs(c, 312, 33, []string{"TRACK MUTES", "PAD MUTES", "SOLO"}, page, uText)
	st := a.muteSampleTrack()
	for i := 0; i < 16; i++ {
		x, y, w, h := padBox(i, 46)
		col := tcol(i)
		name := a.trackName(i)
		on := true
		switch a.sq.muteMode {
		case muteTracks:
			on = !a.proj.Tracks[i].Muted
		case muteSolo:
			on = a.sq.solo[i]
		case mutePads:
			col = tcol(st)
			slot := (st-8)*16 + i
			name = slotName(slot)
			if s := a.pads.s[slot]; !s.empty() && s.Name != "" {
				name += " " + s.Name
			}
			if len(name) > 13 {
				name = name[:13]
			}
			on = (a.proj.Tracks[st].PadMute>>i)&1 == 0
		}
		al := 0.9
		if !on {
			al = 0.18
		}
		glass(c, x, y, w, h, 6, col)
		c.RoundRect(x+5, y+6, 8, 8, 2, col, al)
		c.Text(x+70, y+10, fmt.Sprint(i+1), gfx.Small, uDim, gfx.Right|gfx.Mid)
		tc := uint32(uText)
		if !on {
			tc = uDim
		}
		c.Text(x+6, y+26, name, gfx.Small, tc, gfx.Left|gfx.Mid)
		switch {
		case a.sq.muteMode == muteTracks && !on:
			c.Text(x+70, y+26, "MUTE", gfx.Small, uRed, gfx.Right|gfx.Mid)
		case a.sq.muteMode == muteSolo && on:
			c.Text(x+70, y+26, "SOLO", gfx.Small, shLime, gfx.Right|gfx.Mid)
		case a.sq.muteMode == mutePads && !on:
			c.Text(x+70, y+26, "MUTE", gfx.Small, uRed, gfx.Right|gfx.Mid)
		}
	}
	hint := []string{"PADS: MUTE A TRACK  ·  MUTE: PAD MUTES",
		fmt.Sprintf("PADS: MUTE A PAD OF TRACK %d (BANK %s)  ·  MUTE: SOLO", st+1, bankName(st-8)),
		"PADS: SOLO  ·  MUTE: DONE  ·  SHIFT + MUTE: ALL CLEAR"}[page]
	a.uiBottom(c, hint)
}

// a sample track on the play view: its mixer knobs (SAMPLE opens the SHAPER)
func (a *app) drawSampleTrack(c *gfx.Canvas) {
	t := a.proj.Track
	c.Text(10, 33, fmt.Sprintf("TRACK %d", t+1), gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(110, 34, "SAMPLES · BANK "+bankName(t-8), gfx.Small, uDim, gfx.Left|gfx.Mid)
	for p := 0; p < 16; p++ {
		x, y, w, h := padBox(p, 46)
		h = 30
		y = 46 + float64(3-p/4)*33
		s := &a.pads.s[(t-8)*16+p]
		glass(c, x, y, w, h, 5, tcol(t))
		c.Text(x+5, y+8, fmt.Sprintf("%02d", p+1), gfx.Small, uDim, gfx.Left|gfx.Mid)
		if s.empty() {
			continue
		}
		if pk := a.padPeaks((t-8)*16 + p); len(pk) > 0 {
			for k := 0; k < 60; k++ {
				v := pk[k*len(pk)/60]
				c.Line(x+8+float64(k), y+19-v*7, x+8+float64(k), y+19+v*7, 1, tcol(t), 0.8)
			}
		}
	}
	ps := [3]*param{&mixParams[0], &pSendALvl, &pSendBLvl}
	var labels, vals [3]string
	var norms [3]float64
	for k, p := range ps {
		v := a.val(p.key)
		labels[k], vals[k], norms[k] = p.label, p.show(v), p.norm(v)
	}
	kk, _ := a.turning()
	mockKnobs(c, labels, vals, norms, kk)
	a.uiBottom(c, "SAMPLE: THE SHAPER  ·  PADS PLAY THE BANK  ·  SHIFT + K1: PAN")
}
