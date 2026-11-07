package main

// FM-STEP 0.9.5 screens for the remaining views: tracks (mixer), patterns,
// performance effects, projects, sounds and the exit confirmation. Same
// language as the parameter pages: big title, rounded panels, track colours,
// dot-matrix numbers, instructions in the bottom pill.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/gfx"
	"fmstep/internal/seq"
	"fmstep/internal/sound"
)

// viewTitle: the big title row used by every view.
func viewTitle(c *gfx.Canvas, name, right string, rightCol uint32) {
	c.Text(10, 31, name, gfx.Big, uText, gfx.Left|gfx.Mid)
	if right != "" {
		c.Text(310, 32, right, gfx.Label, rightCol, gfx.Right|gfx.Mid)
	}
}

// padTile gives the box for pad i (0..15) laid out like the pads: pad 1 at
// the bottom left, four rows of four.
func padTile(i int) (x, y, w, h float64) {
	col, row := i%4, 3-i/4
	return 8 + float64(col)*77, 42 + float64(row)*41, 73, 37
}

// ------------------------------------------------------------------ tracks

// drawTracksV2: a mixer. One channel strip per track: number, sound, model,
// a level fader in the track colour, pan, mute, and a light that flashes
// when the track plays.
func (a *app) drawTracksV2(c *gfx.Canvas) {
	if !a.ui.mixer {
		a.drawTrackNotes(c)
		return
	}
	viewTitle(c, "MIXER", "", uDim)
	// 0.9.25: which send K3 sets (PUSH DATA flips it), named by the effect it feeds
	// 0.9.27: K2 = send A, K3 = send B, named by the RACK effect each feeds
	c.Circle(170, 32, 3, knobCols[1], 1)
	c.Text(177, 32.5, "A "+a.sendFx(0), gfx.Small, uText, gfx.Left|gfx.Mid)
	c.Circle(246, 32, 3, knobCols[2], 1)
	c.Text(253, 32.5, "B "+a.sendFx(1), gfx.Small, uText, gfx.Left|gfx.Mid)
	now := time.Now()
	// 0.9.28: 16 tracks, 8 a page: the page of the selected track (pads 1-16 pick)
	page := a.proj.Track / 8
	c.Text(80, 33.5, []string{"1-8", "9-16"}[page], gfx.Small, uDim, gfx.Left|gfx.Mid)
	for i := 0; i < 8; i++ {
		t := page*8 + i
		tr := &a.proj.Tracks[t]
		col := tcol(t)
		x := 6 + float64(i)*38.5
		cx := x + 17.5
		sel := t == a.proj.Track
		bg := uint32(uPanel)
		if sel {
			bg = uPanel2
		}
		c.RoundRect(x, 41, 35, 172, 8, bg, 1)
		if sel {
			c.RoundRectLine(x, 41, 35, 172, 8, 1.4, col, 1)
		}
		// number chip + activity light
		hit := now.Sub(a.sq.flash[t])
		glow := 0.0
		if hit < 350*time.Millisecond && !tr.Muted {
			glow = 1 - float64(hit)/float64(350*time.Millisecond)
		}
		if glow > 0 {
			c.Glow(cx, 54, 16, col, 0.6*glow)
		}
		chip := col
		if tr.Muted {
			chip = uFaint
		}
		c.Circle(cx, 54, 8, chip, 1)
		c.Text(cx, 54.5, fmt.Sprintf("%d", t+1), gfx.Label, uBG, gfx.Center|gfx.Mid)
		// sound name (two short lines) and model
		name := strings.ToUpper(tr.Sound.Name)
		l1, l2 := splitName(name, 6)
		nc := uint32(uText)
		if tr.Muted {
			nc = uDim
		}
		c.Text(cx, 71, l1, gfx.Small, nc, gfx.Center|gfx.Mid)
		if l2 != "" {
			c.Text(cx, 80, l2, gfx.Small, nc, gfx.Center|gfx.Mid)
		}
		eng := int(sound.Complete(tr.Sound).P["engine"])
		if seq.IsSample(t) {
			c.Text(cx, 90, "PADS "+bankName(t-8), gfx.Small, uFaint, gfx.Center|gfx.Mid)
		} else if eng >= 0 && eng < len(engineNames) {
			m := strings.ToUpper(engineNames[eng])
			if len(m) > 7 {
				m = m[:7]
			}
			c.Text(cx, 90, m, gfx.Small, uFaint, gfx.Center|gfx.Mid)
		}
		// level fader
		lv := tr.Sound.P["level"]
		if _, ok := tr.Sound.P["level"]; !ok {
			lv = 0.8
		}
		const fy0, fy1 = 99.0, 166.0
		// level dots: what the track plays right now, the white dot = its peak
		lvl, pk := a.mixLevel(t)
		const rows = 10
		for r := 0; r < rows; r++ {
			y := fy1 - 2 - float64(r)*(fy1-fy0-4)/(rows-1)
			f := float64(r+1) / rows
			for _, dx := range []float64{-11, -5} {
				switch {
				case f <= lvl+0.001 && lvl > 0.02:
					dc := col
					if f > 0.85 {
						dc = uOrange
					}
					if tr.Muted {
						dc = uFaint
					}
					c.Circle(cx+dx, y, 2.1, dc, 0.55+0.45*f)
				case pk > 0.05 && math.Abs(f-pk) < 0.5/rows:
					c.Circle(cx+dx, y, 2.1, uText, 0.9)
				default:
					c.Circle(cx+dx, y, 0.9, uLine, 1)
				}
			}
		}
		// the level fader, slim, to the right of the dots
		fx := cx + 7
		c.RoundRect(fx-1.5, fy0, 3, fy1-fy0, 1.5, uBG, 1)
		fill := col
		if tr.Muted {
			fill = 0x3A3A34
		}
		h := (fy1 - fy0) * lv
		c.RoundRect(fx-1.5, fy1-h, 3, h, 1.5, fill, 1)
		kk, env := a.turning()
		if sel && kk == 0 {
			c.Glow(fx, fy1-h, 14, knobCols[0], 0.6*env)
		}
		c.RoundRect(fx-5, fy1-h-2.5, 10, 5, 2.5, uText, 1)
		// sends A (K2) and B (K3), both shown (0.9.27)
		for i, key := range []string{"delay_send", "reverb_send"} {
			sv := tr.Sound.P[key]
			yy := 171 + float64(i)*6
			sc := []uint32{knobCols[1], knobCols[2]}[i]
			if tr.Muted {
				sc = uFaint
			}
			c.RoundRect(x+6, yy-1.5, 23, 3, 1.5, uBG, 1)
			if sv > 0.005 {
				c.RoundRect(x+6, yy-1.5, math.Max(3, 23*sv), 3, 1.5, sc, 0.95)
			}
			if sel && kk == i+1 && !a.shift {
				c.Glow(x+6+23*sv, yy, 10, sc, 0.6*env)
			}
		}
		// pan
		pan := 0.5
		if v, ok := tr.Sound.P["pan"]; ok {
			pan = v
		}
		c.Line(x+6, 187, x+29, 187, 1, uLine, 1)
		c.Line(cx, 184, cx, 190, 0.8, uFaint, 1)
		if sel && kk == 0 && a.shift {
			c.Glow(x+6+23*pan, 187, 10, knobCols[0], 0.7*env)
		}
		c.Circle(x+6+23*pan, 187, 2.4, nc, 1)
		// solo / mute
		if a.sq.solo[t] {
			c.RoundRect(x+5, 196, 25, 12, 6, uLime, 0.95)
			c.Text(cx, 202, "SOLO", gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else if tr.Muted {
			c.RoundRect(x+5, 196, 25, 12, 6, uRed, 0.9)
			c.Text(cx, 202, "MUTE", gfx.Small, uBG, gfx.Center|gfx.Mid)
		} else {
			c.RoundRect(x+5, 196, 25, 12, 6, col, 0.18)
			c.Text(cx, 202, "ON", gfx.Small, col, gfx.Center|gfx.Mid)
		}
	}
	if a.sq.playing || a.mixMoving() {
		a.animate(time.Now().Add(120 * time.Millisecond)) // activity lights and level dots
	}
	// what the knobs do here, with values for the selected track
	hint := "K1 LEVEL · K2 SEND A · K3 SEND B  ·  SHIFT + K1: PAN"
	if kk, _ := a.turning(); kk >= 0 {
		pr := a.knobParam(kk)
		hint = fmt.Sprintf("T%d  %s  %s", a.proj.Track+1, pr.label, pr.show(a.val(pr.key)))
	}
	a.uiBottom(c, hint)
}

// splitName breaks a name into two lines of at most n characters.
func splitName(s string, n int) (string, string) {
	if len(s) <= n {
		return s, ""
	}
	if i := strings.LastIndex(s[:min(len(s), n+1)], " "); i > 0 {
		r := strings.TrimSpace(s[i+1:])
		if len(r) > n {
			r = r[:n]
		}
		return s[:i], r
	}
	r := s[n:]
	if len(r) > n {
		r = r[:n]
	}
	return s[:n], r
}

// ------------------------------------------------------------------ patterns

func (a *app) drawPatternsV2(c *gfx.Canvas) {
	right := ""
	if len(a.proj.Chain) > 0 {
		right = "CHAIN " + chainText(a.proj.Chain)
	}
	viewTitle(c, "PATTERNS", right, uOrange)
	inChain := map[int]int{}
	for i, p := range a.proj.Chain {
		if _, ok := inChain[p]; !ok {
			inChain[p] = i + 1
		}
	}
	for i := 0; i < 16; i++ {
		x, y, w, h := padTile(i)
		p := &a.proj.Pat[i]
		playing := a.sq.playing && a.sq.posPat == i
		bg := uint32(uPanel)
		if playing {
			bg = 0x1F2614
		}
		c.RoundRect(x, y, w, h, 8, bg, 1)
		switch {
		case playing:
			c.RoundRectLine(x, y, w, h, 8, 1.6, uLime, 1)
		case i == a.proj.Sel:
			c.RoundRectLine(x, y, w, h, 8, 1.2, uText, 0.8)
		}
		nc := uint32(uDim)
		if p.HasNotes() {
			nc = uText
		}
		if playing {
			nc = uLime
		}
		c.Dots(x+6, y+6, fmt.Sprintf("%d", i+1), dotGlyph, 1.6, nc, 0)
		c.Text(x+w-6, y+9, fmt.Sprintf("%d", p.Len), gfx.Small, uFaint, gfx.Right|gfx.Mid)
		if n, ok := inChain[i]; ok {
			c.Circle(x+w-8, y+h-8, 5, uOrange, 1)
			c.Text(x+w-8, y+h-7.5, fmt.Sprintf("%d", n), gfx.Small, uBG, gfx.Center|gfx.Mid)
		}
		// a tiny map of the pattern: one row of dots per track
		if p.HasNotes() {
			sx := (w - 26) / float64(max(p.Len, 1))
			for t := 0; t < seq.Tracks; t++ {
				for s := 0; s < p.Len; s++ {
					if p.S[t][s].On {
						al := 0.9
						if a.proj.Tracks[t].Muted {
							al = 0.25
						}
						c.Rect(x+6+float64(s)*sx, y+18+float64(t)*2, math.Max(0.8, sx*0.7), 1.3, tcol(t), al)
					}
				}
			}
			if playing {
				px := x + 6 + float64(a.sq.posStep)*sx
				c.Line(px, y+16, px, y+35, 0.8, uText, 0.9)
			}
		} else {
			c.Text(x+6, y+26, "EMPTY", gfx.Small, uFaint, gfx.Left|gfx.Mid)
		}
	}
	hint := "TAP  PLAY IT  ·  SHIFT + PADS  CHAIN  ·  ERASE + PAD  CLEAR"
	if len(a.proj.Chain) > 0 {
		hint = "ERASE + LOOP  CLEAR CHAIN  ·  CHOP + PAD  COPY HERE"
	}
	if a.sq.playing {
		a.animate(time.Now().Add(150 * time.Millisecond))
	}
	a.uiBottom(c, hint)
}

// ------------------------------------------------------------------ effects

func (a *app) drawFXV2(c *gfx.Canvas) {
	viewTitle(c, "EFFECTS", "FADER RIDES THE PICKED ONE", uDim)
	fader := -1.0
	if a.ccKnown[controls.CCFader] {
		fader = float64(a.lastCC[controls.CCFader]) / 127
	}
	for i, ft := range faderTargets {
		col, row := i%4, 1-i/4 // pads 1-4 bottom row, 5-8 above
		x, y, w, h := 8+float64(col)*77, 44+float64(row)*80, 73.0, 74.0
		fc := rgb7(ft.col)
		fc = gfx.Mix(fc, 0xFFFFFF, 0.15)
		on := a.fxOn(i)
		v := a.fxNow[i]
		if on {
			c.Glow(x+w/2, y+h/2, 46, fc, 0.25)
			c.RoundRect(x, y, w, h, 12, fc, 0.92)
		} else {
			c.RoundRect(x, y, w, h, 12, uPanel, 1)
			c.RoundRectLine(x, y, w, h, 12, 1.2, fc, 0.55)
		}
		ink, sub := uint32(uText), uint32(uDim)
		if on {
			ink, sub = uBG, gfx.Mix(fc, uBG, 0.6)
		}
		if i == a.faderIdx {
			c.RoundRectLine(x-3, y-3, w+6, h+6, 14, 1.6, uText, 1)
		}
		c.Text(x+8, y+11, fmt.Sprintf("PAD %d", i+1), gfx.Small, sub, gfx.Left|gfx.Mid)
		c.Text(x+w-8, y+11, fmt.Sprintf("%d", i+9), gfx.Small, sub, gfx.Right|gfx.Mid)
		l1, l2 := splitName(ft.label, 7)
		if l2 == "" {
			c.Text(x+8, y+32, l1, gfx.Label, ink, gfx.Left|gfx.Mid)
		} else {
			c.Text(x+8, y+27, l1, gfx.Label, ink, gfx.Left|gfx.Mid)
			c.Text(x+8, y+37, l2, gfx.Label, ink, gfx.Left|gfx.Mid)
		}
		c.Text(x+8, y+51, fxAmount(i, v), gfx.Body, ink, gfx.Left|gfx.Mid)
		// amount bar from where the effect is off
		bx, bw, by := x+8, w-16, y+64
		track := uint32(uBG)
		if on {
			track = gfx.Mix(fc, uBG, 0.45)
		}
		c.RoundRect(bx, by-2.5, bw, 5, 2.5, track, 1)
		n0, n1 := math.Min(ft.neutral, v), math.Max(ft.neutral, v)
		if n1-n0 > 0.01 {
			bc := fc
			if on {
				bc = uBG
			}
			c.RoundRect(bx+bw*n0, by-2.5, math.Max(3, bw*(n1-n0)), 5, 2.5, bc, 1)
		}
		if ft.neutral > 0 && ft.neutral < 1 {
			c.Line(bx+bw*ft.neutral, by-5, bx+bw*ft.neutral, by+5, 1, sub, 1)
		}
		if i == a.faderIdx && fader >= 0 && a.faderVal < 0 {
			c.Poly([]float64{bx + bw*fader - 3, bx + bw*fader + 3, bx + bw*fader}, []float64{by + 9, by + 9, by + 5}, uText, 1)
		}
	}
	a.uiBottom(c, "PADS 1-8  PICK  ·  9-16  ON / OFF  ·  SHIFT + PAD  ALL OFF")
}

// ------------------------------------------------------------------ projects

func (a *app) drawProjectsV2(c *gfx.Canvas) {
	right := "HOLD SAMPLE RECORD + PAD  SAVE"
	rc := uint32(uDim)
	if a.recHeld {
		right, rc = "SAVE: PICK A PAD", uOrange
	}
	viewTitle(c, "PROJECTS", right, rc)
	for i := 0; i < 16; i++ {
		x, y, w, h := padTile(i)
		name := a.sq.projNames[i]
		cur := i+1 == a.sq.projSlot
		bg := uint32(uPanel)
		if name != "" {
			bg = uPanel2
		}
		c.RoundRect(x, y, w, h, 8, bg, 1)
		switch {
		case a.recHeld:
			c.RoundRectLine(x, y, w, h, 8, 1.2, uOrange, 0.8)
		case cur:
			c.RoundRectLine(x, y, w, h, 8, 1.6, uLime, 1)
		}
		nc := uint32(uFaint)
		if name != "" {
			nc = uText
		}
		if cur {
			nc = uLime
		}
		c.Dots(x+6, y+6, fmt.Sprintf("%d", i+1), dotGlyph, 1.6, nc, 0)
		if name == "" {
			c.Text(x+6, y+27, "EMPTY", gfx.Small, uFaint, gfx.Left|gfx.Mid)
			continue
		}
		l1, l2 := splitName(strings.ToUpper(name), 12)
		if l2 == "" {
			c.Text(x+6, y+27, l1, gfx.Small, uText, gfx.Left|gfx.Mid)
		} else {
			c.Text(x+6, y+22, l1, gfx.Small, uText, gfx.Left|gfx.Mid)
			c.Text(x+6, y+30, l2, gfx.Small, uText, gfx.Left|gfx.Mid)
		}
	}
	a.uiBottom(c, "PAD  LOAD  ·  ERASE + PAD  NEW PROJECT WITH THESE SOUNDS")
}

// ------------------------------------------------------------------ sounds

type bankCache struct {
	bank  int
	at    time.Time
	names [16]string
	user  [16]bool
}

func (a *app) bankNames() *bankCache {
	bc := &a.ui.bank
	if bc.at.IsZero() || bc.bank != a.bank || time.Since(bc.at) > 3*time.Second {
		bc.bank, bc.at = a.bank, time.Now()
		for i := 0; i < 16; i++ {
			p, user := sound.LoadSlot(a.soundsDir(), a.bank*16+i+1)
			bc.names[i], bc.user[i] = p.Name, user
		}
	}
	return bc
}

func (a *app) drawSoundsV2(c *gfx.Canvas) {
	c.Text(10, 31, "SOUNDS", gfx.Big, uText, gfx.Left|gfx.Mid)
	// bank tabs
	for b := 0; b < 4; b++ {
		x := 232 + float64(b)*20
		if b == a.bank {
			c.RoundRect(x, 24, 17, 15, 7.5, uLime, 1)
			c.Text(x+8.5, 31.5, string(rune('A'+b)), gfx.Label, uBG, gfx.Center|gfx.Mid)
		} else {
			c.RoundRect(x, 24, 17, 15, 7.5, uPanel, 1)
			c.Text(x+8.5, 31.5, string(rune('A'+b)), gfx.Label, uDim, gfx.Center|gfx.Mid)
		}
	}
	kind := "FACTORY"
	if a.bank > 0 {
		kind = "YOURS"
	}
	c.Text(226, 32, kind, gfx.Small, uFaint, gfx.Right|gfx.Mid)
	bc := a.bankNames()
	col := tcol(a.proj.Track)
	for i := 0; i < 16; i++ {
		x, y, w, h := padTile(i)
		n := a.bank*16 + i + 1
		cur := n == a.slot
		c.RoundRect(x, y, w, h, 8, uPanel, 1)
		switch {
		case a.recHeld:
			c.RoundRectLine(x, y, w, h, 8, 1.2, uOrange, 0.8)
		case cur:
			c.RoundRectLine(x, y, w, h, 8, 1.6, col, 1)
		}
		dot := uint32(uFaint)
		switch {
		case bc.user[i]:
			dot = 0x5FD414
		case n <= 16:
			dot = 0x1E7BFF
		}
		c.Circle(x+9, y+9, 2.6, dot, 1)
		c.Text(x+w-6, y+9, fmt.Sprintf("%s%d", string(rune('A'+a.bank)), i+1), gfx.Small, uFaint, gfx.Right|gfx.Mid)
		name := strings.ToUpper(bc.names[i])
		nc := uint32(uText)
		if !bc.user[i] && n > 16 {
			nc = uFaint
		}
		if cur {
			nc = col
		}
		l1, l2 := splitName(name, 11)
		if l2 == "" {
			c.Text(x+6, y+25, l1, gfx.Label, nc, gfx.Left|gfx.Mid)
		} else {
			c.Text(x+6, y+21, l1, gfx.Label, nc, gfx.Left|gfx.Mid)
			c.Text(x+6, y+30, l2, gfx.Label, nc, gfx.Left|gfx.Mid)
		}
	}
	hint := "PAD  LOAD  ·  PAD BANK  NEXT BANK  ·  HOLD SAMPLE RECORD + PAD  SAVE"
	if a.recHeld {
		hint = "SAVE: PICK A PAD"
	}
	a.uiBottom(c, hint)
}

// ------------------------------------------------------------------ exit

func (a *app) drawExitV2(c *gfx.Canvas) {
	c.RoundRect(8, 30, 304, 192, 16, 0x2A0F0B, 1)
	c.RoundRectLine(8, 30, 304, 192, 16, 1.4, uRed, 0.8)
	c.Text(160, 66, "EXIT TO STOCK?", gfx.Big, uText, gfx.Center|gfx.Mid)
	c.RoundRect(86, 92, 148, 26, 13, uRed, 1)
	c.Text(160, 105, "PRESS STOP TO EXIT", gfx.Label, uBG, gfx.Center|gfx.Mid)
	c.Text(160, 134, "ANY OTHER BUTTON CANCELS", gfx.Body, uDim, gfx.Center|gfx.Mid)
	c.Line(40, 154, 280, 154, 1, 0x4A2520, 1)
	c.Text(160, 172, "YOUR PROJECT AND SOUNDS ARE SAVED.", gfx.Small, uDim, gfx.Center|gfx.Mid)
	c.Text(160, 186, "THE NORMAL MPC SOFTWARE RETURNS IN ABOUT 10 S.", gfx.Small, uDim, gfx.Center|gfx.Mid)
}

// mixLevel: track t's level (0..1 over -48..0 dB, falling smoothly) and its
// peak dot (held for a second, then falling).
func (a *app) mixLevel(t int) (float64, float64) {
	d := &a.dsp.mt
	d.mu.Lock()
	b, at := d.lv[t], d.lvAt
	d.mu.Unlock()
	v := 0.0
	if time.Since(at) < 300*time.Millisecond && b > 0 {
		v = math.Max(0, math.Min(1, (float64(b)/255*60-12)/48))
	}
	now := time.Now()
	m := &a.mt
	if t%8 == 0 { // once a frame: how long since the last one
		m.lvDt = math.Min(0.2, now.Sub(m.lvLast).Seconds())
		m.lvLast = now
	}
	dt := m.lvDt
	if v >= m.lvShow[t] {
		m.lvShow[t] = v
	} else {
		m.lvShow[t] = math.Max(v, m.lvShow[t]-dt*1.6)
	}
	if m.lvShow[t] >= m.lvPeak[t] {
		m.lvPeak[t], m.lvPeakAt[t] = m.lvShow[t], now
	} else if now.Sub(m.lvPeakAt[t]) > time.Second {
		m.lvPeak[t] = math.Max(m.lvShow[t], m.lvPeak[t]-dt*0.8)
	}
	return m.lvShow[t], m.lvPeak[t]
}

// mixMoving: any dot still lit (keep drawing while they fall).
func (a *app) mixMoving() bool {
	for t := 0; t < seq.Tracks; t++ {
		if a.mt.lvShow[t] > 0.01 || a.mt.lvPeak[t] > 0.01 {
			return true
		}
	}
	return false
}
