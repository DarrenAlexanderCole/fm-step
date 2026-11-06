package main

// The MIXTAPE recorder's model and shared parts: the supa struct (filled
// from the live recorder in mixtape.go), the screen-button legend with
// transport icons, the state word and the level meters. The screen itself is
// drawRecorder (recorder.go). The SUPA TAPE cassette / dot-reel / poster
// directions were dropped in 0.9.23: Darren kept the current UI language.

import (
	"fmt"
	"math"

	"fmstep/internal/gfx"
)

const (
	supaIdle = iota
	supaRec
	supaRecPaused
	supaListen
	supaListenPaused
)

type supa struct {
	name   string    // "TAKE 007"
	state  int       // supaIdle ...
	secs   float64   // where we are: the take so far (recording) or the playhead
	total  float64   // the take's length (listening)
	limit  float64   // recording: the most this take can be
	free   float64   // GB free on the card
	env    []float64 // the whole take, one level per 0.1 s (0..1)
	recent []float64 // the last few seconds, ~30 per second, newest last
	angle  float64   // reel spin
	level  [2]float64
	// 0.9.24: trimming a finished take
	trimming        bool
	trimIn, trimOut float64
	loopA, loopB    float64
	loop            bool
	window          float64 // seconds the card shows (0 = the usual 8 s)
	focusT          float64 // the card centres on this when zoomed
	knobs           [3][2]string
	knobHot         int // the knob turned last (-1 none)
	// 0.9.25
	height           float64   // K1 HEIGHT (1 = fit)
	lineMin, lineMax []float64 // the real wave in the card's window (min / max per column, -1..1)
	selOut           bool      // the OUT marker is the one DATA moves
}

// tapeFrac: how much of the tape has run (0..1).
func (s *supa) tapeFrac() float64 {
	switch s.state {
	case supaListen, supaListenPaused, supaIdle:
		if s.total > 0 {
			return math.Min(1, s.secs/s.total)
		}
	case supaRec, supaRecPaused:
		if s.limit > 0 {
			return math.Min(1, s.secs/s.limit)
		}
	}
	return 0
}

func (s *supa) moving() bool { return s.state == supaRec || s.state == supaListen }

// ---------------------------------------------------------------- shared parts

// transport icons for the three screen buttons
const (
	icNone = iota
	icRew
	icPlay
	icPause
	icFF
	icRec
	icText
)

type supaBtn struct {
	icon int
	text string
	on   bool
}

func drawIcon(c *gfx.Canvas, ic int, x, y float64, col uint32) float64 {
	switch ic {
	case icRew:
		c.Poly([]float64{x, x + 6, x + 6}, []float64{y, y - 4, y + 4}, col, 1)
		c.Poly([]float64{x + 6, x + 12, x + 12}, []float64{y, y - 4, y + 4}, col, 1)
		return 12
	case icFF:
		c.Poly([]float64{x, x + 6, x}, []float64{y - 4, y, y + 4}, col, 1)
		c.Poly([]float64{x + 6, x + 12, x + 6}, []float64{y - 4, y, y + 4}, col, 1)
		return 12
	case icPlay:
		c.Poly([]float64{x, x + 7, x}, []float64{y - 4.5, y, y + 4.5}, col, 1)
		return 7
	case icPause:
		c.Rect(x, y-4, 2.6, 8, col, 1)
		c.Rect(x+4.6, y-4, 2.6, 8, col, 1)
		return 7.2
	case icRec:
		c.Circle(x+4, y, 3.6, uRed, 1)
		return 8
	}
	return 0
}

// supaButtons: what the three screen buttons do, under the header.
func supaButtons(c *gfx.Canvas, b [3]supaBtn) {
	for i, bt := range b {
		if bt.icon == icNone && bt.text == "" {
			continue
		}
		cx := 54 + float64(i)*106
		iw := 0.0
		if bt.icon != icNone && bt.icon != icText {
			iw = 12
			if bt.icon == icPlay || bt.icon == icPause {
				iw = 7.2
			}
		}
		tw := 0.0
		if bt.text != "" {
			tw = gfx.Small.Width(bt.text)
		}
		gap := 0.0
		if iw > 0 && tw > 0 {
			gap = 4
		}
		w := iw + gap + tw + 16
		fg, bg := uint32(uDim), uint32(0)
		if bt.on {
			fg, bg = uBG, uLime
			c.RoundRect(cx-w/2, 26, w, 13, 6.5, bg, 1)
		} else {
			c.RoundRectLine(cx-w/2, 26, w, 13, 6.5, 1, uFaint, 1)
		}
		x := cx - w/2 + 8
		if iw > 0 {
			drawIcon(c, bt.icon, x, 32.5, fg)
			x += iw + gap
		}
		if tw > 0 {
			c.Text(x, 32.5, bt.text, gfx.Small, fg, gfx.Left|gfx.Mid)
		}
	}
}

// supaState: the state word with its light.
func supaState(c *gfx.Canvas, s *supa, x, y float64, blink bool) {
	switch s.state {
	case supaRec:
		if blink {
			c.Glow(x+4, y, 9, uRed, 0.6)
			c.Circle(x+4, y, 4, uRed, 1)
		}
		c.Text(x+13, y, "REC", gfx.Label, uRed, gfx.Left|gfx.Mid)
	case supaRecPaused:
		drawIcon(c, icPause, x, y, uRed)
		c.Text(x+13, y, "PAUSED", gfx.Label, uRed, gfx.Left|gfx.Mid)
	case supaListen:
		drawIcon(c, icPlay, x+1, y, uLime)
		c.Text(x+13, y, "LISTEN", gfx.Label, uLime, gfx.Left|gfx.Mid)
	case supaListenPaused:
		drawIcon(c, icPause, x, y, uLime)
		c.Text(x+13, y, "PAUSED", gfx.Label, uLime, gfx.Left|gfx.Mid)
	default:
		c.Text(x, y, "STOPPED", gfx.Label, uDim, gfx.Left|gfx.Mid)
	}
}

// supaRight: time left (recording) or the length (listening), and the card.
func supaRight(c *gfx.Canvas, s *supa, y float64) {
	switch s.state {
	case supaRec, supaRecPaused:
		c.Text(310, y, clock(math.Max(0, s.limit-s.secs))+" LEFT", gfx.Small, uDim, gfx.Right|gfx.Mid)
	case supaListen, supaListenPaused:
		c.Text(310, y, "OF "+clock(s.total), gfx.Small, uDim, gfx.Right|gfx.Mid)
	}
	if s.free > 0 {
		c.Text(310, y+11, fmt.Sprintf("CARD %.1f GB FREE", s.free), gfx.Small, uFaint, gfx.Right|gfx.Mid)
	}
}

// sample: the recent level at fraction f of the window (0 old .. 1 newest)
func (s *supa) recentAt(f float64) float64 {
	if len(s.recent) == 0 {
		return 0
	}
	i := int(f * float64(len(s.recent)-1))
	return s.recent[max(0, min(len(s.recent)-1, i))]
}

func (s *supa) envAt(f float64) float64 {
	if len(s.env) == 0 {
		return 0
	}
	i := int(f * float64(len(s.env)))
	return s.env[max(0, min(len(s.env)-1, i))]
}

func supaLevel(c *gfx.Canvas, lv [2]float64) {
	for i := 0; i < 2; i++ {
		y := 210 + float64(i)*4
		db := 20 * math.Log10(lv[i]+1e-6)
		f := math.Max(0, math.Min(1, (db+48)/48))
		col := uint32(0x5FD414)
		if db > -1 {
			col = uRed
		} else if db > -6 {
			col = uOrange
		}
		c.RoundRect(12, y, 296, 3, 1.5, uPanel2, 1)
		if f > 0 {
			c.RoundRect(12, y, 296*f, 3, 1.5, col, 1)
		}
	}
}
