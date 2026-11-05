// Package controls is the MPC Sample control-surface map, captured on the
// user's device (firmware 1.3.0.12, lab runs 2026-11-03/04).
//
// The "Control Surface" rawmidi port delivers timestamp-framed records; use
// panel.ControlSurface.Read, which deframes them.
package controls

// Pads: channel 10. Note on 0x99 (velocity), note off 0x89, poly pressure 0xA9.
// Index 0 = pad 1 (bottom-left) ... index 15 = pad 16 (top-right).
var PadNotes = [16]byte{37, 36, 42, 82, 40, 38, 46, 44, 48, 47, 45, 43, 49, 55, 51, 53}

// Knobs and fader: channel 1 CC, absolute 0..127. The MCU sends all current
// positions after the init SysEx.
const (
	CCK1     = 16
	CCK2     = 17
	CCK3     = 18
	CCVolume = 19 // MAIN VOLUME (purple)
	CCFader  = 20
	CCData   = 100 // DATA encoder, relative: 1 = +1, 127 = -1
)

// Buttons: channel 1, note on 0x90 vel 127 on press, note off 0x80 on release.
const (
	BtnScreenLeft   = 12 // NOT captured (skipped); 12 inferred from 13/14 and am-mod
	BtnScreenMiddle = 13
	BtnScreenRight  = 14
	BtnSample       = 6
	BtnSeq          = 7
	BtnPadFX        = 1
	BtnKnobFX       = 0
	BtnShift        = 49
	BtnPadBank      = 35
	BtnChop         = 75
	BtnMute         = 43
	BtnLoop         = 74
	Btn16Levels     = 40
	BtnSampleSelect = 36
	BtnTapTempo     = 53
	BtnMinus        = 55
	BtnPlus         = 54
	BtnSampleRecord = 73
	BtnSeqRecord    = 80
	BtnStop         = 81
	BtnPlay         = 82
	BtnErase        = 9
	BtnNoteRepeat   = 11
	BtnDataPush     = 111
)

// PadColour returns the SysEx that sets pad (0..15) to a 7-bit RGB colour.
func PadColour(pad, r, g, b byte) []byte {
	return []byte{0xF0, 0x47, 0x7F, 0x5C, 0x65, 0x00, 0x04, pad, r & 0x7F, g & 0x7F, b & 0x7F, 0xF7}
}
