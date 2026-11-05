package panel

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

type rawmidiInfo struct { // struct snd_rawmidi_info, 268 bytes
	Device, Subdevice uint32
	Stream            int32
	Card              int32
	Flags             uint32
	ID                [64]byte
	Name              [80]byte
	Subname           [32]byte
	SubdevicesCount   uint32
	SubdevicesAvail   uint32
	Reserved          [64]byte
}

// ControlSurface is the panel MCU's rawmidi port ("Control Surface").
type ControlSurface struct {
	f      *os.File
	framed int // 0 unknown, 1 framed (32-byte timestamped records), -1 plain
	raw    []byte
}

// Init SysEx the stock app sends to the panel MCU (purpose unknown).
var InitSysex = []byte{0xF0, 0x47, 0x7F, 0x5C, 0x42, 0x00, 0x00, 0xF7}

func OpenControlSurface(sendInit bool) (*ControlSurface, string, error) {
	devs, _ := filepath.Glob("/dev/snd/midiC*D*")
	for _, d := range devs {
		f, err := os.OpenFile(d, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		var info rawmidiInfo
		if ioctl(f.Fd(), sndRawmidiIoctlInfo, unsafe.Pointer(&info)) == nil {
			name := string(bytes.TrimRight(info.Name[:], "\x00"))
			id := string(bytes.TrimRight(info.ID[:], "\x00"))
			if bytes.Contains([]byte(name+" "+id), []byte("Control Surface")) {
				cs := &ControlSurface{f: f}
				if sendInit {
					f.Write(InitSysex)
				}
				return cs, d, nil
			}
		}
		f.Close()
	}
	return nil, "", errors.New("Control Surface rawmidi port not found")
}

type Key int

const (
	KeyNone Key = iota
	KeyPad
	KeyEncoderPush
)

// Wait returns the next pad press or encoder push, or KeyNone at the deadline.
// Pads: 0x99 note vel>0 (hardware-verified in virtual-mpc-sample PANEL.md).
// Encoder push: 0x90 0x6F vel>0 (am-mod uses code 0x6F; not hardware-verified by us).
func (c *ControlSurface) Wait(deadline time.Time) Key {
	buf := make([]byte, 512)
	var msg []byte
	for time.Now().Before(deadline) {
		data := c.Read(buf)
		if len(data) == 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		for _, b := range data {
			if b >= 0xF8 {
				continue
			}
			if b&0x80 != 0 {
				msg = []byte{b}
				continue
			}
			if len(msg) == 0 {
				continue
			}
			msg = append(msg, b)
			if len(msg) == 3 {
				st, d1, d2 := msg[0], msg[1], msg[2]
				msg = msg[:1] // running status
				if st == 0x99 && d2 > 0 {
					return KeyPad
				}
				if st == 0x90 && d1 == 0x6F && d2 > 0 {
					return KeyEncoderPush
				}
			}
		}
	}
	return KeyNone
}

// isFramed reports whether a read looks like SNDRV_RAWMIDI_MODE_FRAMING_TSTAMP
// records: 32 bytes each = type(0) len(1..16) 0 0 nsec[4] sec[8] data[16].
// Device-confirmed 2026-11-03: the MPC Sample's Control Surface port delivers
// these records even though we never enabled framing.
func isFramed(b []byte) bool {
	if len(b) == 0 || len(b)%32 != 0 {
		return false
	}
	for i := 0; i < len(b); i += 32 {
		if b[i] != 0 || b[i+1] == 0 || b[i+1] > 16 || b[i+2] != 0 || b[i+3] != 0 {
			return false
		}
	}
	return true
}

// Read does one non-blocking read and returns plain MIDI bytes, removing the
// timestamp framing when the port delivers framed records. The returned slice
// aliases buf. Raw bytes of the last read are available from LastRaw.
func (c *ControlSurface) Read(buf []byte) []byte {
	// Go parks a goroutine reading a pollable non-blocking fd until data
	// arrives, so without a deadline this "non-blocking" read would block
	// until a control is touched (device run 2026-11-04: the UI loop stalled
	// and the supervisor's heartbeat check killed the app).
	c.f.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, err := c.f.Read(buf)
	if err != nil || n <= 0 {
		return nil
	}
	c.raw = append(c.raw[:0], buf[:n]...)
	if c.framed == 0 {
		if isFramed(buf[:n]) {
			c.framed = 1
		} else {
			c.framed = -1
		}
	}
	if c.framed < 0 || !isFramed(buf[:n]) {
		return buf[:n]
	}
	out := buf[:0]
	for i := 0; i < n; i += 32 {
		l := int(c.raw[i+1])
		out = append(out, c.raw[i+16:i+16+l]...)
	}
	return out
}

// LastRaw returns the undecoded bytes of the most recent Read.
func (c *ControlSurface) LastRaw() []byte { return c.raw }

// Framed reports the detected port mode: 1 framed, -1 plain, 0 unknown yet.
func (c *ControlSurface) Framed() int { return c.framed }

func (c *ControlSurface) Close() { c.f.Close() }

// ReadRaw does one non-blocking read of raw MIDI bytes (n=0 if none pending).
func (c *ControlSurface) ReadRaw(buf []byte) int {
	c.f.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, err := c.f.Read(buf)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// Write sends raw bytes to the panel MCU.
func (c *ControlSurface) Write(b []byte) error {
	_, err := c.f.Write(b)
	return err
}
