package main

// The MIXTAPE knobs (0.9.24).
//
// Recording (and before a take): K1 REC LEVEL (-24..+12 dB), K2 PAD
// (0 / -10 / -20 / -30 / -40 dB), K3 COMP (OFF, GLUE, PUNCH, LIMIT). They act on
// the take only, never on the mix you hear, and are kept between sessions.
//
// A finished take (KEEP?, and listening to it before saving): K1 IN, K3 OUT, K2 LOOP (off, or 1/4 to
// 16 beats from IN, looping). DATA moves through the take; DATA push switches
// to FINE (a 2-second view, small steps). SAVE writes the take trimmed to IN-OUT.

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
	total := a.mtTotal()
	if total <= 0 {
		return
	}
	span := total
	if a.mt.fine {
		span = math.Min(total, 2)
	}
	mv := float64(delta) * span / 300
	switch k {
	case 0:
		a.mt.trimIn = math.Max(0, math.Min(a.mtOut()-0.05, a.mt.trimIn+mv))
		a.mt.trimFocus = 0
	case 2:
		a.mt.trimOut = math.Max(a.mt.trimIn+0.05, math.Min(total, a.mtOut()+mv))
		a.mt.trimFocus = 1
	case 1:
		if s := a.mtStep(k, delta); s != 0 {
			a.mt.loopIdx = max(0, min(len(mtLoopBeats)-1, a.mt.loopIdx+s))
		}
		a.mt.trimFocus = 0
	}
	// hear it: (re)start from IN once the turning settles
	if k != 2 || !a.mt.playing {
		a.mt.seekTo, a.mt.seekAt, a.mt.seekPending = a.mt.trimIn, time.Now(), true
		if !a.mt.playing {
			a.mt.lpaused = false
			a.mt.playing, a.mt.playPath = true, a.mtTakePath()
			a.mt.playTotal = int(total * 48000)
		}
	} else if a.mt.playing {
		a.mtSendLoop() // OUT: the reader picks it up at the next start
	}
}

// mtSendLoop: the part to play next time listening (re)starts.
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

// mtShownPos maps the engine's played position into the loop.
func (a *app) mtShownPos(raw float64) float64 {
	if !a.mtTrimming() {
		return raw
	}
	in, out, loop := a.mtLoopRange()
	if loop && raw > out && out > in {
		return in + math.Mod(raw-in, out-in)
	}
	return raw
}

// mtDataPush: FINE on / off while trimming.
func (a *app) mtDataPush() {
	if !a.mtTrimming() {
		return
	}
	a.mt.fine = !a.mt.fine
	if a.mt.fine {
		a.say("FINE  ·  A 2-SECOND VIEW, SMALL STEPS  ·  PUSH: WHOLE TAKE")
	} else {
		a.say("WHOLE TAKE  ·  PUSH: FINE")
	}
	a.dirty = true
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
		in, out, _ := a.mtLoopRange()
		_ = out
		return [3][2]string{
			{"IN", clockFine(in)},
			{"LOOP", mtLoopNames[a.mt.loopIdx]},
			{"OUT", clockFine(a.mtOut())},
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
