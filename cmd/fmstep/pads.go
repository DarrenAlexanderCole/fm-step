package main

// PADS (0.9.27): every pad its own sound. 8 banks x 16 pads = 128 slots
// (slot = bank*16 + pad). The engine side is dsp/padsampler.h; this keeps
// the kit (which file on which pad, with its trim, envelope, mode, tune,
// level, pan, reverse), loads it into the engine, and reads the waves it
// shows straight from the WAV files on the card.
//
// The kit is kept in kit.json next to character.json (saved as you go);
// SHIFT + pad 16 PROJECT saves / loads named kits in FMSTEP/kits.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	padSlots     = 128
	padMaxSecs   = 20.0
	padTrackDflt = 7 // T8: the sampler track
)

type padSlot struct {
	Path  string  `json:"path,omitempty"`
	Start float64 `json:"start,omitempty"` // seconds into the file
	Len   float64 `json:"len,omitempty"`   // seconds taken (0 = to the end, up to 20 s)
	Name  string  `json:"name,omitempty"`
	In    float64 `json:"in"`  // trim, 0..1 of what was taken
	Out   float64 `json:"out"` //
	Env   padEnv  `json:"env"`
	Pitch float64 `json:"pitch,omitempty"` // semitones
	Fine  float64 `json:"fine,omitempty"`  // cents
	Level float64 `json:"level"`
	Pan   float64 `json:"pan"`
	Rev   bool    `json:"rev,omitempty"`

	frames  int // loaded in the engine (0: empty or still loading)
	loading bool
	err     string
	peaks   []float64 // 1024 peaks over what was taken (0..1)
}

func newSlot() padSlot { return padSlot{Out: 1, Env: defaultEnv, Level: 0.8, Pan: 0.5} }

func (s *padSlot) empty() bool { return s.Path == "" }

// seconds of the pad as it plays (trimmed, at its pitch)
func (s *padSlot) secs() float64 {
	n := float64(s.frames) / 48000
	if n <= 0 {
		n = s.Len
	}
	return n * (s.Out - s.In) / math.Pow(2, (s.Pitch+s.Fine/100)/12)
}

type padsState struct {
	s        [padSlots]padSlot
	bank     int
	track    int
	inited   bool
	dirty    bool
	savedAt  time.Time
	recSlot  int
	recOn    bool
	recStart time.Time
}

// padData: engine replies (written by the DSP reader goroutine)
type padData struct {
	mu     sync.Mutex
	events []string
	recLen int
	mgr    [2]float64 // RACK MASTER gain: comp, limiter (1 = none)
	mgrAt  time.Time
}

func (d *padData) parse(line string) bool {
	switch {
	case strings.HasPrefix(line, "PLOADED "), strings.HasPrefix(line, "PLOADERR "), strings.HasPrefix(line, "PRECDONE "):
		d.mu.Lock()
		d.events = append(d.events, line)
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "PRECLEN "):
		n, _ := strconv.Atoi(strings.TrimSpace(line[8:]))
		d.mu.Lock()
		d.recLen = n
		d.mu.Unlock()
		return true
	case strings.HasPrefix(line, "MGR "):
		f := strings.Fields(line)
		if len(f) == 3 {
			d.mu.Lock()
			d.mgr[0], _ = strconv.ParseFloat(f[1], 64)
			d.mgr[1], _ = strconv.ParseFloat(f[2], 64)
			d.mgrAt = time.Now()
			d.mu.Unlock()
		}
		return true
	}
	return false
}

func (a *app) slotIdx(pad int) int  { return a.pads.bank*16 + pad }
func (a *app) padSel() int          { return a.slotIdx(a.smp.sel) }
func (a *app) pslot(i int) *padSlot { return &a.pads.s[max(0, min(padSlots-1, i))] }
func bankName(b int) string         { return string(rune('A' + b)) }
func slotName(i int) string         { return fmt.Sprintf("%s%02d", bankName(i/16), i%16+1) }

func (a *app) kitPath() string { return filepath.Join(a.user, "kit.json") }
func (a *app) kitsDir() string {
	return filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "kits")
}

func (a *app) padsInit() {
	if a.pads.inited {
		return
	}
	a.pads.inited = true
	a.pads.track = padTrackDflt
	for i := range a.pads.s {
		a.pads.s[i] = newSlot()
	}
	if b, err := os.ReadFile(a.kitPath()); err == nil {
		a.kitApply(b)
	}
	if a.dsp != nil {
		a.dsp.send("CHOPTRK -1")
		a.dsp.send("PTRK -1") // 0.9.28: tracks 9-16 play the pads
	}
	for i := range a.pads.s {
		a.padLoad(i)
	}
}

func (a *app) kitApply(b []byte) bool {
	var k struct {
		Pads map[string]padSlot `json:"pads"`
	}
	if json.Unmarshal(b, &k) != nil {
		return false
	}
	for i := range a.pads.s {
		a.pads.s[i] = newSlot()
	}
	for key, s := range k.Pads {
		i := -1
		if len(key) == 3 && key[0] >= 'A' && key[0] <= 'H' {
			if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 16 {
				i = int(key[0]-'A')*16 + n - 1
			}
		}
		if i < 0 {
			continue
		}
		s.In, s.Out = clamp01(s.In), clamp01(s.Out)
		if s.Out <= s.In {
			s.In, s.Out = 0, 1
		}
		s.Level, s.Pan = math.Max(0, math.Min(2, s.Level)), clamp01(s.Pan)
		s.Env.A, s.Env.D, s.Env.S, s.Env.R = clamp01(s.Env.A), clamp01(s.Env.D), clamp01(s.Env.S), clamp01(s.Env.R)
		s.Env.Mode = max(0, min(2, s.Env.Mode))
		a.pads.s[i] = s
	}
	return true
}

func (a *app) kitJSON() []byte {
	k := struct {
		Version string             `json:"version"`
		Pads    map[string]padSlot `json:"pads"`
	}{appVersion, map[string]padSlot{}}
	for i, s := range a.pads.s {
		if !s.empty() {
			k.Pads[slotName(i)] = s
		}
	}
	b, _ := json.MarshalIndent(k, "", "  ")
	return b
}

// padsSave: the kit as it is now (at most every 2 s unless now)
func (a *app) padsSave(now bool) {
	if !a.pads.dirty || (!now && time.Since(a.pads.savedAt) < 2*time.Second) {
		return
	}
	if os.WriteFile(a.kitPath(), a.kitJSON(), 0644) == nil {
		a.pads.dirty, a.pads.savedAt = false, time.Now()
	}
}

func (a *app) padsChanged() {
	a.pads.dirty = true
	a.padsSave(false)
}

// padLoad: send slot i to the engine (its file, then its settings)
func (a *app) padLoad(i int) {
	s := a.pslot(i)
	s.frames, s.err, s.peaks = 0, "", nil
	if s.empty() || a.dsp == nil {
		s.loading = false
		if a.dsp != nil {
			a.dsp.send("PCLR %d", i)
		}
		return
	}
	s.loading = true
	a.dsp.send("PLOAD %d %.4f %.4f %s", i, s.Start, s.Len, s.Path)
	a.padSend(i)
}

// padSend: slot i's settings to the engine
func (a *app) padSend(i int) {
	if a.dsp == nil {
		return
	}
	s := a.pslot(i)
	e := s.Env
	a.dsp.send("PSET %d %.5f %.5f %.1f %.1f %.3f %.1f %d %.2f %.1f %.3f %.3f %d", i, s.In, s.Out,
		envAms(e.A), envDms(e.D), e.S, envRms(e.R), e.Mode, s.Pitch, s.Fine, s.Level, s.Pan, b2i(s.Rev))
}

// padAssign: a part of a WAV onto slot i (from SAMPLE SELECT or a CHOP)
func (a *app) padAssign(i int, path string, start, length float64) {
	s := a.pslot(i)
	keep := *s
	*s = newSlot()
	if !keep.empty() { // a pad keeps its sound settings when it gets a new sound
		s.Env, s.Level, s.Pan, s.Pitch, s.Fine = keep.Env, keep.Level, keep.Pan, keep.Pitch, keep.Fine
	}
	s.Path, s.Start, s.Len = path, math.Max(0, start), math.Min(padMaxSecs, math.Max(0, length))
	s.Name = strings.ToUpper(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	a.padLoad(i)
	a.padsChanged()
	a.log("PAD_ASSIGN %s %s start=%.3f len=%.3f", slotName(i), path, start, length)
}

func (a *app) padClear(i int) {
	*a.pslot(i) = newSlot()
	a.padLoad(i)
	a.padsChanged()
}

// padsTick: engine news (loads done, recordings done)
func (a *app) padsTick() {
	if a.dsp == nil || !a.pads.inited {
		return
	}
	d := &a.dsp.pads
	d.mu.Lock()
	ev := d.events
	d.events = nil
	d.mu.Unlock()
	for _, l := range ev {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		i, _ := strconv.Atoi(f[1])
		if i < 0 || i >= padSlots {
			continue
		}
		s := a.pslot(i)
		switch f[0] {
		case "PLOADED":
			s.frames, _ = strconv.Atoi(f[2])
			s.loading = false
			a.log("PAD_LOADED %s frames=%d", slotName(i), s.frames)
		case "PLOADERR":
			s.loading, s.err = false, f[2]
			a.say(fmt.Sprintf("PAD %s: CAN'T LOAD (%s)", slotName(i), strings.ToUpper(f[2])))
			a.log("PAD_LOADERR %s %s", slotName(i), l)
		case "PRECDONE":
			n, _ := strconv.Atoi(f[2])
			a.pads.recOn = false
			if n <= 0 || len(f) < 4 || f[3] == "-" {
				a.say("NOTHING RECORDED")
				break
			}
			path := strings.Join(f[3:], " ")
			keep := *s
			*s = newSlot()
			if !keep.empty() {
				s.Env, s.Level, s.Pan = keep.Env, keep.Level, keep.Pan
			}
			s.Path, s.Name, s.frames = path, strings.ToUpper(strings.TrimSuffix(filepath.Base(path), ".wav")), n
			a.padSend(i)
			a.padsChanged()
			a.say(fmt.Sprintf("PAD %s: %.1f S RECORDED", slotName(i), float64(n)/48000))
			a.log("PAD_RECORDED %s frames=%d %s", slotName(i), n, path)
		}
		a.dirty = true
	}
	a.padsSave(false)
}

// ---------------------------------------------------------------- WAV reading (for the waves on screen)

type wavHdr struct {
	fmt, ch, bits int
	rate          float64
	dataAt        int64
	frames        int64
}

func readWavHdr(f *os.File) (wavHdr, bool) {
	var h wavHdr
	b := make([]byte, 12)
	if _, err := io.ReadFull(f, b); err != nil || string(b[:4]) != "RIFF" || string(b[8:]) != "WAVE" {
		return h, false
	}
	pos := int64(12)
	for i := 0; i < 64; i++ {
		c := make([]byte, 8)
		if _, err := f.ReadAt(c, pos); err != nil {
			return h, false
		}
		sz := int64(binary.LittleEndian.Uint32(c[4:]))
		switch string(c[:4]) {
		case "fmt ":
			fb := make([]byte, min(sz, 40))
			f.ReadAt(fb, pos+8)
			if len(fb) >= 16 {
				h.fmt = int(binary.LittleEndian.Uint16(fb))
				h.ch = int(binary.LittleEndian.Uint16(fb[2:]))
				h.rate = float64(binary.LittleEndian.Uint32(fb[4:]))
				h.bits = int(binary.LittleEndian.Uint16(fb[14:]))
				if h.fmt == 0xFFFE && len(fb) >= 26 {
					h.fmt = int(binary.LittleEndian.Uint16(fb[24:]))
				}
			}
		case "data":
			h.dataAt = pos + 8
			if h.ch > 0 && h.bits >= 8 {
				h.frames = sz / int64(h.ch*h.bits/8)
				if st, err := f.Stat(); err == nil {
					h.frames = min(h.frames, (st.Size()-h.dataAt)/int64(h.ch*h.bits/8))
				}
			}
			ok := h.ch > 0 && h.rate > 0 && (h.fmt == 1 || h.fmt == 3) && (h.bits == 8 || h.bits == 16 || h.bits == 24 || h.bits == 32)
			return h, ok
		}
		pos += 8 + sz + sz&1
	}
	return h, false
}

// mono sample at byte offset (the channels averaged)
func (h wavHdr) sample(b []byte, i int) float64 {
	bps := h.bits / 8
	v := 0.0
	for c := 0; c < h.ch && c < 2; c++ {
		q := b[(i*h.ch+c)*bps:]
		switch {
		case h.fmt == 3 && h.bits == 32:
			v += float64(math.Float32frombits(binary.LittleEndian.Uint32(q)))
		case h.bits == 8:
			v += (float64(q[0]) - 128) / 128
		case h.bits == 16:
			v += float64(int16(binary.LittleEndian.Uint16(q))) / 32768
		case h.bits == 24:
			v += float64(int32(uint32(q[0])<<8|uint32(q[1])<<16|uint32(q[2])<<24)) / 2147483648
		default:
			v += float64(int32(binary.LittleEndian.Uint32(q))) / 2147483648
		}
	}
	return v / float64(min(h.ch, 2))
}

var wavCache = struct {
	sync.Mutex
	m map[string][]float64
}{m: map[string][]float64{}}

// wavPeaks: cols abs peaks (0..1) over [start, start+length] seconds of a
// WAV (length 0 = to the end). Long stretches are sampled, not read whole.
func wavPeaks(path string, start, length float64, cols int) ([]float64, float64) {
	key := fmt.Sprintf("%s|%.4f|%.4f|%d", path, start, length, cols)
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	h, ok := readWavHdr(f)
	if !ok || h.frames <= 0 {
		return nil, 0
	}
	total := float64(h.frames) / h.rate
	wavCache.Lock()
	if p, ok := wavCache.m[key]; ok {
		wavCache.Unlock()
		return p, total
	}
	wavCache.Unlock()
	a0 := int64(math.Max(0, start) * h.rate)
	n := h.frames - a0
	if length > 0 {
		n = min(n, int64(length*h.rate))
	}
	if n <= 0 {
		return nil, total
	}
	fb := int64(h.ch * h.bits / 8)
	out := make([]float64, cols)
	per := n / int64(cols)
	read := min(max(per, 1), 2048) // frames read per column
	buf := make([]byte, read*fb)
	pk := 0.0
	for c := 0; c < cols; c++ {
		at := a0 + int64(c)*n/int64(cols)
		got, _ := f.ReadAt(buf, h.dataAt+at*fb)
		m := 0.0
		for i := 0; i < got/int(fb); i++ {
			m = math.Max(m, math.Abs(h.sample(buf, i)))
		}
		out[c] = m
		pk = math.Max(pk, m)
	}
	if pk > 0 {
		for c := range out {
			out[c] = math.Min(1, out[c]/pk)
		}
	}
	wavCache.Lock()
	if len(wavCache.m) > 256 {
		wavCache.m = map[string][]float64{}
	}
	wavCache.m[key] = out
	wavCache.Unlock()
	return out, total
}

// wavZero: the nearest point to t (seconds) where the sound crosses zero
func wavZero(path string, t float64) float64 {
	f, err := os.Open(path)
	if err != nil {
		return t
	}
	defer f.Close()
	h, ok := readWavHdr(f)
	if !ok {
		return t
	}
	const win = 2400 // 50 ms each side
	c := int64(t * h.rate)
	a0 := max(0, c-win)
	fb := int64(h.ch * h.bits / 8)
	buf := make([]byte, 2*win*fb)
	got, _ := f.ReadAt(buf, h.dataAt+a0*fb)
	nfr := got / int(fb)
	best, bd := c, int64(1<<40)
	prev := 0.0
	for i := 0; i < nfr; i++ {
		v := h.sample(buf, i)
		if i > 0 && (prev <= 0) != (v <= 0) {
			p := a0 + int64(i)
			if d := abs64(p - c); d < bd {
				best, bd = p, d
			}
		}
		prev = v
	}
	return float64(best) / h.rate
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// padPeaks: slot i's wave (what was taken), cached on the slot
func (a *app) padPeaks(i int) []float64 {
	s := a.pslot(i)
	if s.empty() {
		return nil
	}
	if s.peaks == nil {
		s.peaks, _ = wavPeaks(s.Path, s.Start, s.Len, 1024)
	}
	return s.peaks
}

// padTaken: seconds taken from the file (what the pad holds)
func (a *app) padTaken(i int) float64 {
	s := a.pslot(i)
	if s.frames > 0 {
		return float64(s.frames) / 48000
	}
	if s.Len > 0 {
		return s.Len
	}
	if t := wavSeconds(s.Path); t > 0 {
		return math.Min(padMaxSecs, t-s.Start)
	}
	return 0
}
