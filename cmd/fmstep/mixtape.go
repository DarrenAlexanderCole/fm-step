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
	recent     []float64 // the mix level, ~30 a second (-48..0 dB as 0..1), newest last
	recEnv     []float64 // the take being recorded, one level per report (~0.1 s)
}

func db48(lin float64) float64 { return math.Max(0, math.Min(1, (20*math.Log10(lin+1e-6)+48)/48)) }

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
			d.recent = append(d.recent, math.Max(0, (float64(d.lv[8])/255*60-12)/48))
			if len(d.recent) > 300 {
				d.recent = append(d.recent[:0], d.recent[len(d.recent)-150:]...)
			}
			d.mu.Unlock()
		}
		return true
	case "MTR":
		if len(f) == 5 {
			d.mu.Lock()
			d.pk[0], _ = strconv.ParseFloat(f[2], 64)
			d.pk[1], _ = strconv.ParseFloat(f[3], 64)
			nf, _ := strconv.Atoi(f[1])
			if nf > d.frames { // not while paused
				d.recEnv = append(d.recEnv, db48(math.Max(d.pk[0], d.pk[1])))
			}
			d.frames = nf
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
	view        bool // the cassette is on screen
	rec         bool // a take is being recorded
	stopping    bool // MTSTOP sent, waiting for MTDONE
	ask         bool // stopped: DELETE · LISTEN · SAVE
	path        string
	name        string // "TAKE 007"
	frames      int
	limit       int // frames: 20 minutes, or what fits on the card
	free        uint64
	started     time.Time
	delArm      time.Time
	saved       string // the LIBRARY selects this take next time it opens
	recDown     time.Time
	recUsed     bool
	playing     bool // a take is being listened to (or paused in it)
	lpaused     bool // listening, paused
	paused      bool // recording, paused
	seekTo      float64
	seekAt      time.Time
	seekPending bool
	playPath    string
	playPos     int
	playTotal   int
	playFrom    time.Time // when LISTEN last (re)started
	doneSeq     int
	errSeq      int
	endSeq      int
	angle       float64
	lastT       time.Time
	pk          [2]float64
	// 0.9.24: trimming a finished take, and the knobs
	trimIn, trimOut float64 // seconds (trimOut 0 = the end)
	loopIdx         int
	fine            bool
	trimFocus       int
	acc             [3]int
	knobK           int
	knobAt          time.Time
	saving          bool
	saveMsg         chan string
	// 0.9.25
	height, zoom      float64
	selOut, monitor   bool
	lineKey           string
	lineMin, lineMax  []float64
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
	a.mt.rec, a.mt.stopping, a.mt.ask, a.mt.paused = true, false, false, false
	a.mt.path, a.mt.name = path, fmt.Sprintf("TAKE %03d", n)
	a.mt.frames, a.mt.limit, a.mt.free = 0, secs*48000, free
	a.mt.started = time.Now()
	a.mt.delArm = time.Time{}
	d := &a.dsp.mt
	d.mu.Lock()
	d.frames, d.recAt, d.full, d.recEnv = 0, time.Now(), false, nil
	d.mu.Unlock()
	a.mtSendRec()
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
			a.mtResetTrim()
			a.mtShow(true)
			if full {
				a.say("THE TAPE IS FULL  ·  KEEP " + a.mt.name + "?")
			} else {
				a.say("KEEP " + a.mt.name + "?")
			}
		}
	}
	if a.mt.saving {
		select {
		case m := <-a.mt.saveMsg:
			a.mt.saving = false
			if m == "OK" {
				a.mt.ask, a.mt.saved = false, a.mt.path
				a.mt.frames = int(wavSeconds(a.mt.path) * 48000)
				a.mt.peaks = nil
				a.smp.scanned = time.Time{}
				a.log("MIXTAPE_SAVE %s trimmed", a.mt.path)
				a.say("SAVED " + a.mt.name + " (TRIMMED)  ·  PAD FX: CHOP IT")
				a.mtResetTrim()
			} else {
				a.log("MIXTAPE_TRIM_FAILED %s", m)
				a.say("COULDN'T TRIM  ·  THE WHOLE TAKE IS STILL ON THE CARD")
			}
			a.dirty = true
		default:
		}
	}
		if a.mt.playing && a.mt.seekPending && time.Since(a.mt.seekAt) > 120*time.Millisecond {
		a.mt.seekPending = false
		a.mtListen(a.mt.playPath, a.mt.seekTo)
	}
	if a.mt.playing {
		if pp != a.mt.playPos && pAt.After(a.mt.playFrom) {
			a.mt.playPos, a.mt.playTotal = pp, pt
			a.dirty = true
		}
		if endSeq != a.mt.endSeq {
			a.mt.endSeq = endSeq
			a.mt.playing, a.mt.lpaused = false, false
			if a.mt.ask {
				a.say("THE END  ·  KEEP " + a.mt.name + "?")
			}
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
	a.mtSendLoop()
	a.dsp.send("MTPLAY %.2f %s", math.Max(0, from), path)
	if a.mt.lpaused {
		a.dsp.send("MTPPAUSE 1") // a jump while paused stays paused
	}
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
	a.mt.playing, a.mt.lpaused, a.mt.seekPending = false, false, false
}

// mtListenTake: the LIBRARY's middle button on a take: LISTEN from START / stop.
func (a *app) mtListenTake(e cardEntry) {
	if a.mt.playing && a.mt.playPath == e.path {
		a.mtListenStop()
		a.say("STOPPED")
		return
	}
	a.mt.lpaused = false
	a.mtListen(e.path, a.takeFrom(e))
	a.say("LISTENING FROM " + clock(a.takeFrom(e)) + "  ·  MIDDLE: STOP")
}

// mtPos: where the listening head is (seconds), counting a jump not yet sent.
func (a *app) mtPos() float64 {
	if a.mt.seekPending {
		return a.mt.seekTo
	}
	return a.mtShownPos(float64(a.mt.playPos) / 48000)
}

// mtNudge: move the listening head (sent once the turning or pressing settles).
func (a *app) mtNudge(d float64) {
	total := float64(a.mt.playTotal) / 48000
	to := math.Max(0, a.mtPos()+d)
	if total > 0 {
		to = math.Min(to, math.Max(0, total-0.5))
	}
	a.mt.seekTo, a.mt.seekAt, a.mt.seekPending = to, time.Now(), true
	a.dirty = true
}

func (a *app) mtPauseListen(on bool) {
	a.mt.lpaused = on
	a.dsp.send("MTPPAUSE %d", b2i(on))
	a.dirty = true
}

// mtButton: buttons while SUPA TAPE is on screen. Returns true when used.
func (a *app) mtButton(note byte, down bool) bool {
	if !a.mt.view || a.mode == modeConfirmExit {
		return false
	}
	switch note {
	case controls.BtnStop:
		if a.mt.playing { // STOP ends listening (the sequencer is left alone)
			if down {
				a.mtListenStop()
				if a.mt.ask {
					a.say("KEEP " + a.mt.name + "?")
				} else {
					a.say("STOPPED")
				}
			}
			return true
		}
		return false
	case controls.BtnPlay:
		if a.mtTrimming() && a.shift { // 0.9.25: SHIFT + PLAY saves the take (trimmed to IN-OUT)
			if down {
				a.mtSave()
			}
			return true
		}
		return false
	case controls.BtnErase:
		if a.mtTrimming() { // ERASE twice: delete the take
			if down {
				a.mtDelete()
			}
			return true
		}
		return false
	case controls.BtnShift, controls.BtnTapTempo, controls.BtnPlus,
		controls.BtnMinus, controls.BtnSeqRecord, controls.BtnSampleRecord, controls.BtnNoteRepeat, controls.BtnPadFX:
		return false // these keep working (PAD FX opens the LIBRARY)
	case controls.BtnScreenLeft, controls.BtnScreenMiddle, controls.BtnScreenRight:
		if down {
			a.mtScreen(note)
		}
		return true
	case controls.BtnDataPush:
		if down {
			a.mtDataPush()
		}
		return true
	}
	// any other button: put the tape away, then do what it normally does
	if down {
		a.mtShow(false)
		a.mtListenStop()
	}
	return false
}

func (a *app) mtScreen(note byte) {
	defer func() { a.dirty = true }()
	if a.mt.stopping {
		return
	}
	if a.mt.rec { // recording: the middle button pauses
		if note == controls.BtnScreenMiddle {
			a.mt.paused = !a.mt.paused
			a.dsp.send("MTPAUSE %d", b2i(a.mt.paused))
			a.log("MIXTAPE_PAUSE %t at=%.1f", a.mt.paused, float64(a.mt.frames)/48000)
			if a.mt.paused {
				a.say("PAUSED  ·  MIDDLE: CARRY ON RECORDING")
			} else {
				a.say("RECORDING " + a.mt.name)
			}
		} else if note == controls.BtnScreenLeft {
			a.mtMonitor(!a.mt.monitor)
		} else {
			a.say("LEFT: MONITOR  ·  MIDDLE: PAUSE  ·  SAMPLE RECORD: STOP")
		}
		return
	}
	if a.mt.playing && !a.mt.ask { // listening (a LIBRARY take): the transport
		jump := 5.0
		if a.shift {
			jump = 30
		}
		switch note {
		case controls.BtnScreenLeft:
			a.mtNudge(-jump)
		case controls.BtnScreenRight:
			a.mtNudge(jump)
		case controls.BtnScreenMiddle:
			a.mtPauseListen(!a.mt.lpaused)
		}
		return
	}
	if !a.mt.ask {
		if note == controls.BtnScreenLeft {
			a.mtMonitor(!a.mt.monitor)
			return
		}
		if note == controls.BtnScreenMiddle && a.mt.path != "" {
			if _, err := os.Stat(a.mt.path); err == nil {
				a.mt.lpaused = false
				a.mtListen(a.mt.path, 0)
				return
			}
		}
		a.say("SAMPLE RECORD: NEW TAKE  ·  PAD FX: THE LIBRARY")
		return
	}
	switch note { // 0.9.25: left / right pick IN / OUT, the middle plays or pauses
	case controls.BtnScreenLeft:
		a.mtSelect(false)
	case controls.BtnScreenRight:
		a.mtSelect(true)
	case controls.BtnScreenMiddle:
		if a.mt.playing {
			a.mtPauseListen(!a.mt.lpaused)
		} else {
			from := a.mt.trimIn
			if a.mt.playPos > 0 && float64(a.mt.playPos)/48000 > from && float64(a.mt.playPos)/48000 < a.mtOut() {
				from = float64(a.mt.playPos) / 48000
			}
			a.mtStartListen(from)
			a.say("LISTENING  ·  MIDDLE: PAUSE  ·  SHIFT + PLAY: SAVE")
		}
	}
}

// mtDelete: ERASE twice throws the take away.
func (a *app) mtDelete() {
	if time.Since(a.mt.delArm) > 3*time.Second {
		a.mt.delArm = time.Now()
		a.say("PRESS ERASE AGAIN TO THROW " + a.mt.name + " AWAY")
		a.animate(time.Now().Add(3 * time.Second))
		return
	}
	a.mtListenStop()
	os.Remove(a.mt.path)
	os.Remove(peaksPath(a.mt.path))
	a.log("MIXTAPE_DELETE %s", a.mt.path)
	a.say(a.mt.name + " DELETED")
	a.mt.ask, a.mt.path, a.mt.delArm = false, "", time.Time{}
	a.smp.scanned = time.Time{}
	a.dirty = true
}

// mtSave: SHIFT + PLAY keeps the take, trimmed to IN-OUT when it was trimmed.
func (a *app) mtSave() {
	a.mtListenStop()
	if total := a.mtTotal(); a.mt.trimIn > 0.01 || (a.mt.trimOut > 0 && a.mt.trimOut < total-0.01) {
		if a.mt.saving {
			return
		}
		a.mt.saving = true
		a.mt.saveMsg = make(chan string, 1)
		path, in, out, name, ch := a.mt.path, a.mt.trimIn, a.mtOut(), a.mt.name, a.mt.saveMsg
		a.say("SAVING " + name + " TRIMMED...")
		a.log("MIXTAPE_TRIM %s in=%.3f out=%.3f", path, in, out)
		go func() {
			if err := mtSaveTrimmed(path, in, out); err != nil {
				ch <- "ERR " + err.Error()
				return
			}
			ch <- "OK"
		}()
		return
	}
	a.mt.ask, a.mt.saved = false, a.mt.path
	a.smp.scanned = time.Time{}
	a.log("MIXTAPE_SAVE %s", a.mt.path)
	a.say("SAVED " + a.mt.name + "  ·  CHOP: THE LIBRARY")
	a.dirty = true
}

// mtDataTurn: DATA while listening jogs the head: 1 s a click (SHIFT: 10 s).
func (a *app) mtDataTurn(d2 byte) {
	if a.mtTrimming() { // 0.9.25: DATA moves the picked marker (IN or OUT)
		c := 1
		if d2 > 64 {
			c = -1
		}
		a.mtMoveMarker(c)
		return
	}
	if !a.mt.playing {
		return
	}
	d := 1.0
	if a.shift {
		d = 10
	}
	if a.mt.fine {
		d /= 10
	}
	if d2 > 64 {
		d = -d
	}
	a.mtNudge(d)
}

// ---------------------------------------------------------------- drawing

// mtBadge: on every screen while recording, a red light and the time.
func (a *app) mtBadge(c *gfx.Canvas) {
	if !a.mt.rec && !a.mt.stopping {
		return
	}
	secs := float64(a.mt.frames) / 48000
	c.RoundRect(158, 5, 52, 17, 8.5, 0x3A1512, 1)
	switch {
	case a.mt.paused:
		drawIcon(c, icPause, 163.5, 13.5, uRed)
	case a.mt.stopping || int(time.Since(a.mt.started).Seconds()*2)%2 == 0:
		c.Circle(167, 13.5, 3.2, uRed, 1)
	}
	c.Dots(174, 8.5, clock(secs), dotGlyph, 1.42, uRed, 0x3A1512)
	a.animate(time.Now().Add(600 * time.Millisecond))
}

// mtSupa: the live recorder as the SUPA TAPE picture.
func (a *app) mtSupa() supa {
	s := supa{name: a.mt.name, angle: a.mt.angle}
	if s.name == "" {
		s.name = "NO TAKE YET"
	}
	limit := float64(a.mt.limit) / 48000
	if limit <= 0 {
		limit = mtMaxSecs
	}
	s.limit = limit
	s.free = float64(a.mt.free) / (1 << 30)
	d := &a.dsp.mt
	d.mu.Lock()
	recent := append([]float64(nil), d.recent...)
	recEnv := append([]float64(nil), d.recEnv...)
	m := float64(d.lv[8]) / 255
	d.mu.Unlock()
	if len(recent) > 150 { // the last ~5 s
		recent = recent[len(recent)-150:]
	}
	s.recent = recent
	switch {
	case a.mt.rec || a.mt.stopping:
		s.state = supaRec
		if a.mt.paused {
			s.state = supaRecPaused
		}
		s.secs = float64(a.mt.frames) / 48000
		s.env = recEnv
		s.level = a.mt.pk
	case a.mt.playing:
		s.state = supaListen
		if a.mt.lpaused {
			s.state = supaListenPaused
		}
		s.secs = a.mtPos()
		s.total = float64(a.mt.playTotal) / 48000
		if pk := a.mtPeaks(a.mt.playPath); len(pk) > 0 {
			s.env = make([]float64, len(pk))
			for i, b := range pk {
				s.env[i] = float64(b) / 255
			}
		}
		if n := s.name; a.mt.playPath != a.mt.path || n == "NO TAKE YET" {
			s.name = strings.ToUpper(strings.TrimSuffix(filepath.Base(a.mt.playPath), filepath.Ext(a.mt.playPath)))
		}
	default: // stopped: the last take, ready to listen to
		if a.mt.path != "" {
			s.total = float64(a.mt.frames) / 48000
			if pk := a.mtPeaks(a.mt.path); len(pk) > 0 {
				s.env = make([]float64, len(pk))
				for i, b := range pk {
					s.env[i] = float64(b) / 255
				}
			}
		}
	}
	if !a.mt.rec {
		lv := math.Pow(10, (m*60-60)/20)
		s.level = [2]float64{lv, lv}
	}
	s.knobs = a.mtKnobRow()
	s.knobHot = -1
	if time.Since(a.mt.knobAt) < 2*time.Second {
		s.knobHot = a.mt.knobK
	}
	if a.mtTrimming() {
		s.trimming = true
		if s.total <= 0 {
			s.total = a.mtTotal()
		}
		if s.state == supaIdle && s.secs == 0 {
			s.secs = a.mt.trimIn
		}
		s.trimIn, s.trimOut = a.mt.trimIn, a.mtOut()
		s.loopA, s.loopB, s.loop = a.mtLoopRange()
		// 0.9.25: K3 ZOOM sets the window, centred on the picked marker; K1 HEIGHT
		s.window = a.mtZoom()
		s.focusT = s.trimIn
		if a.mt.selOut {
			s.focusT = s.trimOut
		}
		if s.window >= s.total {
			s.focusT = s.total / 2
		}
		s.height = a.mtHeight()
		s.selOut = a.mt.selOut
		t0 := math.Max(0, math.Min(s.total-s.window, s.focusT-s.window/2))
		s.lineMin, s.lineMax = a.mtLine(a.mt.path, t0, s.window, 296)
	}
	return s
}

func (a *app) drawMixtape(c *gfx.Canvas) {
	now := time.Now()
	dt := 0.0
	if !a.mt.lastT.IsZero() {
		dt = math.Min(0.1, now.Sub(a.mt.lastT).Seconds())
	}
	a.mt.lastT = now
	s := a.mtSupa()
	if s.moving() {
		a.mt.angle += dt * 2.4
		s.angle = a.mt.angle
	}
	a.animate(now.Add(120 * time.Millisecond))
	blink := int(time.Since(a.mt.started).Seconds()*2)%2 == 0
	// what the screen buttons do
	var b [3]supaBtn
	hint := ""
	mon := supaBtn{icon: icText, text: "MONITOR", on: a.mt.monitor}
	switch {
	case a.mt.rec:
		if a.mt.paused {
			b[1] = supaBtn{icon: icRec, text: "CARRY ON", on: true}
			hint = "MIDDLE: CARRY ON  ·  SAMPLE RECORD: STOP"
		} else {
			b[1] = supaBtn{icon: icPause, text: "PAUSE"}
			hint = "LEFT: MONITOR  ·  MIDDLE: PAUSE  ·  SAMPLE RECORD: STOP"
		}
		b[0] = mon
	case a.mt.stopping:
		hint = "FINISHING THE TAKE..."
	case a.mt.ask: // 0.9.25: IN · PLAY · OUT
		mid := supaBtn{icon: icPlay, text: "PLAY"}
		if a.mt.playing && !a.mt.lpaused {
			mid = supaBtn{icon: icPause, text: "PAUSE"}
		}
		b = [3]supaBtn{{icon: icText, text: "IN", on: !a.mt.selOut}, mid, {icon: icText, text: "OUT", on: a.mt.selOut}}
		hint = "DATA MOVES IT  ·  PUSH: FINE  ·  SHIFT + PLAY: SAVE  ·  ERASE x2: DELETE"
		if time.Since(a.mt.delArm) < 3*time.Second {
			hint = "ERASE AGAIN: DELETE " + a.mt.name
		}
	case a.mt.playing:
		jump := "5 S"
		if a.shift {
			jump = "30 S"
		}
		mid := supaBtn{icon: icPause, text: "PAUSE"}
		if a.mt.lpaused {
			mid = supaBtn{icon: icPlay, text: "PLAY", on: true}
		}
		b = [3]supaBtn{{icon: icRew, text: jump}, mid, {icon: icFF, text: jump}}
		hint = "DATA: JOG 1 S (SHIFT 10 S)  ·  STOP: DONE"
	case a.mt.path != "":
		b[0], b[1] = mon, supaBtn{icon: icPlay, text: "LISTEN"}
		hint = "SAMPLE RECORD: NEW TAKE  ·  CHOP: THE LIBRARY"
	default:
		b[0] = mon
		hint = "SAMPLE RECORD: START A TAKE (UP TO 20 MIN)"
	}
	supaButtons(c, b)
	drawRecorder(c, &s, blink, tcol(a.proj.Track))
	supaLevel(c, s.level)
	untouched := a.mt.trimIn == 0 && a.mt.trimOut == 0 && a.mt.loopIdx == 0 && !a.mt.fine && a.mt.knobAt.IsZero() && !a.mt.selOut
	if a.mt.ask && !a.mt.playing && untouched && !a.mt.saving {
		c.RoundRect(52, 92, 216, 52, 10, uBG, 0.94)
		c.RoundRectLine(52, 92, 216, 52, 10, 1, uFaint, 1)
		c.Text(160, 110, "KEEP "+a.mt.name+"?", gfx.Title, uText, gfx.Center|gfx.Mid)
		tsecs := float64(a.mt.frames) / 48000
		c.Text(160, 129, fmt.Sprintf("%d MIN %d S  ·  %d MB  ·  SHIFT + PLAY KEEPS IT", int(tsecs)/60, int(tsecs)%60, a.mt.frames*4>>20), gfx.Small, uDim, gfx.Center|gfx.Mid)
	}
	a.uiBottom(c, hint)
}
