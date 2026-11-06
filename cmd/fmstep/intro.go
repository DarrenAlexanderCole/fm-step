package main

// The startup moment. It plays while the sound engine, controls and session
// load, so it costs no time: it starts as soon as the screen is open and
// lands when everything is ready (never sooner than introMin, so it reads
// as a moment rather than a flicker).
//
// The pieces of FM-STEP arrive and lock together:
//   - the eight track colours fly in as FLOW bubbles and land on a 16-step
//     loop line (the sequencer);
//   - the three knob colours rise behind them as watercolour hills (the sound);
//   - FM-STEP lights up dot by dot in the dot-matrix type (the screen);
//   - a playhead sweeps the loop, each note flashes as it is passed and the
//     16 pads light up in step with it (the hardware joins in);
//   - everything settles into the first page.

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/gfx"
	"fmstep/internal/panel"
)

const (
	introMin   = 1800 * time.Millisecond // shortest version of the moment
	introOut   = 350 * time.Millisecond  // fade into the first page
	introFloor = 1200 * time.Millisecond // always long enough to be seen
	introFrame = 33 * time.Millisecond   // ~30 fps target (partial flushes)
)

// Where each track's note lands on the 16-step loop line.
var introSteps = [8]int{0, 4, 2, 10, 6, 12, 8, 14}

type intro struct {
	cs    atomic.Pointer[panel.ControlSurface] // set once the pads are open
	ready chan struct{}                        // closed when loading is done
	done  chan struct{}                        // closed when the intro has ended
	ver   string
	min   time.Duration // how long the moment plays at least (see introBudget)
}

// runIntro draws the moment on scr until loading is done, then fades out.
// It owns the screen and the pad lights until done is closed.
func runIntro(scr *panel.Screen, in *intro, logf func(string, ...any)) {
	defer close(in.done)
	start := time.Now()
	// a shorter budget plays the same moment faster (up to 2.5x)
	speed := 1.0
	if in.min > 0 && in.min < introMin {
		speed = math.Min(2.5, introMin.Seconds()/in.min.Seconds())
	}
	var pads [16][3]byte
	frames := 0
	var drawSum, flushSum, drawMax, flushMax time.Duration
	outAt := time.Time{}
	for {
		now := time.Now()
		t := now.Sub(start)
		if outAt.IsZero() && t >= in.min {
			select {
			case <-in.ready:
				outAt = now
			default:
			}
		}
		out := 0.0
		if !outAt.IsZero() {
			out = float64(now.Sub(outAt)) / float64(introOut)
			if out >= 1 {
				break
			}
		}
		drawIntro(scr.C, t.Seconds()*speed, out, in.ver)
		d0 := time.Since(now)
		scr.Flush()
		fl := time.Since(now) - d0
		drawSum, flushSum = drawSum+d0, flushSum+fl
		drawMax, flushMax = max(drawMax, d0), max(flushMax, fl)
		frames++
		if cs := in.cs.Load(); cs != nil {
			introPads(cs, &pads, t.Seconds()*speed, out)
		}
		if d := introFrame - time.Since(now); d > 0 {
			time.Sleep(d)
		}
	}
	if cs := in.cs.Load(); cs != nil {
		for i := range pads {
			if pads[i] != [3]byte{} {
				cs.Write(controls.PadColour(byte(i), 0, 0, 0))
			}
		}
	}
	n := time.Duration(max(frames, 1))
	logf("INTRO_DONE ms=%d frames=%d speed=%.2f draw_avg_ms=%.1f draw_max_ms=%.1f flush_avg_ms=%.1f flush_max_ms=%.1f",
		time.Since(start).Milliseconds(), frames, speed,
		float64(drawSum/n)/1e6, float64(drawMax)/1e6, float64(flushSum/n)/1e6, float64(flushMax)/1e6)
}

// ease: smooth 0..1 between a and b seconds.
func ease(t, a, b float64) float64 {
	x := math.Max(0, math.Min(1, (t-a)/(b-a)))
	return x * x * (3 - 2*x)
}

// Loop-line geometry.
const (
	introLineY = 168.0
	introX0    = 40.0
	introX1    = 280.0
)

func introStepX(s int) float64 { return introX0 + (introX1-introX0)*(float64(s)+0.5)/16 }

// playhead position in steps (0..16) and whether it is running.
func introPlayhead(t float64) (float64, bool) {
	const a, b = 1.15, 1.95
	if t < a {
		return 0, false
	}
	if t > b {
		return 16, false
	}
	return 16 * (t - a) / (b - a), true
}

// drawIntro draws one frame at time t (seconds); out (0..1) fades it into
// the background. Pure drawing: also used by the GIF and preview tests.
func drawIntro(c *gfx.Canvas, t, out float64, ver string) {
	c.Clear(uBG)
	vis := 1 - ease(out, 0, 1)

	// 1. faint dot field
	fa := 0.10 * ease(t, 0, 0.5) * vis
	for y := 6.0; y < 240; y += 12 {
		for x := 6.0; x < 320; x += 12 {
			c.Blend(int(x), int(y), uDim, fa)
		}
	}

	// 2. watercolour hills in the knob colours, rising behind the loop
	rise := ease(t, 0.45, 1.5)
	for k := 0; k < 3; k++ {
		cx := 320*(0.22+0.28*float64(k)) + 14*math.Sin(t*0.9+float64(k)*2.1)
		width := 62.0 + 10*float64(k%2)
		height := (240 - introLineY + 26) * (0.25 + 0.75*rise) * (0.85 + 0.15*math.Sin(t*2.3+float64(k)))
		xs, ys := make([]float64, 0, 81), make([]float64, 0, 81)
		for i := 0; i <= 80; i++ {
			x := 320 * float64(i) / 80
			d := (x - cx) / width
			hump := math.Exp(-d * d)
			ys = append(ys, 240-height*hump*rise)
			xs = append(xs, x)
		}
		al := 0.20 * rise * vis
		c.FillBetween(xs, ys, 240, knobCols[k], al)
		c.Polyline(xs, ys, 1.1, knobCols[k], 0.30*rise*vis)
	}

	// 3. the loop line and its 16 step ticks
	la := ease(t, 0.1, 0.6) * vis
	grow := ease(t, 0.1, 0.7)
	mid := (introX0 + introX1) / 2
	half := (introX1 - introX0) / 2 * grow
	c.Line(mid-half, introLineY, mid+half, introLineY, 1, uDim, 0.45*la)
	for s := 0; s < 16; s++ {
		x := introStepX(s)
		if math.Abs(x-mid) > half {
			continue
		}
		r := 1.0
		if s%4 == 0 {
			r = 1.6
		}
		c.Circle(x, introLineY, r, uDim, 0.7*la)
	}

	// 4. the eight track notes fly in and land on their steps
	ph, running := introPlayhead(t)
	for tr := 0; tr < 8; tr++ {
		col := rgb7(trackColours[tr])
		land := 0.25 + 0.11*float64(tr) + 0.45 // arrival time
		dep := land - 0.55                     // departure
		p := ease(t, dep, land)
		if t < dep {
			continue
		}
		s := introSteps[tr]
		tx, ty := introStepX(s), introLineY
		// start on the edge, in a ring around the centre
		ang := -math.Pi/2 + float64(tr)*math.Pi/4 + 0.4
		sx, sy := 160+190*math.Cos(ang), 120+150*math.Sin(ang)
		// curved path: bow upward like a FLOW bubble falling into place
		x := sx + (tx-sx)*p
		y := sy + (ty-sy)*p - 50*math.Sin(math.Pi*p)
		r := 3.4 - 1.0*p
		al := vis
		if p < 1 {
			// a short trail
			for j := 1; j <= 4; j++ {
				q := math.Max(0, p-0.05*float64(j))
				jx := sx + (tx-sx)*q
				jy := sy + (ty-sy)*q - 50*math.Sin(math.Pi*q)
				c.Circle(jx, jy, r*(1-0.17*float64(j)), col, 0.22*al*(1-0.2*float64(j)))
			}
		} else {
			// landed: a splash ring, then the note sits on the line
			since := t - land
			if since < 0.4 {
				c.Ring(tx, ty, 3+14*since, 1, col, (1-since/0.4)*0.8*al)
			}
			// flash as the playhead passes
			if d := ph - float64(s); running && d >= 0 && d < 1.6 {
				f := 1 - d/1.6
				c.Glow(tx, ty, 10*f+3, col, 0.55*f*al)
				r += 1.6 * f
			}
		}
		c.Circle(x, y, r, col, al)
	}

	// 5. the playhead sweeping the loop
	if running {
		x := introX0 + (introX1-introX0)*ph/16
		c.Line(x, introLineY-9, x, introLineY+9, 1.2, uLime, 0.9*vis)
		c.Line(introX0, introLineY, x, introLineY, 1, uLime, 0.35*vis)
	} else if t > 1.95 {
		c.Line(introX0, introLineY, introX1, introLineY, 1, uLime, 0.35*vis*ease(t, 1.95, 2.0))
	}

	// 6. FM-STEP lights up dot by dot
	introTitle(c, t, vis)

	// 7. the line under the name
	sa := ease(t, 1.45, 1.85) * vis
	if sa > 0 {
		c.Text(160, 120, "SYNTH GROOVEBOX  ·  v"+ver, gfx.Small, uDim, gfx.Center|gfx.Mid)
		if sa < 1 {
			c.Rect(100, 113, 120, 14, uBG, 1-sa)
		}
	}
}

// introTitle: "FM-STEP" in big 5x7 dot-matrix letters. Each dot lights at
// its own moment, sweeping left to right with a little scatter, like a
// display warming up; the knob colours ride the leading edge.
func introTitle(c *gfx.Canvas, t, vis float64) {
	const s = "FM-STEP"
	const pitch = 5.4
	const r = 1.85
	w := float64(len(s))*6*pitch - pitch
	x0 := 160 - w/2 + pitch/2
	y0 := 62.0
	for i, ch := range s {
		g := panel.Glyph5x7(ch)
		for gy := 0; gy < 7; gy++ {
			for gx := 0; gx < 5; gx++ {
				x := x0 + (float64(i*6+gx))*pitch
				y := y0 + float64(gy)*pitch
				on := g[gy]&(0x10>>gx) != 0
				// off dots: the unlit matrix, visible from the start
				if !on {
					c.Circle(x, y, r*0.7, 0x283019, 0.8*ease(t, 0.3, 0.8)*vis)
					continue
				}
				col := float64(i*6 + gx)
				h := math.Mod(float64((i*31+gx*7+gy*13)%17)/17, 1)
				lit := 0.75 + col*0.016 + 0.12*h // when this dot lights
				e := ease(t, lit, lit+0.12)
				if e <= 0 {
					c.Circle(x, y, r*0.7, 0x283019, 0.8*ease(t, 0.3, 0.8)*vis)
					continue
				}
				// just-lit dots flash in a knob colour, then settle to lime
				k := (i*6 + gx) % 3
				fresh := 1 - ease(t, lit+0.05, lit+0.35)
				dc := gfx.Mix(uLime, knobCols[k], 0.85*fresh)
				if fresh > 0.3 {
					c.Glow(x, y, 4, knobCols[k], 0.35*fresh*vis)
				}
				c.Circle(x, y, r*(0.7+0.3*e), dc, vis)
			}
		}
	}
}

// introPads lights the 16 pads with the playhead: the pad under each landed
// note glows in its track colour as it is passed, the others blink dimly.
func introPads(cs *panel.ControlSurface, pads *[16][3]byte, t, out float64) {
	ph, running := introPlayhead(t)
	trackAt := [16]int{}
	for i := range trackAt {
		trackAt[i] = -1
	}
	for tr, s := range introSteps {
		trackAt[s] = tr
	}
	for i := 0; i < 16; i++ {
		var want [3]byte
		d := ph - float64(i)
		lvl := 0.0
		switch {
		case running && d >= 0 && d < 2:
			lvl = 1 - d/2
		case !running && t > 1.95:
			lvl = 0.25 // all lit softly as the loop completes
		}
		lvl *= 1 - out
		if lvl > 0.02 {
			c := [3]byte{40, 40, 40}
			if tr := trackAt[i]; tr >= 0 {
				c = trackColours[tr]
			}
			want = [3]byte{byte(float64(c[0]) * lvl), byte(float64(c[1]) * lvl), byte(float64(c[2]) * lvl)}
		}
		if want != pads[i] {
			pads[i] = want
			// pad numbering on the MPC runs bottom-left to top-right; the
			// sweep goes along the rows in reading order from the bottom.
			cs.Write(controls.PadColour(byte(i), want[0], want[1], want[2]))
		}
	}
}

// startBudget: from the launcher starting to FM-STEP being playable.
const startBudget = 5 * time.Second

// introBudget returns how long the moment may play so the whole launch fits
// in startBudget: what is left of the budget, minus the fade, capped at
// introMin. launchFile holds the uptime at which the launcher started; if it
// is missing the full moment plays.
func introBudget(launchFile string, now float64) time.Duration {
	b, err := os.ReadFile(launchFile)
	if err != nil {
		return introMin
	}
	var up float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%g", &up); err != nil || up <= 0 || now < up {
		return introMin
	}
	left := startBudget - time.Duration((now-up)*float64(time.Second)) - introOut - 100*time.Millisecond
	if left < introFloor {
		// over budget (e.g. the first launch after an install, when the
		// firmware check runs in full): still play a short, visible moment
		return introFloor
	}
	if left > introMin {
		return introMin
	}
	return left
}

// uptimeSec is /proc/uptime as a number (0 if unreadable).
func uptimeSec() float64 {
	var v float64
	fmt.Sscanf(uptime(), "%g", &v)
	return v
}

// appVersion is the build shown in the logs and the startup moment.
const appVersion = "0.9.26"

// uptime returns /proc/uptime's first field, to line app logs up with the
// launcher's and supervisor's "up=" stamps.
func uptime() string {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "?"
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return "?"
	}
	return f[0]
}
