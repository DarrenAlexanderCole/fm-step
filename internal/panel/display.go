// Package panel gives the supervisor its own minimal display and input path
// so it can show a recovery message when neither the stock app nor the
// FM-STEP app is running. It uses only DRM/KMS dumb buffers and ALSA rawmidi.
package panel

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"fmstep/internal/gfx"
)

// ioctl numbers (asm-generic encoding, identical on arm64 and x86-64),
// verified against <drm/drm_mode.h> and <sound/asound.h>.
const (
	drmIoctlSetMaster   = 0x641e
	drmIoctlGetRes      = 0xc04064a0
	drmIoctlGetConn     = 0xc05064a7
	drmIoctlGetEnc      = 0xc01464a6
	drmIoctlCreateDumb  = 0xc02064b2
	drmIoctlMapDumb     = 0xc01064b3
	drmIoctlAddFB       = 0xc01c64ae
	drmIoctlSetCrtc     = 0xc06864a2
	drmIoctlDirtyFB     = 0xc01864b1
	sndRawmidiIoctlInfo = 0x810c5701
)

type cardRes struct { // struct drm_mode_card_res, 64 bytes
	FbIDPtr, CrtcIDPtr, ConnectorIDPtr, EncoderIDPtr     uint64
	CountFbs, CountCrtcs, CountConnectors, CountEncoders uint32
	MinW, MaxW, MinH, MaxH                               uint32
}

type modeInfo struct { // struct drm_mode_modeinfo, 68 bytes
	Clock                                         uint32
	HDisplay, HSyncStart, HSyncEnd, HTotal, HSkew uint16
	VDisplay, VSyncStart, VSyncEnd, VTotal, VScan uint16
	VRefresh, Flags, Type                         uint32
	Name                                          [32]byte
}

type getConn struct { // struct drm_mode_get_connector, 80 bytes
	EncodersPtr, ModesPtr, PropsPtr, PropValuesPtr uint64
	CountModes, CountProps, CountEncoders          uint32
	EncoderID, ConnectorID, ConnectorType          uint32
	ConnectorTypeID, Connection, MmW, MmH          uint32
	Subpixel, Pad                                  uint32
}

type getEnc struct{ EncoderID, EncoderType, CrtcID, PossibleCrtcs, PossibleClones uint32 }

type createDumb struct {
	Height, Width, Bpp, Flags, Handle, Pitch uint32
	Size                                     uint64
}

type mapDumb struct {
	Handle, Pad uint32
	Offset      uint64
}

type fbCmd struct{ FbID, Width, Height, Pitch, Bpp, Depth, Handle uint32 }

type crtcCmd struct { // struct drm_mode_crtc, 104 bytes
	SetConnectorsPtr uint64
	CountConnectors  uint32
	CrtcID, FbID     uint32
	X, Y             uint32
	GammaSize        uint32
	ModeValid        uint32
	Mode             modeInfo
}

type dirtyCmd struct {
	FbID, Flags, Color, NumClips uint32
	ClipsPtr                     uint64
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

// Screen is a 32-bit XRGB dumb framebuffer on the first connected output.
// Screen draws into an off-screen Canvas (C); Flush copies it to the
// display in one go (no flicker, and transitions can compose two frames).
type Screen struct {
	f     *os.File
	W, H  int
	pitch int
	pix   []byte
	fbID  uint32
	C     *gfx.Canvas
	shown []uint32 // what the display holds (for partial updates)
	// LastArea is the number of pixels the last Flush sent (0 = none).
	LastArea int
}

func OpenScreen(dev string) (*Screen, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fd := f.Fd()
	ioctl(fd, drmIoctlSetMaster, nil) // fails harmlessly if already master/none needed

	var res cardRes
	if err := ioctl(fd, drmIoctlGetRes, unsafe.Pointer(&res)); err != nil {
		f.Close()
		return nil, fmt.Errorf("GETRESOURCES: %v", err)
	}
	if res.CountConnectors == 0 || res.CountCrtcs == 0 {
		f.Close()
		return nil, errors.New("no connectors/crtcs")
	}
	conns := make([]uint32, res.CountConnectors)
	crtcs := make([]uint32, res.CountCrtcs)
	res2 := cardRes{
		ConnectorIDPtr:  uint64(uintptr(unsafe.Pointer(&conns[0]))),
		CrtcIDPtr:       uint64(uintptr(unsafe.Pointer(&crtcs[0]))),
		CountConnectors: res.CountConnectors,
		CountCrtcs:      res.CountCrtcs,
	}
	if err := ioctl(fd, drmIoctlGetRes, unsafe.Pointer(&res2)); err != nil {
		f.Close()
		return nil, fmt.Errorf("GETRESOURCES(2): %v", err)
	}
	runtime.KeepAlive(conns)
	runtime.KeepAlive(crtcs)

	for _, cid := range conns {
		var c getConn
		c.ConnectorID = cid
		if ioctl(fd, drmIoctlGetConn, unsafe.Pointer(&c)) != nil || c.Connection != 1 || c.CountModes == 0 {
			continue
		}
		modes := make([]modeInfo, c.CountModes)
		c2 := getConn{ConnectorID: cid, CountModes: c.CountModes,
			ModesPtr: uint64(uintptr(unsafe.Pointer(&modes[0])))}
		err := ioctl(fd, drmIoctlGetConn, unsafe.Pointer(&c2))
		runtime.KeepAlive(modes)
		if err != nil {
			continue
		}
		mode := modes[0]
		crtc := crtcs[0]
		if c2.EncoderID != 0 {
			e := getEnc{EncoderID: c2.EncoderID}
			if ioctl(fd, drmIoctlGetEnc, unsafe.Pointer(&e)) == nil && e.CrtcID != 0 {
				crtc = e.CrtcID
			}
		}
		s, err := setup(f, cid, crtc, mode)
		if err != nil {
			f.Close()
			return nil, err
		}
		return s, nil
	}
	f.Close()
	return nil, errors.New("no connected output")
}

func setup(f *os.File, conn, crtc uint32, mode modeInfo) (*Screen, error) {
	fd := f.Fd()
	cd := createDumb{Width: uint32(mode.HDisplay), Height: uint32(mode.VDisplay), Bpp: 32}
	if err := ioctl(fd, drmIoctlCreateDumb, unsafe.Pointer(&cd)); err != nil {
		return nil, fmt.Errorf("CREATE_DUMB: %v", err)
	}
	fb := fbCmd{Width: cd.Width, Height: cd.Height, Pitch: cd.Pitch, Bpp: 32, Depth: 24, Handle: cd.Handle}
	if err := ioctl(fd, drmIoctlAddFB, unsafe.Pointer(&fb)); err != nil {
		return nil, fmt.Errorf("ADDFB: %v", err)
	}
	md := mapDumb{Handle: cd.Handle}
	if err := ioctl(fd, drmIoctlMapDumb, unsafe.Pointer(&md)); err != nil {
		return nil, fmt.Errorf("MAP_DUMB: %v", err)
	}
	pix, err := syscall.Mmap(int(fd), int64(md.Offset), int(cd.Size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap: %v", err)
	}
	connID := conn
	cc := crtcCmd{SetConnectorsPtr: uint64(uintptr(unsafe.Pointer(&connID))), CountConnectors: 1,
		CrtcID: crtc, FbID: fb.FbID, ModeValid: 1, Mode: mode}
	err = ioctl(fd, drmIoctlSetCrtc, unsafe.Pointer(&cc))
	runtime.KeepAlive(&connID)
	if err != nil {
		return nil, fmt.Errorf("SETCRTC: %v", err)
	}
	return &Screen{f: f, W: int(cd.Width), H: int(cd.Height), pitch: int(cd.Pitch), pix: pix, fbID: fb.FbID, C: gfx.NewCanvas(int(cd.Width), int(cd.Height))}, nil
}

func (s *Screen) Fill(rgb uint32) { s.C.Clear(rgb) }

// Rect fills a solid rectangle (legacy helper, whole pixels).
func (s *Screen) Rect(x, y, w, h int, rgb uint32) {
	s.C.Rect(float64(x), float64(y), float64(w), float64(h), rgb, 1)
}

// Glyph5x7 returns the 5x7 bitmap of r (for dot-matrix text).
func Glyph5x7(r rune) [7]uint8 {
	if g, ok := font[r]; ok {
		return g
	}
	if r >= 'a' && r <= 'z' {
		if g, ok := font[r-32]; ok {
			return g
		}
	}
	return font['?']
}

// Text draws uppercase 5x7 text at (x,y) with an integer scale (legacy).
func (s *Screen) Text(x, y, scale int, rgb uint32, text string) {
	for _, r := range strings.ToUpper(text) {
		g, ok := font[r]
		if !ok {
			g = font['?']
		}
		for gy := 0; gy < 7; gy++ {
			for gx := 0; gx < 5; gx++ {
				if g[gy]&(0x10>>gx) == 0 {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						px, py := x+gx*scale+dx, y+gy*scale+dy
						if px >= 0 && py >= 0 && px < s.W && py < s.H {
							s.C.Pix[py*s.W+px] = rgb
						}
					}
				}
			}
		}
		x += 6 * scale
	}
}

// Flush copies what changed on the canvas to the display buffer and tells
// manual-update panels (SPI mipi-dbi) to send only that rectangle. A full
// 320x240 transfer takes ~54 ms on the MPC, so small changes are much faster
// and an unchanged frame costs nothing.
func (s *Screen) Flush() {
	if s.pix == nil {
		return
	}
	full := s.shown == nil
	if full {
		s.shown = make([]uint32, s.W*s.H)
	}
	x0, y0, x1, y1 := s.W, s.H, -1, -1
	for y := 0; y < s.H; y++ {
		row := s.pix[y*s.pitch:]
		cr := s.C.Pix[y*s.W : (y+1)*s.W]
		sr := s.shown[y*s.W : (y+1)*s.W]
		for x, v := range cr {
			if v == sr[x] && !full {
				continue
			}
			sr[x] = v
			*(*uint32)(unsafe.Pointer(&row[x*4])) = v
			if x < x0 {
				x0 = x
			}
			if x > x1 {
				x1 = x
			}
			if y < y0 {
				y0 = y
			}
			y1 = y
		}
	}
	if x1 < 0 {
		s.LastArea = 0
		return
	}
	s.LastArea = (x1 - x0 + 1) * (y1 - y0 + 1)
	s.dirty(x0, y0, x1+1, y1+1)
}

// Invalidate makes the next Flush send the whole frame.
func (s *Screen) Invalidate() { s.shown = nil }

type clipRect struct{ X1, Y1, X2, Y2 uint16 }

func (s *Screen) dirty(x0, y0, x1, y1 int) {
	if s.f == nil {
		return
	}
	clip := clipRect{uint16(x0), uint16(y0), uint16(x1), uint16(y1)}
	d := dirtyCmd{FbID: s.fbID, NumClips: 1, ClipsPtr: uint64(uintptr(unsafe.Pointer(&clip)))}
	ioctl(s.f.Fd(), drmIoctlDirtyFB, unsafe.Pointer(&d))
	runtime.KeepAlive(&clip)
}

func (s *Screen) Close() {
	if s.f == nil {
		return
	}
	if s.pix != nil {
		syscall.Munmap(s.pix)
	}
	s.f.Close()
}

// NewMemScreen returns an off-screen framebuffer (for layout previews/tests).
func NewMemScreen(w, h int) *Screen {
	return &Screen{W: w, H: h, pitch: w * 4, pix: make([]byte, w*h*4), C: gfx.NewCanvas(w, h)}
}

// RGBA returns pixel (x,y) as 0xRRGGBB.
func (s *Screen) RGB(x, y int) uint32 {
	return s.C.Pix[y*s.W+x] & 0xFFFFFF
}
