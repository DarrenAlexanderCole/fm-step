// Package audio is a minimal ALSA PCM playback client using raw kernel
// ioctls (no alsa-lib), so FM-STEP stays a single static binary.
// Struct layouts verified against <sound/asound.h> (same on arm64/x86-64).
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ioctlHwRefine = 0xc2604110
	ioctlHwParams = 0xc2604111
	ioctlSwParams = 0xc0884113
	ioctlPrepare  = 0x4140
	ioctlWriteI   = 0x40184150
	ioctlDrain    = 0x4144
	ioctlDrop     = 0x4143

	hwSize       = 608
	offMasks     = 4
	offIntervals = 260
	offRmask     = 512
	offCmask     = 516
	offInfo      = 520

	paramAccess     = 0
	paramFormat     = 1
	paramSubfmt     = 2
	paramFirstInt   = 8
	ParamSampleBits = 8
	ParamChannels   = 10
	ParamRate       = 11
	ParamPeriodSize = 13
	ParamPeriods    = 15
	ParamBufferSize = 17
	paramLastInt    = 19

	accessRWInterleaved = 3
	FormatS16LE         = 2
	FormatS24LE         = 6
	FormatS32LE         = 10
)

type hwParams [hwSize]byte

func (h *hwParams) u32(off int) uint32              { return binary.LittleEndian.Uint32(h[off:]) }
func (h *hwParams) put(off int, v uint32)           { binary.LittleEndian.PutUint32(h[off:], v) }
func maskOff(p int) int                             { return offMasks + p*32 }
func intOff(p int) int                              { return offIntervals + (p-paramFirstInt)*12 }
func (h *hwParams) Interval(p int) (uint32, uint32) { o := intOff(p); return h.u32(o), h.u32(o + 4) }

func (h *hwParams) any() {
	*h = hwParams{}
	for p := 0; p <= paramSubfmt; p++ {
		for i := 0; i < 8; i++ {
			h.put(maskOff(p)+i*4, 0xffffffff)
		}
	}
	for p := paramFirstInt; p <= paramLastInt; p++ {
		o := intOff(p)
		h.put(o, 0)
		h.put(o+4, 0xffffffff)
		h.put(o+8, 0)
	}
	h.put(offRmask, 0xffffffff)
	h.put(offCmask, 0)
	h.put(offInfo, 0xffffffff)
}

func (h *hwParams) setMask(p int, bit uint) {
	o := maskOff(p)
	for i := 0; i < 8; i++ {
		h.put(o+i*4, 0)
	}
	h.put(o+int(bit/32)*4, 1<<(bit%32))
}

func (h *hwParams) maskHas(p int, bit uint) bool {
	return h.u32(maskOff(p)+int(bit/32)*4)&(1<<(bit%32)) != 0
}

func (h *hwParams) setInterval(p int, min, max uint32) {
	o := intOff(p)
	h.put(o, min)
	h.put(o+4, max)
	h.put(o+8, 1<<2) // integer
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
		if e == syscall.EINTR {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

// Capabilities describes what the device accepts (from HW_REFINE).
func Capabilities(dev string) (string, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var h hwParams
	h.any()
	if err := ioctl(f.Fd(), ioctlHwRefine, unsafe.Pointer(&h)); err != nil {
		return "", fmt.Errorf("HW_REFINE: %v", err)
	}
	var fm []string
	for _, x := range []struct {
		b uint
		n string
	}{{FormatS16LE, "S16_LE"}, {FormatS24LE, "S24_LE"}, {FormatS32LE, "S32_LE"}, {32, "S24_3LE"}} {
		if h.maskHas(paramFormat, x.b) {
			fm = append(fm, x.n)
		}
	}
	s := fmt.Sprintf("formats=%s interleaved=%v", strings.Join(fm, ","), h.maskHas(paramAccess, accessRWInterleaved))
	for _, p := range []struct {
		id int
		n  string
	}{{ParamChannels, "channels"}, {ParamRate, "rate"}, {ParamPeriodSize, "period_size"}, {ParamPeriods, "periods"}, {ParamBufferSize, "buffer_size"}} {
		a, b := h.Interval(p.id)
		s += fmt.Sprintf(" %s=%d..%d", p.n, a, b)
	}
	return s, nil
}

// PCM is an open, prepared playback stream.
type PCM struct {
	f              *os.File
	Channels       int
	Rate           int
	Format         int
	Period         int // frames
	Buffer         int // frames
	BytesPerSample int
	Xruns          int
}

// Open configures interleaved playback. Channels/rate are exact; format is
// the first of `formats` the device accepts.
func Open(dev string, channels, rate, period, periods int, formats []int) (*PCM, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fd := f.Fd()
	var h hwParams
	h.any()
	if err := ioctl(fd, ioctlHwRefine, unsafe.Pointer(&h)); err != nil {
		f.Close()
		return nil, fmt.Errorf("HW_REFINE: %v", err)
	}
	format := -1
	for _, fm := range formats {
		if h.maskHas(paramFormat, uint(fm)) {
			format = fm
			break
		}
	}
	if format < 0 {
		f.Close()
		return nil, errors.New("no supported sample format")
	}
	try := func(withPeriod bool) error {
		h.any()
		h.setMask(paramAccess, accessRWInterleaved)
		h.setMask(paramFormat, uint(format))
		h.setInterval(ParamChannels, uint32(channels), uint32(channels))
		h.setInterval(ParamRate, uint32(rate), uint32(rate))
		if withPeriod {
			h.setInterval(ParamPeriodSize, uint32(period), uint32(period))
			h.setInterval(ParamPeriods, uint32(periods), uint32(periods))
		}
		return ioctl(fd, ioctlHwParams, unsafe.Pointer(&h))
	}
	if err := try(true); err != nil {
		if err2 := try(false); err2 != nil {
			f.Close()
			return nil, fmt.Errorf("HW_PARAMS: %v / %v", err, err2)
		}
	}
	ps, _ := h.Interval(ParamPeriodSize)
	bs, _ := h.Interval(ParamBufferSize)
	p := &PCM{f: f, Channels: channels, Rate: rate, Format: format, Period: int(ps), Buffer: int(bs)}
	p.BytesPerSample = map[int]int{FormatS16LE: 2, FormatS24LE: 4, FormatS32LE: 4}[format]

	var sw [136]byte
	le := binary.LittleEndian
	le.PutUint64(sw[16:], uint64(ps))      // avail_min
	le.PutUint64(sw[24:], 1)               // xfer_align
	le.PutUint64(sw[32:], uint64(ps)*2)    // start_threshold
	le.PutUint64(sw[40:], uint64(bs))      // stop_threshold
	le.PutUint32(sw[72:], 0x00020000|0x0f) // proto (SNDRV_PCM_VERSION 2.0.15)
	if err := ioctl(fd, ioctlSwParams, unsafe.Pointer(&sw)); err != nil {
		f.Close()
		return nil, fmt.Errorf("SW_PARAMS: %v", err)
	}
	if err := ioctl(fd, ioctlPrepare, nil); err != nil {
		f.Close()
		return nil, fmt.Errorf("PREPARE: %v", err)
	}
	return p, nil
}

type xferi struct {
	Result int64
	Buf    uintptr
	Frames uint64
}

// Write writes interleaved float samples (len = frames*Channels, -1..1),
// converting to the device format. Recovers from underruns.
func (p *PCM) Write(samples []float32, scratch []byte) error {
	frames := len(samples) / p.Channels
	need := len(samples) * p.BytesPerSample
	b := scratch[:need]
	for i, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		switch p.Format {
		case FormatS16LE:
			binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(s*32767)))
		case FormatS24LE:
			binary.LittleEndian.PutUint32(b[i*4:], uint32(int32(s*8388607)))
		default:
			binary.LittleEndian.PutUint32(b[i*4:], uint32(int32(float64(s)*2147483647)))
		}
	}
	off := 0
	for off < frames {
		x := xferi{Buf: uintptr(unsafe.Pointer(&b[off*p.Channels*p.BytesPerSample])), Frames: uint64(frames - off)}
		err := ioctl(p.f.Fd(), ioctlWriteI, unsafe.Pointer(&x))
		runtime.KeepAlive(b)
		if err == syscall.EPIPE {
			p.Xruns++
			if err := ioctl(p.f.Fd(), ioctlPrepare, nil); err != nil {
				return fmt.Errorf("re-PREPARE: %v", err)
			}
			continue
		}
		if err != nil {
			return err
		}
		off += int(x.Result)
	}
	return nil
}

// Close stops immediately (drop) or plays out (drain) and closes the device.
func (p *PCM) Close(drain bool) {
	if drain {
		ioctl(p.f.Fd(), ioctlDrain, nil)
	} else {
		ioctl(p.f.Fd(), ioctlDrop, nil)
	}
	p.f.Close()
}

func (p *PCM) String() string {
	fm := map[int]string{FormatS16LE: "S16_LE", FormatS24LE: "S24_LE", FormatS32LE: "S32_LE"}[p.Format]
	return fmt.Sprintf("%dch %dHz %s period=%d buffer=%d", p.Channels, p.Rate, fm, p.Period, p.Buffer)
}
