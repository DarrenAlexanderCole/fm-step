// fmstep-lab: second on-device session. Started by the supervisor in place of
// the app (stock software stopped). Steps, each shown on the MPC screen:
//
//  1. CONTROLS: raw byte capture of the control surface (hex, per read),
//     written to FMSTEP/logs/lab-controls-raw.txt with buffered writes.
//  2. SOUND: first audio. A one-voice, two-operator FM tone on every pad;
//     K1 = modulator ratio, K2 = FM amount, K3 = decay, MAIN VOLUME = level.
//     Audio facts go to FMSTEP/logs/lab-audio.txt.
//  3. ENDING from FMSTEP/lab.txt: exit | crash | freeze | killsup
//     (recovery tests). On a supervisor restart (FMSTEP_RESTART>0) steps 1-2
//     are skipped and the ending repeats after the app has become healthy.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fmstep/internal/audio"
	"fmstep/internal/link"
	"fmstep/internal/panel"
)

// Pad note numbers in pad order 1..16 (virtual-mpc-sample PANEL.md; ch10).
var padNotes = []byte{37, 36, 42, 82, 40, 38, 46, 44, 48, 47, 45, 43, 49, 55, 51, 53}

type ev struct {
	st, d1, d2 byte
}

type lab struct {
	logs   string
	scr    *panel.Screen
	cs     *panel.ControlSurface
	t0     time.Time
	beatOn atomic.Bool

	mu     sync.Mutex
	lines  []string // recent events for the screen
	cc     [128]int // last CC value per controller (ch1)
	events chan ev

	rawMu sync.Mutex
	raw   *bufio.Writer
	rawF  *os.File
}

func (l *lab) note(f string, a ...any) {
	line := fmt.Sprintf(f, a...)
	fmt.Println(line) // -> app.log via supervisor
}

func (l *lab) recent(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 5 {
		l.lines = l.lines[1:]
	}
	l.mu.Unlock()
}

func hexs(b []byte) string {
	var sb strings.Builder
	for i, x := range b {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02X", x)
	}
	return sb.String()
}

// reader: raw capture + MIDI parse. Runs for the whole session.
func (l *lab) reader() {
	buf := make([]byte, 512)
	var msg []byte
	need := func(st byte) int {
		switch st & 0xF0 {
		case 0xC0, 0xD0:
			return 2
		}
		return 3
	}
	var sysex bool
	for {
		data := l.cs.Read(buf)
		if len(data) == 0 {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		t := time.Since(l.t0).Seconds()
		l.rawMu.Lock()
		if l.raw != nil {
			fmt.Fprintf(l.raw, "t=%8.4f framed=%d midi=%s\n", t, l.cs.Framed(), hexs(data))
		}
		l.rawMu.Unlock()
		for _, b := range data {
			switch {
			case b == 0xF0:
				sysex = true
			case b == 0xF7:
				sysex = false
			case sysex || b >= 0xF8:
			case b&0x80 != 0:
				msg = []byte{b}
			case len(msg) > 0:
				msg = append(msg, b)
				if len(msg) == need(msg[0]) {
					e := ev{st: msg[0], d1: msg[1]}
					if len(msg) == 3 {
						e.d2 = msg[2]
					}
					if e.st == 0xB0 {
						l.mu.Lock()
						l.cc[e.d1] = int(e.d2)
						l.mu.Unlock()
					}
					select {
					case l.events <- e:
					default:
					}
					msg = msg[:1]
				}
			}
		}
	}
}

func (l *lab) flusher(stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			l.rawMu.Lock()
			if l.raw != nil {
				l.raw.Flush()
				l.rawF.Sync()
			}
			l.rawMu.Unlock()
		}
	}
}

func (l *lab) draw(title string, lines []string, left time.Duration) {
	if l.scr == nil {
		return
	}
	s := l.scr
	s.Fill(0x00102030)
	s.Text(6, 6, 2, 0x00FFFFFF, title)
	y := 30
	for _, ln := range lines {
		if strings.HasPrefix(ln, "   ") { // emphasised line
			s.Text(6, y, 2, 0x0080FFFF, strings.TrimSpace(ln))
			y += 20
			continue
		}
		s.Text(6, y, 1, 0x00FFFF80, ln)
		y += 12
	}
	if left > 0 {
		s.Text(6, y+6, 2, 0x0080FF80, fmt.Sprintf("%d S LEFT", int(left.Seconds())+1))
	}
	l.mu.Lock()
	rec := append([]string(nil), l.lines...)
	l.mu.Unlock()
	for i, r := range rec {
		s.Text(6, 160+i*13, 1, 0x00C0C0C0, r)
	}
	s.Flush()
}

func describe(e ev) string {
	ch := e.st&0x0F + 1
	switch e.st & 0xF0 {
	case 0x80:
		return fmt.Sprintf("NOTE OFF CH%d %d", ch, e.d1)
	case 0x90:
		return fmt.Sprintf("NOTE ON  CH%d %d VEL %d", ch, e.d1, e.d2)
	case 0xA0:
		return fmt.Sprintf("PRESSURE CH%d %d = %d", ch, e.d1, e.d2)
	case 0xB0:
		return fmt.Sprintf("CC       CH%d %d = %d", ch, e.d1, e.d2)
	}
	return fmt.Sprintf("%02X %02X %02X", e.st, e.d1, e.d2)
}

// ---------------------------------------------------------------- phase 1
// Buttons in the order the user is asked to press them (from a photo of the
// user's MPC Sample, 2026-11-04).
var buttonNames = []string{
	"LEFT BUTTON ABOVE SCREEN", "MIDDLE BUTTON ABOVE SCREEN", "RIGHT BUTTON ABOVE SCREEN",
	"SAMPLE", "SEQ", "PAD FX", "KNOB FX", "SHIFT", "PAD BANK",
	"CHOP", "MUTE", "LOOP", "16 LEVELS", "SAMPLE SELECT", "TAP TEMPO",
	"MINUS  -", "PLUS  +", "SAMPLE RECORD", "SEQ RECORD", "STOP", "PLAY",
	"ERASE", "NOTE REPEAT",
}

func (l *lab) phaseControls(d time.Duration) {
	f, err := os.Create(filepath.Join(l.logs, "lab-buttons.txt"))
	if err == nil {
		l.rawMu.Lock()
		l.rawF, l.raw = f, bufio.NewWriterSize(f, 64<<10)
		l.rawMu.Unlock()
	}
	logf := func(format string, a ...any) {
		l.rawMu.Lock()
		if l.raw != nil {
			fmt.Fprintf(l.raw, format+"\n", a...)
		}
		l.rawMu.Unlock()
	}
	// drain anything queued before we start asking
	for len(l.events) > 0 {
		<-l.events
	}
	idx := 0
	last := ""
	end := time.Now().Add(d)
	for time.Now().Before(end) && idx < len(buttonNames) {
		select {
		case e := <-l.events:
			l.recent(describe(e))
			var id string
			switch {
			case e.st == 0xB0 && e.d1 == 100: // DATA encoder turn = skip
				logf("MAP %-28s = SKIPPED", buttonNames[idx])
				last = buttonNames[idx] + ": SKIPPED"
				idx++
				continue
			case e.st == 0x90 && e.d1 == 111: // encoder push: known, ignore
			case e.st&0xF0 == 0x90 && e.d2 > 0:
				id = fmt.Sprintf("NOTE %d CH%d", e.d1, e.st&0x0F+1)
			case e.st&0xF0 == 0xB0 && (e.d1 < 16 || e.d1 > 20):
				id = fmt.Sprintf("CC %d CH%d VAL %d", e.d1, e.st&0x0F+1, e.d2)
			}
			if id != "" {
				logf("MAP %-28s = %s  (bytes %02X %02X %02X)", buttonNames[idx], id, e.st, e.d1, e.d2)
				last = buttonNames[idx] + " = " + id
				idx++
				time.Sleep(250 * time.Millisecond) // ignore the release / bounce
				for len(l.events) > 0 {
					e2 := <-l.events
					logf("  after: %s", describe(e2))
				}
			}
		case <-time.After(150 * time.Millisecond):
		}
		name := "ALL DONE"
		if idx < len(buttonNames) {
			name = buttonNames[idx]
		}
		l.draw(fmt.Sprintf("1/2 BUTTONS %d/%d", idx, len(buttonNames)), []string{
			"PRESS ONLY THIS BUTTON, ONCE:",
			"",
			"   " + name,
			"",
			"NOTHING HAPPENS? TURN DATA TO SKIP IT.",
			"LAST: " + last,
		}, time.Until(end))
	}
	logf("DONE mapped=%d of %d", idx, len(buttonNames))
	l.rawMu.Lock()
	if l.raw != nil {
		l.raw.Flush()
		l.rawF.Sync()
		l.rawF.Close()
		l.raw = nil
	}
	l.rawMu.Unlock()
}

// ---------------------------------------------------------------- phase 2
type voice struct {
	mu    sync.Mutex
	freq  float64
	vel   float64
	trig  bool
	ratio float64
	index float64
	decay float64 // seconds
	level float64
}

var ratios = []float64{0.5, 1, 2, 3, 4, 5, 6, 7, 8}

func setRealtime() string {
	type param struct{ prio int32 }
	p := param{prio: 60}
	_, _, e := syscall.RawSyscall(syscall.SYS_SCHED_SETSCHEDULER, 0, 1 /*SCHED_FIFO*/, uintptr(unsafe.Pointer(&p)))
	if e != 0 {
		return "sched_fifo_failed:" + e.Error()
	}
	return "sched_fifo_60"
}

func pcmDevice() string {
	if t, err := os.Readlink("/dev/snd/by-path/platform-sound"); err == nil {
		// e.g. ../controlC2
		if i := strings.LastIndex(t, "controlC"); i >= 0 {
			if n, err := strconv.Atoi(t[i+8:]); err == nil {
				return fmt.Sprintf("/dev/snd/pcmC%dD0p", n)
			}
		}
	}
	return "/dev/snd/pcmC2D0p"
}

func (l *lab) phaseSound(d time.Duration) {
	af, _ := os.Create(filepath.Join(l.logs, "lab-audio.txt"))
	alog := func(f string, a ...any) {
		s := fmt.Sprintf(f, a...)
		l.note("AUDIO %s", s)
		if af != nil {
			fmt.Fprintln(af, s)
			af.Sync()
		}
	}
	defer func() {
		if af != nil {
			af.Close()
		}
	}()
	dev := pcmDevice()
	caps, err := audio.Capabilities(dev)
	alog("device=%s caps=%s err=%v", dev, caps, err)

	channels := 2
	var pcm *audio.PCM
	for _, ch := range []int{2, 4, 1} {
		for _, rate := range []int{48000, 44100} {
			pcm, err = audio.Open(dev, ch, rate, 256, 4, []int{audio.FormatS32LE, audio.FormatS16LE, audio.FormatS24LE})
			if err == nil {
				channels = ch
				break
			}
			alog("open ch=%d rate=%d: %v", ch, rate, err)
		}
		if pcm != nil {
			break
		}
	}
	if pcm == nil {
		alog("AUDIO_OPEN_FAILED")
		end := time.Now().Add(8 * time.Second)
		for time.Now().Before(end) {
			l.draw("2/2 SOUND", []string{"AUDIO DEVICE COULD NOT BE OPENED", "DETAILS SAVED TO THE LOG"}, time.Until(end))
			time.Sleep(200 * time.Millisecond)
		}
		return
	}
	alog("OPEN_OK %s", pcm)

	v := &voice{ratio: 2, index: 2, decay: 0.6}
	var stop atomic.Bool
	var maxBlock atomic.Int64
	done := make(chan struct{})
	go func() { // audio thread
		defer close(done)
		runtime.LockOSThread()
		alog("audio_thread %s", setRealtime())
		rate := float64(pcm.Rate)
		frames := pcm.Period
		buf := make([]float32, frames*channels)
		scratch := make([]byte, len(buf)*4)
		var pc, pm, env, lvl float64
		var freq, ratio, index, decayMul, vel float64 = 440, 2, 2, 0.9999, 0
		for !stop.Load() {
			v.mu.Lock()
			if v.trig {
				v.trig = false
				freq, vel = v.freq, v.vel
				env = 1
				pc, pm = 0, 0
			}
			ratio, index = v.ratio, v.index
			decayMul = math.Exp(-1 / (v.decay * rate / 6.9)) // -60 dB over `decay`
			target := v.level
			v.mu.Unlock()
			for i := 0; i < frames; i++ {
				lvl += (target - lvl) * 0.001 // smooth volume changes
				m := math.Sin(pm) * index
				s := math.Sin(pc+m) * env * vel * lvl * 0.3
				pc += 2 * math.Pi * freq / rate
				pm += 2 * math.Pi * freq * ratio / rate
				if pc > 2*math.Pi {
					pc -= 2 * math.Pi
				}
				if pm > 2*math.Pi {
					pm -= 2 * math.Pi
				}
				env *= decayMul
				for c := 0; c < channels; c++ {
					buf[i*channels+c] = float32(s)
				}
			}
			t := time.Now()
			if err := pcm.Write(buf, scratch); err != nil {
				alog("WRITE_ERROR %v", err)
				return
			}
			if us := time.Since(t).Microseconds(); us > maxBlock.Load() {
				maxBlock.Store(us)
			}
		}
	}()

	end := time.Now().Add(d)
	for time.Now().Before(end) {
		select {
		case e := <-l.events:
			l.recent(describe(e))
			if e.st == 0x99 && e.d2 > 0 {
				idx := 0
				for i, n := range padNotes {
					if n == e.d1 {
						idx = i
					}
				}
				v.mu.Lock()
				v.freq = 130.81 * math.Pow(2, float64(idx)/12)
				v.vel = float64(e.d2) / 127
				v.trig = true
				v.mu.Unlock()
			}
		case <-time.After(100 * time.Millisecond):
		}
		l.mu.Lock()
		k1, k2, k3, vol := l.cc[16], l.cc[17], l.cc[18], l.cc[19]
		l.mu.Unlock()
		v.mu.Lock()
		v.ratio = ratios[k1*len(ratios)/128]
		v.index = float64(k2) / 127 * 8
		v.decay = 0.05 + float64(k3)/127*2.5
		g := float64(vol) / 127
		v.level = g * g
		r, ix, dc := v.ratio, v.index, v.decay
		v.mu.Unlock()
		volMsg := fmt.Sprintf("MAIN VOLUME %d/127", vol)
		if vol < 10 {
			volMsg += "  - TURN IT UP A LITTLE"
		}
		l.draw("2/2 SOUND - FM TEST", []string{
			"PLAY THE PADS. START WITH VOLUME LOW.",
			fmt.Sprintf("K1 RATIO %.1f   K2 FM %.1f   K3 DECAY %.2f S", r, ix, dc),
			volMsg,
			fmt.Sprintf("%s  XRUNS %d", pcm, pcm.Xruns),
		}, time.Until(end))
	}
	// fade out, then release the device cleanly
	v.mu.Lock()
	v.level = 0
	v.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	stop.Store(true)
	<-done
	pcm.Close(false)
	alog("CLOSED xruns=%d max_write_block_us=%d", pcm.Xruns, maxBlock.Load())
}

// ---------------------------------------------------------------- main
func main() {
	sd := os.Getenv("FMSTEP_SD")
	l := &lab{logs: filepath.Join(sd, "FMSTEP", "logs"), t0: time.Now(), events: make(chan ev, 256)}
	os.MkdirAll(l.logs, 0755)
	restart, _ := strconv.Atoi(os.Getenv("FMSTEP_RESTART"))
	ending := "exit"
	if b, err := os.ReadFile(filepath.Join(sd, "FMSTEP", "lab.txt")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			ending = strings.ToLower(f[0])
		}
	}
	l.note("LAB_START ending=%s restart=%d", ending, restart)

	lk := os.NewFile(link.FD, "link")
	var lkMu sync.Mutex
	send := func(c byte) {
		lkMu.Lock()
		defer lkMu.Unlock()
		lk.Write([]byte{c})
	}
	l.beatOn.Store(true)
	go func() {
		for {
			if l.beatOn.Load() {
				send(link.Heartbeat)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	var err error
	if l.scr, err = panel.OpenScreen("/dev/dri/card0"); err != nil {
		l.note("SCREEN_FAILED %v", err)
		l.scr = nil
	}
	var dev string
	if l.cs, dev, err = panel.OpenControlSurface(true); err != nil {
		l.note("CONTROL_SURFACE_FAILED %v", err)
	} else {
		l.note("CONTROL_SURFACE_OK %s", dev)
		go l.reader()
		for p := byte(0); p < 16; p++ {
			l.cs.Write([]byte{0xF0, 0x47, 0x7F, 0x5C, 0x65, 0x00, 0x04, p, 0, 20, 30, 0xF7})
		}
	}
	stopFlush := make(chan struct{})
	go l.flusher(stopFlush)
	send(link.Ready)

	secs := 90
	if v, err := strconv.Atoi(os.Getenv("FMSTEP_LAB_SECS")); err == nil && v > 0 {
		secs = v
	}
	if restart == 0 && ending == "exit" {
		if l.cs != nil {
			l.phaseControls(time.Duration(secs) * time.Second)
		}
		l.phaseSound(time.Duration(secs) * time.Second)
	} else {
		end := time.Now().Add(13 * time.Second) // let the supervisor mark us healthy first
		for time.Now().Before(end) {
			title, info := "RECOVERY TEST: "+strings.ToUpper(ending), "WAITING UNTIL THE SUPERVISOR MARKS US HEALTHY"
			if restart > 0 {
				title, info = "RESTARTED AFTER "+strings.ToUpper(ending), "THE SUPERVISOR RESTARTED FM-STEP (1 OF 1)"
			}
			l.draw(title, []string{info}, time.Until(end))
			time.Sleep(200 * time.Millisecond)
		}
	}
	close(stopFlush)

	msg := map[string]string{
		"exit":    "EXIT TO STOCK",
		"crash":   "CRASH TEST: CRASHING IN 5 S",
		"freeze":  "FREEZE TEST: FREEZING IN 5 S",
		"killsup": "SUPERVISOR CRASH TEST IN 5 S",
	}[ending]
	if msg == "" {
		ending, msg = "exit", "EXIT TO STOCK"
	}
	if ending != "exit" {
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			l.draw("RECOVERY TEST", []string{msg, "STOCK SHOULD RETURN BY ITSELF.", "IF NOT IN 60 S: HOLD POWER, REMOVE SD"}, time.Until(end))
			time.Sleep(200 * time.Millisecond)
		}
	}
	l.note("LAB_ENDING %s", ending)
	switch ending {
	case "crash":
		os.Exit(139) // devices are released by the kernel
	case "freeze":
		l.draw("FROZEN", []string{"HEARTBEAT STOPPED ON PURPOSE"}, 0)
		l.beatOn.Store(false)
		for {
			time.Sleep(time.Hour)
		}
	case "killsup":
		syscall.Kill(os.Getppid(), syscall.SIGKILL)
		for {
			time.Sleep(time.Hour) // systemd kills us with the unit
		}
	}
	// Exit to Stock: release hardware, then tell the supervisor.
	if l.scr != nil {
		l.scr.Fill(0)
		l.scr.Text(6, 100, 2, 0x00FFFFFF, "RETURNING TO STOCK")
		l.scr.Flush()
		l.scr.Close()
	}
	if l.cs != nil {
		for p := byte(0); p < 16; p++ {
			l.cs.Write([]byte{0xF0, 0x47, 0x7F, 0x5C, 0x65, 0x00, 0x04, p, 0, 0, 0, 0xF7})
		}
		l.cs.Close()
	}
	l.note("LAB_DONE")
	send(link.ExitStock)
	os.Exit(0)
}
