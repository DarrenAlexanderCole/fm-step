package main

// MIXTAPE (0.9.22): a cassette recorder for the whole jam. The engine side
// (a copy of the mix into a ring, a writer thread to the card, LISTEN) is
// dsp/mixtape.h; this file starts and stops takes, asks what to do with them
// and draws the cassette.
//
//   SAMPLE RECORD (tap)     start a take · tap again: stop
//   SHIFT + SAMPLE RECORD   show the cassette (while recording, from any view)
//   on stop                 the screen buttons: DELETE (press twice) · LISTEN · SAVE
//   DATA (listening)        jump 5 s back / forward
//   PAD FX                  the LIBRARY: a saved take is at the top, ready to chop
//
// Takes go to FMSTEP/MIXTAPE/take NNN.wav on the card (16-bit stereo 48 kHz,
// up to 20 minutes, or less when the card is nearly full). Holding SAMPLE
// RECORD with a pad (saving a sound or a project) never starts a take.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fmstep/internal/controls"
	"fmstep/internal/gfx"
)

const (
	mtMaxSecs   = 20 * 60
	mtBytesSec  = 48000 * 4
	mtCardSpare = 64 << 20 // always leave this much free on the card
)

// mtData: what the engine reports (written by the DSP reader goroutine).
type mtData struct {
	mu         sync.Mutex
	frames     int
	pk         [2]float64
	drops      int
	recAt      time.Time
	doneSeq    int
	doneFrames int
	donePath   string
	full       bool
	errSeq     int
	err        string
	playPos    int
	playTotal  int
	playAt     time.Time
	endSeq     int
	lv         [9]uint8
	lvAt       time.Time
}

func (d *mtData) parse(line string, logf func(string, ...any)) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	switch f[0] {
	case "LV":
		if len(f) == 2 && len(f[1]) == 18 {
			d.mu.Lock()
			for i := 0; i < 9; i++ {
				v, _ := strconv.ParseUint(f[1][2*i:2*i+2], 16, 8)
				d.lv[i] = uint8(v)
			}
			d.lvAt = time.Now()
			d.mu.Unlock()
		}
		return true
	case "MTR":
		if len(f) == 5 {
			d.mu.Lock()
			d.frames, _ = strconv.Atoi(f[1])
			d.pk[0], _ = strconv.ParseFloat(f[2], 64)
			d.pk[1], _ = strconv.ParseFloat(f[3], 64)
			d.drops, _ = strconv.Atoi(f[4])
			d.recAt = time.Now()
			d.mu.Unlock()
		}
		return true
	case "MTP":
		if len(f) == 3 {
			d.mu.Lock()
			d.playPos, _ = strconv.Atoi(f[1])
			d.playTotal, _ = strconv.Atoi(f[2])
			d.playAt = time.Now()
			d.mu.Unlock()
		}
		return true
	case "MTPEND":
		d.mu.Lock()
		d.endSeq++
		d.mu.Unlock()
		return true
	case "MTFULL":
		d.mu.Lock()
		d.full = true
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	case "MTDONE":
		d.mu.Lock()
		if len(f) >= 3 {
			d.doneFrames, _ = strconv.Atoi(f[1])
			d.donePath = strings.SplitN(line, " ", 3)[2]
		}
		d.doneSeq++
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	case "MTERR":
		d.mu.Lock()
		d.err = line
		d.errSeq++
		d.mu.Unlock()
		logf("DSP %s", line)
		return true
	case "MTSTART":
		logf("DSP %s", line)
		return true
	}
	return false
}

type mtState struct {
	view      bool // the cassette is on screen
	rec       bool // a take is being recorded
	stopping  bool // MTSTOP sent, waiting for MTDONE
	ask       bool // stopped: DELETE · LISTEN · SAVE
	path      string
	name      string // "TAKE 007"
	frames    int
	limit     int // frames: 20 minutes, or what fits on the card
	free      uint64
	started   time.Time
	delArm    time.Time
	saved     string // the LIBRARY selects this take next time it opens
	recDown   time.Time
	recUsed   bool
	playing   bool
	playPath  string
	playPos   int
	playTotal int
	playFrom  time.Time // when LISTEN last (re)started
	doneSeq   int
	errSeq    int
	endSeq    int
	angle     float64
	lastT     time.Time
	pk        [2]float64
	// mixer dots
	lvShow   [9]float64
	lvPeak   [9]float64
	lvPeakAt [9]time.Time
	lvOn     bool
	lvLast   time.Time
	lvDt     float64
	peaks    map[string][]byte
	peaksSz  map[string]int64
}

func (a *app) mixtapeDir() string {
	return filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "MIXTAPE")
}

// cardFree: bytes free where dir lives.
func cardFree(dir string) uint64 {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0
	}
	return uint64(st.Bavail) * uint64(st.Bsize)
}

var takeRe = regexp.MustCompile(`(?i)^take (\d+)\.wav$`)

func nextTake(dir string) (string, int) {
	n := 0
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if m := takeRe.FindStringSubmatch(e.Name()); m != nil {
			if v, _ := strconv.Atoi(m[1]); v > n {
				n = v
			}
		}
	}
	return filepath.Join(dir, fmt.Sprintf("take %03d.wav", n+1)), n + 1
}

// mtRecButton: SAMPLE RECORD down / up. A short tap with nothing else
// pressed starts or stops a take; SHIFT + tap just shows the cassette.
func (a *app) mtRecButton(down bool) {
	if down {
		a.mt.recDown, a.mt.recUsed = time.Now(), false
		return
	}
	if a.mt.recUsed || time.Since(a.mt.recDown) > 700*time.Millisecond || a.mode == modeConfirmExit {
		return
	}
	if a.shift {
		a.mtShow(!a.mt.view)
		return
	}
	if a.mt.rec {
		a.mtStop()
	} else if !a.mt.stopping {
		a.mtStart()
	}
}

func (a *app) mtShow(on bool) {
	if on && a.smp.view != smpOff {
		a.smpLeave()
	}
	a.mt.view = on
	if a.scr != nil {
		a.scr.Invalidate()
	}
	a.dirty = true
}

func (a *app) mtStart() {
	if os.Getenv("FMSTEP_SD") == "" {
		a.say("NO CARD TO RECORD TO")
		return
	}
	dir := a.mixtapeDir()
	os.MkdirAll(dir, 0755)
	free := cardFree(dir)
	secs := mtMaxSecs
	if free > 0 {
		fit := 0
		if free > mtCardSpare {
			fit = int((free - mtCardSpare) / mtBytesSec)
		}
		secs = min(secs, fit)
	}
	if secs < 10 {
		a.say("THE CARD IS FULL  ·  NO ROOM FOR A TAKE")
		a.log("MIXTAPE_NOSPACE free=%d", free)
		return
	}
	a.mtListenStop()
	path, n := nextTake(dir)
	a.mt.rec, a.mt.stopping, a.mt.ask = true, false, false
	a.mt.path, a.mt.name = path, fmt.Sprintf("TAKE %03d", n)
	a.mt.frames, a.mt.limit, a.mt.free = 0, secs*48000, free
	a.mt.started = time.Now()
	a.mt.delArm = time.Time{}
	d := &a.dsp.mt
	d.mu.Lock()
	d.frames, d.recAt, d.full = 0, time.Now(), false
	d.mu.Unlock()
	a.dsp.send("MTREC %s", path)
	if a.smp.view == smpOff { // chopping: keep chopping, the badge shows the take
		a.mtShow(true)
	}
	a.say("RECORDING " + a.mt.name + "  ·  SAMPLE RECORD: STOP")
	a.log("MIXTAPE_START %s limit_s=%d free=%d", path, secs, free)
	a.animate(time.Now().Add(time.Second))
}

func (a *app) mtStop() {
	if !a.mt.rec {
		return
	}
	a.dsp.send("MTSTOP")
	a.mt.rec, a.mt.stopping = false, true
	a.say("FINISHING " + a.mt.name + "...")
	a.log("MIXTAPE_STOP frames=%d", a.mt.frames)
	a.dirty = true
}

// mtTick runs every loop: engine news, the length limit, a lost engine.
func (a *app) mtTick() {
	if a.dsp == nil {
		return
	}
	d := &a.dsp.mt
	d.mu.Lock()
	frames, pk, recAt := d.frames, d.pk, d.recAt
	doneSeq, doneFrames, donePath, full := d.doneSeq, d.doneFrames, d.donePath, d.full
	errSeq, errMsg := d.errSeq, d.err
	pp, pt, pAt, endSeq := d.playPos, d.playTotal, d.playAt, d.endSeq
	d.mu.Unlock()
	if a.mt.rec {
		if frames != a.mt.frames {
			a.mt.frames = frames
			a.dirty = true
		}
		for i := range pk {
			a.mt.pk[i] = math.Max(pk[i], a.mt.pk[i]*0.85)
		}
		if a.mt.limit > 0 && frames >= a.mt.limit {
			a.mtStop()
		}
		if time.Since(recAt) > 3*time.Second && time.Since(a.mt.started) > 3*time.Second {
			// the engine restarted: the take on the card is good up to the last second
			a.mt.rec, a.mt.stopping, a.mt.ask = false, false, true
			a.say("RECORDING STOPPED  ·  THE TAKE IS ON THE CARD")
			a.log("MIXTAPE_LOST frames=%d", a.mt.frames)
		}
	}
	if errSeq != a.mt.errSeq {
		a.mt.errSeq = errSeq
		switch {
		case strings.HasPrefix(errMsg, "MTERR open"), strings.HasPrefix(errMsg, "MTERR thread"):
			a.mt.rec, a.mt.stopping = false, false
			a.say("COULDN'T START RECORDING ON THE CARD")
		case strings.HasPrefix(errMsg, "MTERR write"):
			a.say("THE CARD STOPPED WRITING  ·  THE TAKE IS KEPT UP TO THERE")
		case strings.HasPrefix(errMsg, "MTERR play"):
			a.mt.playing = false
			a.say("CAN'T PLAY THAT TAKE")
		case strings.HasPrefix(errMsg, "MTERR busy"):
			a.mt.rec, a.mt.stopping = false, false
			a.say("STILL FINISHING THE LAST TAKE  ·  TRY AGAIN")
		}
	}
	if doneSeq != a.mt.doneSeq {
		a.mt.doneSeq = doneSeq
		a.mt.rec, a.mt.stopping = false, false
		a.mt.frames = doneFrames
		if donePath != "" {
			a.mt.path = donePath
		}
		d.mu.Lock()
		drops := d.drops
		d.mu.Unlock()
		a.log("MIXTAPE_DONE frames=%d secs=%.1f full=%t drops=%d", doneFrames, float64(doneFrames)/48000, full, drops)
		if doneFrames < 24000 {
			os.Remove(a.mt.path)
			os.Remove(peaksPath(a.mt.path))
			a.mt.ask = false
			a.say("TOO SHORT  ·  NOTHING KEPT")
		} else {
			a.mt.ask = true
			a.mtShow(true)
			if full {
				a.say("THE TAPE IS FULL  ·  KEEP " + a.mt.name + "?")
			} else {
				a.say("KEEP " + a.mt.name + "?")
			}
		}
	}
	if a.mt.playing {
		if pp != a.mt.playPos && pAt.After(a.mt.playFrom) {
			a.mt.playPos, a.mt.playTotal = pp, pt
			a.dirty = true
		}
		if endSeq != a.mt.endSeq {
			a.mt.endSeq = endSeq
			a.mt.playing = false
			a.dirty = true
		}
	} else {
		a.mt.endSeq = endSeq
	}
	// levels for the mixer dots (and the cassette while listening)
	want := a.mt.view || (a.mode == modeTracks && a.ui.mixer && a.smp.view == smpOff)
	if want != a.mt.lvOn {
		a.mt.lvOn = want
		a.dsp.send("LEVELS %d", b2i(want))
	}
}

func peaksPath(wav string) string {
	dir, name := filepath.Split(wav)
	return filepath.Join(dir, ".peaks", strings.TrimSuffix(name, filepath.Ext(name))+".pk")
}

// mtPeaks: a take's overview (one level per 0.1 s), cached while unchanged.
func (a *app) mtPeaks(wav string) []byte {
	p := peaksPath(wav)
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if a.mt.peaks == nil {
		a.mt.peaks, a.mt.peaksSz = map[string][]byte{}, map[string]int64{}
	}
	if a.mt.peaksSz[p] != st.Size() {
		b, _ := os.ReadFile(p)
		a.mt.peaks[p], a.mt.peaksSz[p] = b, st.Size()
	}
	return a.mt.peaks[p]
}

func (a *app) mtListen(path string, from float64) {
	a.dsp.send("MTPLAY %.2f %s", math.Max(0, from), path)
	a.mt.playFrom = time.Now()
	a.mt.playing, a.mt.playPath = true, path
	a.mt.playPos, a.mt.playTotal = int(from*48000), int(wavSeconds(path)*48000)
	a.log("MIXTAPE_LISTEN %s from=%.1f", filepath.Base(path), from)
	a.animate(time.Now().Add(500 * time.Millisecond))
	a.dirty = true
}

func (a *app) mtListenStop() {
	if a.mt.playing && a.dsp != nil {
		a.dsp.send("MTPLAYSTOP")
	}
	a.mt.playing = false
}

// mtListenTake: the LIBRARY's middle button on a take: LISTEN from START / stop.
func (a *app) mtListenTake(e cardEntry) {
	if a.mt.playing && a.mt.playPath == e.path {
		a.mtListenStop()
		a.say("STOPPED")
		return
	}
	a.mtListen(e.path, a.takeFrom(e))
	a.say("LISTENING FROM " + clock(a.takeFrom(e)) + "  ·  MIDDLE: STOP")
}

// mtButton: buttons while the cassette is on screen. Returns true when used.
func (a *app) mtButton(note byte, down bool) bool {
	if !a.mt.view || a.mode == modeConfirmExit {
		return false
	}
	switch note {
	case controls.BtnShift, controls.BtnPlay, controls.BtnStop, controls.BtnTapTempo, controls.BtnPlus,
		controls.BtnMinus, controls.BtnSeqRecord, controls.BtnSampleRecord, controls.BtnNoteRepeat, controls.BtnPadFX:
		return false // these keep working (PAD FX opens the LIBRARY)
	case controls.BtnScreenLeft, controls.BtnScreenMiddle, controls.BtnScreenRight:
		if down {
			a.mtScreen(note)
		}
		return true
	case controls.BtnDataPush:
		return true
	}
	// any other button: put the cassette away, then do what it normally does
	if down {
		a.mtShow(false)
		a.mtListenStop()
	}
	return false
}

func (a *app) mtScreen(note byte) {
	if a.mt.rec || a.mt.stopping {
		a.say("RECORDING  ·  SAMPLE RECORD: STOP")
		return
	}
	if !a.mt.ask {
		if note == controls.BtnScreenMiddle && a.mt.path != "" {
			if _, err := os.Stat(a.mt.path); err == nil {
				if a.mt.playing {
					a.mtListenStop()
				} else {
					a.mtListen(a.mt.path, 0)
				}
				return
			}
		}
		a.say("SAMPLE RECORD: NEW TAKE  ·  PAD FX: THE LIBRARY")
		return
	}
	switch note {
	case controls.BtnScreenLeft: // DELETE, twice
		if time.Since(a.mt.delArm) > 3*time.Second {
			a.mt.delArm = time.Now()
			a.say("PRESS DELETE AGAIN TO THROW " + a.mt.name + " AWAY")
			a.animate(time.Now().Add(3 * time.Second))
			break
		}
		a.mtListenStop()
		os.Remove(a.mt.path)
		os.Remove(peaksPath(a.mt.path))
		a.log("MIXTAPE_DELETE %s", a.mt.path)
		a.say(a.mt.name + " DELETED")
		a.mt.ask, a.mt.path, a.mt.delArm = false, "", time.Time{}
		a.smp.scanned = time.Time{}
	case controls.BtnScreenMiddle: // LISTEN / stop
		if a.mt.playing {
			a.mtListenStop()
		} else {
			a.mtListen(a.mt.path, 0)
			a.say("LISTENING  ·  DATA: JUMP 5 S")
		}
	case controls.BtnScreenRight: // SAVE (it's already on the card; this is the yes)
		a.mtListenStop()
		a.mt.ask, a.mt.saved = false, a.mt.path
		a.smp.scanned = time.Time{}
		a.log("MIXTAPE_SAVE %s", a.mt.path)
		a.say("SAVED " + a.mt.name + "  ·  PAD FX: CHOP IT")
	}
	a.dirty = true
}

// mtData: DATA while the cassette is up: jump through the take being heard.
func (a *app) mtDataTurn(d2 byte) {
	if !a.mt.playing {
		return
	}
	d := 5.0
	if d2 > 64 {
		d = -5
	}
	pos := float64(a.mt.playPos)/48000 + d
	if a.mt.playTotal > 0 {
		pos = math.Min(pos, float64(a.mt.playTotal)/48000-1)
	}
	a.mtListen(a.mt.playPath, math.Max(0, pos))
}

// ---------------------------------------------------------------- drawing

// mtBadge: on every screen while recording, a red dot and the time.
func (a *app) mtBadge(c *gfx.Canvas) {
	if !a.mt.rec && !a.mt.stopping {
		return
	}
	secs := float64(a.mt.frames) / 48000
	c.RoundRect(158, 5, 52, 17, 8.5, 0x3A1512, 1)
	on := a.mt.stopping || int(time.Since(a.mt.started).Seconds()*2)%2 == 0
	if on {
		c.Circle(167, 13.5, 3.2, uRed, 1)
	}
	c.Dots(174, 8.5, clock(secs), dotGlyph, 1.42, uRed, 0x3A1512)
	a.animate(time.Now().Add(600 * time.Millisecond))
}

func (a *app) drawMixtape(c *gfx.Canvas) {
	now := time.Now()
	dt := 0.0
	if !a.mt.lastT.IsZero() {
		dt = math.Min(0.1, now.Sub(a.mt.lastT).Seconds())
	}
	a.mt.lastT = now
	name := a.mt.name
	if name == "" {
		name = "NO TAKE YET"
	}
	c.Text(10, 38, "MIXTAPE", gfx.Big, uText, gfx.Left|gfx.Mid)
	c.Text(310, 38, name, gfx.Small, pLilac, gfx.Right|gfx.Mid)
	// how far along: recording = the take so far of the limit; listening = the position
	secs := float64(a.mt.frames) / 48000
	limit := float64(a.mt.limit) / 48000
	if limit <= 0 {
		limit = mtMaxSecs
	}
	frac := math.Min(1, secs/limit)
	moving := a.mt.rec
	if a.mt.playing && a.mt.playTotal > 0 {
		frac = float64(a.mt.playPos) / float64(a.mt.playTotal)
		secs = float64(a.mt.playPos) / 48000
		moving = true
	}
	if moving {
		a.mt.angle += dt * 2.4
		a.animate(now.Add(120 * time.Millisecond))
	}
	// the cassette shell
	const cx, cy = 160.0, 104.0
	shell, ink := uint32(0xE9E3D6), uint32(0x33284D)
	c.RoundRect(cx-120, cy-50, 240, 100, 8, shell, 1)
	c.RoundRect(cx-108, cy-42, 216, 28, 3, pLilac, 1) // the label
	label := "FM-STEP  ·  JAM"
	if t := a.mt.started; t.Year() >= 2025 {
		label += "  ·  " + strings.ToUpper(t.Format("2 Jan"))
	}
	c.Text(cx-100, cy-34, label, gfx.Small, ink, gfx.Left|gfx.Mid)
	c.Line(cx-100, cy-23, cx+100, cy-23, 0.8, ink, 0.5)
	c.Text(cx+100, cy-34, "A", gfx.Title, ink, gfx.Right|gfx.Mid)
	// the window with the two reels: the tape moves from left to right
	c.RoundRect(cx-74, cy-8, 148, 38, 18, 0x2A2A28, 1)
	for k, rx := range []float64{cx - 44, cx + 44} {
		tape := 9 + 12*(1-frac)
		if k == 1 {
			tape = 9 + 12*frac
		}
		c.Circle(rx, cy+11, tape, 0x3B2A1E, 1)
		c.Circle(rx, cy+11, 7, shell, 1)
		for s := 0; s < 6; s++ {
			ang := float64(s)*math.Pi/3 + a.mt.angle*(1.6-0.6*float64(k))
			c.Line(rx+3*math.Cos(ang), cy+11+3*math.Sin(ang), rx+6.5*math.Cos(ang), cy+11+6.5*math.Sin(ang), 1.6, 0x2A2A28, 1)
		}
	}
	for _, p := range [][2]float64{{cx - 112, cy - 44}, {cx + 112, cy - 44}, {cx - 112, cy + 44}, {cx + 112, cy + 44}} {
		c.Circle(p[0], p[1], 2, 0xB8B0A0, 1)
	}
	c.Poly([]float64{cx - 50, cx + 50, cx + 42, cx - 42}, []float64{cy + 50, cy + 50, cy + 37, cy + 37}, 0xD6CEBE, 1)
	// counter and state
	c.Dots(12, 160, clock(secs), dotGlyph, 3.2, uText, 0x26271F)
	switch {
	case a.mt.rec:
		if int(time.Since(a.mt.started).Seconds()*2)%2 == 0 {
			c.Glow(126, 166, 9, uRed, 0.6)
			c.Circle(126, 166, 4, uRed, 1)
		}
		c.Text(136, 166, "REC", gfx.Label, uRed, gfx.Left|gfx.Mid)
	case a.mt.stopping:
		c.Text(122, 166, "FINISHING", gfx.Label, uDim, gfx.Left|gfx.Mid)
	case a.mt.playing:
		c.Poly([]float64{122, 122, 130}, []float64{161, 171, 166}, uLime, 1)
		c.Text(136, 166, "LISTEN", gfx.Label, uLime, gfx.Left|gfx.Mid)
	default:
		c.Text(122, 166, "STOPPED", gfx.Label, uDim, gfx.Left|gfx.Mid)
	}
	if a.mt.rec || a.mt.stopping {
		c.Text(310, 160, clock(math.Max(0, limit-secs))+" LEFT ON THE TAPE", gfx.Small, uDim, gfx.Right|gfx.Mid)
	} else if a.mt.playing && a.mt.playTotal > 0 {
		c.Text(310, 160, "OF "+clock(float64(a.mt.playTotal)/48000), gfx.Small, uDim, gfx.Right|gfx.Mid)
	}
	if a.mt.free > 0 {
		c.Text(310, 172, fmt.Sprintf("CARD: %.1f GB FREE", float64(a.mt.free)/(1<<30)), gfx.Small, uFaint, gfx.Right|gfx.Mid)
	}
	// level: the take's peaks while recording, the mix otherwise
	lv := a.mt.pk
	if !a.mt.rec {
		a.dsp.mt.mu.Lock()
		m := float64(a.dsp.mt.lv[8]) / 255
		a.dsp.mt.mu.Unlock()
		lv = [2]float64{math.Pow(10, (m*60-60)/20), math.Pow(10, (m*60-60)/20)}
	}
	for i := 0; i < 2; i++ {
		y := 182 + float64(i)*5
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
	if a.mt.ask {
		c.RoundRect(40, 58, 240, 96, 10, uBG, 0.92)
		c.RoundRectLine(40, 58, 240, 96, 10, 1, uFaint, 1)
		c.Text(160, 78, "KEEP "+a.mt.name+"?", gfx.Title, uText, gfx.Center|gfx.Mid)
		tsecs := float64(a.mt.frames) / 48000
		c.Text(160, 96, fmt.Sprintf("%d MIN %d S  ·  %d MB", int(tsecs)/60, int(tsecs)%60, a.mt.frames*4>>20), gfx.Small, uDim, gfx.Center|gfx.Mid)
		del := "DELETE"
		if time.Since(a.mt.delArm) < 3*time.Second {
			del = "SURE?"
		}
		lis := "LISTEN"
		if a.mt.playing {
			lis = "STOP"
		}
		for i, l := range []string{del, lis, "SAVE"} {
			x := 82 + float64(i)*78
			col, fg := uint32(uPanel2), uint32(uDim)
			switch {
			case i == 2:
				col, fg = uLime, uBG
			case i == 0 && del == "SURE?":
				col, fg = uRed, uBG
			case i == 1 && a.mt.playing:
				col, fg = uText, uBG
			}
			c.RoundRect(x-32, 118, 64, 22, 11, col, 1)
			c.Text(x, 129, l, gfx.Label, fg, gfx.Center|gfx.Mid)
		}
		a.uiBottom(c, "SCREEN BUTTONS: DELETE · LISTEN · SAVE")
		return
	}
	switch {
	case a.mt.rec:
		a.uiBottom(c, "SAMPLE RECORD: STOP  ·  KEEP PLAYING, IT ALL GOES ON THE TAPE")
	case a.mt.playing:
		a.uiBottom(c, "MIDDLE: STOP  ·  DATA: JUMP 5 S")
	case a.mt.path != "":
		a.uiBottom(c, "SAMPLE RECORD: NEW TAKE  ·  MIDDLE: LISTEN  ·  PAD FX: CHOP")
	default:
		a.uiBottom(c, "SAMPLE RECORD: START A TAKE (UP TO 20 MIN)")
	}
}
