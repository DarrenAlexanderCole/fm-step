package main

// The MIXTAPE knobs (0.9.24).
//
// Recording (and before a take): K1 REC LEVEL (-24..+12 dB), K2 PAD
// (0 / -10 / -20 / -30 / -40 dB), K3 COMP (OFF, GLUE, PUNCH, LIMIT). They act on
// the take only, never on the mix you hear, and are kept between sessions.
//
// A finished take (0.9.25): the left / right screen buttons pick IN / OUT and
// DATA moves it (big moves; DATA push = FINE, tiny steps); the middle button
// plays / pauses. K1 HEIGHT, K2 LOOP (1/4 beat .. 4 bars from the playhead, OUT
// follows), K3 ZOOM. SHIFT + PLAY saves (trimmed to IN-OUT), ERASE twice deletes.
// Recording or idle: the left button is MONITOR (hear level, pad and comp).

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

var (
	mtPads      = []float64{0, -10, -20, -30, -40}
	mtComps     = []string{"OFF", "GLUE", "PUNCH", "LIMIT"}
	mtLoopBeats = []float64{0, 0.25, 0.5, 1, 2, 4, 8, 16}
	mtLoopNames = []string{"OFF", "1/4", "1/2", "1 BEAT", "2 BEATS", "1 BAR", "2 BARS", "4 BARS"}
)

func init() {
	charDefaults["mt_lvl"] = 24.0 / 36 // 0 dB on a -24..+12 scale
	charDefaults["mt_pad"] = 0
	charDefaults["mt_comp"] = 0
}

func (a *app) mtLevelDb() float64 { return a.char["mt_lvl"]*36 - 24 }
func (a *app) mtPadIdx() int      { return int(math.Round(a.char["mt_pad"] * 4)) }
func (a *app) mtCompIdx() int     { return int(math.Round(a.char["mt_comp"] * 3)) }

// mtSendRec: the take's level and compressor to the engine.
func (a *app) mtSendRec() {
	if a.dsp == nil || a.char == nil {
		return
	}
	a.dsp.send("MTGAIN %.2f", a.mtLevelDb()+mtPads[a.mtPadIdx()])
	a.dsp.send("MTCOMP %d", a.mtCompIdx())
}

// mtTrimming: the knobs shape a finished take (not a new one).
func (a *app) mtTrimming() bool {
	return !a.mt.rec && !a.mt.stopping && a.mt.ask && a.mt.path != ""
}

func (a *app) mtTakePath() string {
	if a.mt.ask {
		return a.mt.path
	}
	if a.mt.playing {
		return a.mt.playPath
	}
	return ""
}

func (a *app) mtTotal() float64 {
	if a.mt.playTotal > 0 {
		return float64(a.mt.playTotal) / 48000
	}
	if p := a.mtTakePath(); p != "" {
		if s := wavSeconds(p); s > 0 {
			return s
		}
	}
	return float64(a.mt.frames) / 48000
}

// mtOut: OUT in seconds (0 means the end).
func (a *app) mtOut() float64 {
	t := a.mtTotal()
	if a.mt.trimOut <= 0 || a.mt.trimOut > t {
		return t
	}
	return a.mt.trimOut
}

// mtLoopRange: what plays: [IN, OUT], or the loop from IN.
func (a *app) mtLoopRange() (float64, float64, bool) {
	in, out := a.mt.trimIn, a.mtOut()
	if b := mtLoopBeats[a.mt.loopIdx]; b > 0 {
		bpm := math.Max(40, a.proj.G["tempo"])
		return in, math.Min(out, in+b*60/bpm), true
	}
	return in, out, false
}

func (a *app) mtResetTrim() {
	a.mt.trimIn, a.mt.trimOut, a.mt.loopIdx, a.mt.fine = 0, 0, 0, false
	a.mt.height, a.mt.zoom, a.mt.selOut = 1, 0, false
}

// mtStep: a stepped knob moves one place per 6 ticks.
func (a *app) mtStep(k, delta int) int {
	a.mt.acc[k] += delta
	s := a.mt.acc[k] / 6
	a.mt.acc[k] -= s * 6
	return s
}

func (a *app) mtKnob(k, delta int) {
	a.touched(fmt.Sprintf("mt%d", k))
	a.mt.knobK, a.mt.knobAt = k, time.Now()
	defer func() { a.dirty = true }()
	if !a.mtTrimming() {
		switch k {
		case 0:
			db := math.Max(-24, math.Min(12, a.mtLevelDb()+float64(delta)*0.25))
			a.char["mt_lvl"] = (db + 24) / 36
		case 1:
			if s := a.mtStep(k, delta); s != 0 {
				a.char["mt_pad"] = float64(max(0, min(4, a.mtPadIdx()+s))) / 4
			}
		case 2:
			if s := a.mtStep(k, delta); s != 0 {
				a.char["mt_comp"] = float64(max(0, min(3, a.mtCompIdx()+s))) / 3
			}
		}
		a.mtSendRec()
		a.saveCharacter()
		return
	}
	// 0.9.25 trimming: K1 HEIGHT, K2 LOOP (from the playhead, OUT follows), K3 ZOOM
	total := a.mtTotal()
	if total <= 0 {
		return
	}
	switch k {
	case 0:
		a.mt.height = math.Max(1, math.Min(16, a.mtHeight()*math.Pow(1.04, float64(delta))))
	case 2:
		a.mt.zoom = math.Max(0.05, math.Min(total, a.mtZoom()*math.Pow(1.05, -float64(delta))))
	case 1:
		if s := a.mtStep(k, delta); s != 0 {
			was := a.mt.loopIdx
			a.mt.loopIdx = max(0, min(len(mtLoopBeats)-1, a.mt.loopIdx+s))
			if a.mt.loopIdx > 0 {
				if was == 0 { // the loop starts where the playhead is
					a.mt.trimIn = math.Min(math.Max(0, a.mtPos()), math.Max(0, total-0.05))
				}
				bpm := math.Max(40, a.proj.G["tempo"])
				a.mt.trimOut = math.Min(total, a.mt.trimIn+mtLoopBeats[a.mt.loopIdx]*60/bpm)
			}
			a.mtSendLoop()
			if !a.mt.playing {
				a.mtStartListen(a.mt.trimIn)
			}
		}
	}
}

func (a *app) mtHeight() float64 {
	if a.mt.height < 1 {
		return 1
	}
	return a.mt.height
}

// mtZoom: the seconds the card shows (0 = the whole take).
func (a *app) mtZoom() float64 {
	t := a.mtTotal()
	if a.mt.zoom <= 0 || a.mt.zoom > t {
		return t
	}
	return a.mt.zoom
}

// mtStartListen plays the take from t (the engine keeps to IN / OUT / LOOP).
func (a *app) mtStartListen(t float64) {
	a.mt.lpaused = false
	a.mtListen(a.mtTakePath(), t)
}

// mtSelect: the left / right buttons pick which marker DATA moves.
func (a *app) mtSelect(out bool) {
	a.mt.selOut = out
	if out {
		a.say("OUT  ·  TURN DATA TO MOVE IT  ·  PUSH: FINE")
	} else {
		a.say("IN  ·  TURN DATA TO MOVE IT  ·  PUSH: FINE")
	}
	a.dirty = true
}

// mtMoveMarker: DATA turns move the picked marker: big moves, or tiny ones in FINE.
func (a *app) mtMoveMarker(clicks int) {
	total := a.mtTotal()
	if total <= 0 {
		return
	}
	step := a.mtZoom() / 60
	if a.mt.fine {
		step = a.mtZoom() / 1200
	}
	mv := float64(clicks) * step
	if a.mt.selOut {
		a.mt.trimOut = math.Max(a.mt.trimIn+0.01, math.Min(total, a.mtOut()+mv))
	} else {
		out := a.mtOut()
		a.mt.trimIn = math.Max(0, math.Min(out-0.01, a.mt.trimIn+mv))
		if a.mt.loopIdx > 0 { // a loop keeps its length: OUT follows IN
			bpm := math.Max(40, a.proj.G["tempo"])
			a.mt.trimOut = math.Min(total, a.mt.trimIn+mtLoopBeats[a.mt.loopIdx]*60/bpm)
		}
	}
	a.mtSendLoop() // the engine follows at once, no restart
	a.dirty = true
}

// mtSendLoop: the part to play (the engine follows it live).
func (a *app) mtSendLoop() {
	if a.dsp == nil {
		return
	}
	if !a.mtTrimming() {
		a.dsp.send("MTLOOP 0 0 0")
		return
	}
	in, out, loop := a.mtLoopRange()
	a.dsp.send("MTLOOP %.3f %.3f %d", in, out, b2i(loop))
}

// mtShownPos: since 0.9.25 the engine reports the true place in the file.
func (a *app) mtShownPos(raw float64) float64 { return raw }

// mtDataPush: FINE on / off while trimming.
func (a *app) mtDataPush() {
	if !a.mtTrimming() {
		return
	}
	a.mt.fine = !a.mt.fine
	if a.mt.fine {
		a.say("FINE  ·  DATA MOVES IN TINY STEPS  ·  PUSH: BIG MOVES")
	} else {
		a.say("BIG MOVES  ·  PUSH: FINE")
	}
	a.dirty = true
}

// mtMonitor: hear the take's sound (level, pad, comp) instead of the plain mix.
func (a *app) mtMonitor(on bool) {
	a.mt.monitor = on
	if a.dsp != nil {
		a.mtSendRec()
		a.dsp.send("MTMON %d", b2i(on))
	}
	if on {
		a.say("MONITOR ON  ·  YOU HEAR LEVEL, PAD AND COMP")
	} else {
		a.say("MONITOR OFF")
	}
	a.dirty = true
}

// ---------------------------------------------------------------- the real wave

// mtLine: min / max per column of the take between t0 and t0+win (seconds),
// read from the WAV (16-bit), cached; nil for wide views (the overview is used).
func (a *app) mtLine(path string, t0, win float64, cols int) ([]float64, []float64) {
	if path == "" || win <= 0 || win > 30 {
		return nil, nil
	}
	key := fmt.Sprintf("%s|%.4f|%.4f|%d", path, t0, win, cols)
	if key == a.mt.lineKey {
		return a.mt.lineMin, a.mt.lineMax
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	dataAt, ch := int64(44), 2
	var hdr [44]byte
	if _, err := io.ReadFull(f, hdr[:]); err == nil && string(hdr[36:40]) == "data" {
		ch = int(binary.LittleEndian.Uint16(hdr[22:]))
	}
	fb := int64(2 * ch)
	a0, n := int64(t0*48000), int64(win*48000)
	buf := make([]byte, n*fb)
	got, _ := f.ReadAt(buf, dataAt+a0*fb)
	frames := int64(got) / fb
	mn, mx := make([]float64, cols), make([]float64, cols)
	for c := 0; c < cols; c++ {
		i0, i1 := int64(c)*n/int64(cols), max(int64(c)*n/int64(cols)+1, int64(c+1)*n/int64(cols))
		lo, hi := 0.0, 0.0
		for i := i0; i < i1 && i < frames; i++ {
			v := float64(int16(binary.LittleEndian.Uint16(buf[i*fb:]))) / 32768
			if ch > 1 {
				v = (v + float64(int16(binary.LittleEndian.Uint16(buf[i*fb+2:])))/32768) / 2
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		mn[c], mx[c] = lo, hi
	}
	a.mt.lineKey, a.mt.lineMin, a.mt.lineMax = key, mn, mx
	return mn, mx
}

// ---------------------------------------------------------------- saving trimmed

// mtSaveTrimmed rewrites a take keeping only [in, out] seconds, and its overview.
func mtSaveTrimmed(path string, in, out float64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil || string(hdr[:4]) != "RIFF" || string(hdr[8:]) != "WAVE" {
		return fmt.Errorf("not a WAV")
	}
	var dataAt, dataSz int64
	ch := 2
	for {
		var c [8]byte
		if _, err := io.ReadFull(f, c[:]); err != nil {
			return fmt.Errorf("no data")
		}
		sz := int64(binary.LittleEndian.Uint32(c[4:]))
		here, _ := f.Seek(0, io.SeekCurrent)
		if string(c[:4]) == "fmt " {
			var b [16]byte
			io.ReadFull(f, b[:])
			ch = int(binary.LittleEndian.Uint16(b[2:]))
		}
		if string(c[:4]) == "data" {
			dataAt = here
			end, _ := f.Seek(0, io.SeekEnd)
			dataSz = min(sz, end-dataAt)
			if sz == 0 {
				dataSz = end - dataAt
			}
			break
		}
		f.Seek(here+sz+sz&1, io.SeekStart)
	}
	fb := int64(2 * ch)
	total := dataSz / fb
	a := max(0, min(total, int64(in*48000)))
	b := max(a+1, min(total, int64(out*48000)))
	tmp := filepath.Join(filepath.Dir(path), ".trim-"+filepath.Base(path))
	o, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n := b - a
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+n*fb))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], uint16(ch))
	binary.LittleEndian.PutUint32(h[24:], 48000)
	binary.LittleEndian.PutUint32(h[28:], uint32(48000*fb))
	binary.LittleEndian.PutUint16(h[32:], uint16(fb))
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(n*fb))
	o.Write(h)
	f.Seek(dataAt+a*fb, io.SeekStart)
	var pk []byte
	acc, peak := 0, 0.0
	buf := make([]byte, 65536*fb)
	left := n * fb
	for left > 0 {
		m := min(int64(len(buf)), left)
		got, err := io.ReadFull(f, buf[:m])
		if got <= 0 {
			if err != nil {
				break
			}
		}
		if _, err := o.Write(buf[:got]); err != nil {
			o.Close()
			os.Remove(tmp)
			return err
		}
		for i := 0; i+int(fb) <= got; i += int(fb) {
			s := 0.0
			for c := 0; c < ch; c++ {
				s += math.Abs(float64(int16(binary.LittleEndian.Uint16(buf[i+2*c:]))))
			}
			s = s / float64(ch) / 32768
			peak = math.Max(peak, s)
			if acc++; acc == 4800 {
				pk = append(pk, levelByte(peak))
				acc, peak = 0, 0
			}
		}
		left -= int64(got)
	}
	if acc > 0 {
		pk = append(pk, levelByte(peak))
	}
	o.Sync()
	if err := o.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(peaksPath(path)), 0755)
	os.WriteFile(peaksPath(path), pk, 0644)
	return nil
}

func levelByte(x float64) byte {
	db := 20 * math.Log10(x+1e-6)
	return byte(math.Max(0, math.Min(255, (db+48)/48*255)))
}

// mtKnobRow: what K1-K3 do here, in the knob colours (one line above the meters).
func (a *app) mtKnobRow() [3][2]string {
	if a.mtTrimming() {
		z := a.mtZoom()
		zs := fmt.Sprintf("%.1f S", z)
		if z < 1 {
			zs = fmt.Sprintf("%.0f MS", z*1000)
		}
		return [3][2]string{
			{"HEIGHT", fmt.Sprintf("x%.1f", a.mtHeight())},
			{"LOOP", mtLoopNames[a.mt.loopIdx]},
			{"ZOOM", zs},
		}
	}
	return [3][2]string{
		{"REC LEVEL", fmt.Sprintf("%+.1f DB", a.mtLevelDb())},
		{"PAD", fmt.Sprintf("%.0f DB", mtPads[a.mtPadIdx()])},
		{"COMP", mtComps[a.mtCompIdx()]},
	}
}

func clockFine(s float64) string {
	m := int(s) / 60
	return fmt.Sprintf("%d:%04.1f", m, s-float64(m*60))
}
