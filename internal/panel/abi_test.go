package panel

import (
	"testing"
	"unsafe"
)

// Sizes from <drm/drm_mode.h> and <sound/asound.h> (identical on arm64).
func TestABISizes(t *testing.T) {
	for name, got := range map[string]uintptr{
		"drm_mode_card_res": unsafe.Sizeof(cardRes{}), "drm_mode_modeinfo": unsafe.Sizeof(modeInfo{}),
		"drm_mode_get_connector": unsafe.Sizeof(getConn{}), "drm_mode_get_encoder": unsafe.Sizeof(getEnc{}),
		"drm_mode_create_dumb": unsafe.Sizeof(createDumb{}), "drm_mode_map_dumb": unsafe.Sizeof(mapDumb{}),
		"drm_mode_fb_cmd": unsafe.Sizeof(fbCmd{}), "drm_mode_crtc": unsafe.Sizeof(crtcCmd{}),
		"drm_mode_fb_dirty_cmd": unsafe.Sizeof(dirtyCmd{}), "snd_rawmidi_info": unsafe.Sizeof(rawmidiInfo{}),
	} {
		want := map[string]uintptr{"drm_mode_card_res": 64, "drm_mode_modeinfo": 68, "drm_mode_get_connector": 80,
			"drm_mode_get_encoder": 20, "drm_mode_create_dumb": 32, "drm_mode_map_dumb": 16, "drm_mode_fb_cmd": 28,
			"drm_mode_crtc": 104, "drm_mode_fb_dirty_cmd": 24, "snd_rawmidi_info": 268}[name]
		if got != want {
			t.Errorf("%s: size %d, want %d", name, got, want)
		}
	}
	if off := unsafe.Offsetof(crtcCmd{}.Mode); off != 36 {
		t.Errorf("drm_mode_crtc.mode offset %d, want 36", off)
	}
	if off := unsafe.Offsetof(rawmidiInfo{}.Name); off != 84 {
		t.Errorf("snd_rawmidi_info.name offset %d, want 84", off)
	}
}
