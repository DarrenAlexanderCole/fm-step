// fmstep-probe: first on-device run. Started by the FM-STEP supervisor in place
// of the app (stock software stopped). It:
//
//  1. records read-only system facts to FMSTEP/logs/probe-system.txt
//     (firmware hashes, services, audio/MIDI/display devices, SSH presence),
//  2. draws instructions on the display (tests our DRM path),
//  3. lights the pads blue (tests the pad-colour SysEx),
//  4. records every byte from the control surface for -duration while the
//     user presses pads/buttons and turns knobs, decoded, to probe-controls.txt,
//  5. turns the pads off, closes everything and asks for Exit to Stock.
//
// It writes nothing outside the SD card and makes no audio.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fmstep/internal/link"
	"fmstep/internal/panel"
)

var systemCmds = []string{
	"cat /etc/os-release; uname -a",
	"cat /proc/cpuinfo | grep -m4 -E 'model name|Hardware|CPU part'; nproc; free; df -h",
	"sha256sum '/usr/bin/MPC Sample' /usr/lib/systemd/system/ac50.service /usr/bin/launch-ac50",
	"systemctl cat ac50.service",
	"cat /usr/bin/launch-ac50",
	"systemctl list-dependencies ac50.service --no-pager",
	"systemctl list-dependencies --reverse ac50.service --no-pager",
	"systemctl list-units --type=service --no-pager --all",
	"systemctl list-unit-files --no-pager | grep -i -E 'ssh|dropbear|telnet|adb|gadget'",
	"command -v sshd dropbear dbclient ssh telnetd adbd 2>&1; ls /etc/ssh /etc/dropbear 2>&1",
	"(ss -ltnup || netstat -ltnup) 2>&1",
	"ip -br addr 2>&1 || ifconfig -a",
	"ls /sys/kernel/config/usb_gadget/*/functions 2>&1; ls /sys/kernel/config/usb_gadget/*/configs/*/ 2>&1",
	"mount",
	"cat /proc/asound/cards; cat /proc/asound/devices",
	"for f in /proc/asound/card*/pcm*/info /proc/asound/card*/midi*; do echo \"== $f\"; cat $f; done",
	"ls -l /dev/snd /dev/dri /dev/snd/by-path 2>&1",
	"cat /sys/class/drm/*/modes 2>&1; ls /sys/class/backlight 2>&1",
	"ldd --version 2>&1 | head -1; busybox 2>&1 | head -1",
	"systemctl --version | head -1",
	"id; ulimit -a",
}

func runCmds(path string) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	for _, c := range systemCmds {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		out, err := exec.CommandContext(ctx, "/bin/sh", "-c", c).CombinedOutput()
		cancel()
		fmt.Fprintf(f, "\n##### $ %s\n%s", c, out)
		if err != nil {
			fmt.Fprintf(f, "[exit: %v]\n", err)
		}
		f.Sync()
	}
}

type key struct {
	kind     string
	ch, data byte
}
type stat struct {
	n        int
	min, max byte
}

func decode(m []byte) (string, key, byte) {
	st := m[0]
	ch := st&0x0F + 1
	switch st & 0xF0 {
	case 0x80:
		return fmt.Sprintf("NOTE_OFF ch%d note=%d vel=%d", ch, m[1], m[2]), key{"note", ch, m[1]}, m[2]
	case 0x90:
		return fmt.Sprintf("NOTE_ON  ch%d note=%d vel=%d", ch, m[1], m[2]), key{"note", ch, m[1]}, m[2]
	case 0xA0:
		return fmt.Sprintf("POLY_AT  ch%d note=%d val=%d", ch, m[1], m[2]), key{"polyat", ch, m[1]}, m[2]
	case 0xB0:
		return fmt.Sprintf("CC       ch%d cc=%d val=%d", ch, m[1], m[2]), key{"cc", ch, m[1]}, m[2]
	case 0xD0:
		return fmt.Sprintf("CHAN_AT  ch%d val=%d", ch, m[1]), key{"chanat", ch, 0}, m[1]
	case 0xE0:
		return fmt.Sprintf("PITCH    ch%d lsb=%d msb=%d", ch, m[1], m[2]), key{"pitch", ch, 0}, m[2]
	}
	return "OTHER", key{"other", 0, st}, 0
}

func msgLen(st byte) int {
	switch st & 0xF0 {
	case 0xC0, 0xD0:
		return 2
	}
	return 3
}

func hexs(b []byte) string {
	s := make([]string, len(b))
	for i, x := range b {
		s[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(s, " ")
}

func padColour(pad, r, g, b byte) []byte {
	return []byte{0xF0, 0x47, 0x7F, 0x5C, 0x65, 0x00, 0x04, pad, r, g, b, 0xF7}
}

func main() {
	duration := flag.Duration("duration", 90*time.Second, "capture time")
	initSysex := flag.Bool("init-sysex", true, "send the stock init SysEx")
	flag.Parse()

	logs := filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "logs")
	os.MkdirAll(logs, 0755)
	lk := os.NewFile(link.FD, "link")
	var lkMu sync.Mutex
	send := func(c byte) {
		lkMu.Lock()
		defer lkMu.Unlock()
		if lk != nil {
			lk.Write([]byte{c})
		}
	}
	stop := make(chan struct{})
	go func() { // heartbeat independent of slow work below
		t := time.NewTicker(200 * time.Millisecond)
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				send(link.Heartbeat)
			}
		}
	}()

	cf, _ := os.Create(filepath.Join(logs, "probe-controls.txt"))
	defer cf.Close()
	note := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Println(line)
		if cf != nil {
			fmt.Fprintln(cf, line)
			cf.Sync()
		}
	}

	scr, err := panel.OpenScreen("/dev/dri/card0")
	if err != nil {
		note("SCREEN_FAILED %v", err)
	} else {
		note("SCREEN_OK %dx%d", scr.W, scr.H)
	}
	cs, dev, err := panel.OpenControlSurface(*initSysex)
	if err != nil {
		note("CONTROL_SURFACE_FAILED %v", err)
	} else {
		note("CONTROL_SURFACE_OK %s init_sysex=%v", dev, *initSysex)
		for p := byte(0); p < 16; p++ {
			cs.Write(padColour(p, 0, 0, 40))
		}
		note("PADS_SET_BLUE (if the pads did not turn blue, the colour SysEx is wrong)")
	}
	send(link.Ready)

	sysDone := make(chan struct{})
	go func() { runCmds(filepath.Join(logs, "probe-system.txt")); close(sysDone) }()

	stats := map[key]*stat{}
	var recent []string
	start := time.Now()
	end := start.Add(*duration)
	buf := make([]byte, 512)
	var msg, sysex []byte
	lastDraw := time.Time{}
	for time.Now().Before(end) {
		if cs != nil {
			n := cs.ReadRaw(buf)
			for _, b := range buf[:n] {
				t := time.Since(start).Seconds()
				switch {
				case b == 0xF0:
					sysex = []byte{b}
				case b == 0xF7 && sysex != nil:
					sysex = append(sysex, b)
					note("t=%7.3f SYSEX %s", t, hexs(sysex))
					sysex = nil
				case sysex != nil:
					sysex = append(sysex, b)
				case b >= 0xF8:
					// realtime: ignore
				case b&0x80 != 0:
					msg = []byte{b}
				case len(msg) > 0:
					msg = append(msg, b)
					if len(msg) == msgLen(msg[0]) {
						if len(msg) == 2 {
							msg = append(msg, 0)
						}
						d, k, v := decode(msg)
						note("t=%7.3f RAW %s  %s", t, hexs(msg[:msgLen(msg[0])]), d)
						s := stats[k]
						if s == nil {
							s = &stat{min: v, max: v}
							stats[k] = s
						}
						s.n++
						if v < s.min {
							s.min = v
						}
						if v > s.max {
							s.max = v
						}
						recent = append(recent, d)
						if len(recent) > 6 {
							recent = recent[1:]
						}
						msg = msg[:1] // running status
					}
				}
			}
			if n == 0 {
				time.Sleep(5 * time.Millisecond)
			}
		} else {
			time.Sleep(50 * time.Millisecond)
		}
		if scr != nil && time.Since(lastDraw) > 250*time.Millisecond {
			lastDraw = time.Now()
			scr.Fill(0x00102030)
			scr.Text(6, 6, 2, 0x00FFFFFF, "FM-STEP PROBE")
			scr.Text(6, 30, 1, 0x00FFFF80, "PRESS EVERY PAD SOFT AND HARD")
			scr.Text(6, 42, 1, 0x00FFFF80, "PRESS EVERY BUTTON, TURN K1 K2 K3,")
			scr.Text(6, 54, 1, 0x00FFFF80, "VOLUME, FADER, TURN + PUSH DATA")
			scr.Text(6, 72, 2, 0x0080FF80, fmt.Sprintf("%d S LEFT", int(time.Until(end).Seconds())+1))
			for i, r := range recent {
				scr.Text(6, 104+i*16, 1, 0x00C0C0C0, r)
			}
			scr.Text(6, 210, 1, 0x00909090, "STOCK RETURNS AUTOMATICALLY")
			scr.Flush()
		}
	}

	note("\n== SUMMARY (kind ch data: count min..max)")
	keys := make([]key, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.ch != b.ch {
			return a.ch < b.ch
		}
		return a.data < b.data
	})
	for _, k := range keys {
		s := stats[k]
		note("%-7s ch%-2d %3d: %4d  %3d..%3d", k.kind, k.ch, k.data, s.n, s.min, s.max)
	}

	// Release hardware, then hand back to stock.
	if scr != nil {
		scr.Fill(0)
		scr.Text(6, 100, 2, 0x00FFFFFF, "RETURNING TO STOCK")
		scr.Flush()
		scr.Close()
	}
	if cs != nil {
		for p := byte(0); p < 16; p++ {
			cs.Write(padColour(p, 0, 0, 0))
		}
		cs.Close()
	}
	select {
	case <-sysDone:
	case <-time.After(20 * time.Second):
		note("SYSTEM_INFO_TIMEOUT")
	}
	note("PROBE_DONE")
	close(stop)
	send(link.ExitStock)
	os.Exit(0)
}
