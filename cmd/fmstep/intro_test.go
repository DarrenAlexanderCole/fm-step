package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fmstep/internal/panel"
)

// TestIntro checks that a frame of the startup moment is cheap enough to
// keep up ~30 fps, that runIntro waits for "ready" and ends promptly after
// it, and (with FMSTEP_GIF=dir) writes its frames for tools/make_gifs.py.
func TestIntro(t *testing.T) {
	scr := panel.NewMemScreen(320, 240)
	var worst time.Duration
	dir := os.Getenv("FMSTEP_GIF")
	if dir != "" {
		os.MkdirAll(filepath.Join(dir, "intro"), 0755)
	}
	n := 0
	for ft := 0.0; ft < 2.6; ft += 1.0 / 30 {
		out := 0.0
		if ft > 2.25 {
			out = (ft - 2.25) / 0.35
		}
		s := time.Now()
		drawIntro(scr.C, ft, out, appVersion)
		if d := time.Since(s); d > worst {
			worst = d
		}
		if dir != "" {
			img := image.NewRGBA(image.Rect(0, 0, 320, 240))
			for y := 0; y < 240; y++ {
				for x := 0; x < 320; x++ {
					v := scr.RGB(x, y)
					img.Set(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
				}
			}
			f, _ := os.Create(filepath.Join(dir, "intro", fmt.Sprintf("%03d.png", n)))
			png.Encode(f, img)
			f.Close()
		}
		n++
	}
	t.Logf("worst intro frame %v on this machine", worst)
	if worst > 15*time.Millisecond {
		t.Errorf("intro frame too slow: %v", worst)
	}

	// Loading finishes early: the moment still plays its minimum, then fades.
	in := &intro{ready: make(chan struct{}), done: make(chan struct{}), ver: appVersion, min: introMin}
	close(in.ready)
	s := time.Now()
	go runIntro(scr, in, t.Logf)
	<-in.done
	if d := time.Since(s); d < introMin || d > introMin+introOut+300*time.Millisecond {
		t.Errorf("early ready: intro took %v", d)
	}

	// Loading is slow: the moment keeps playing until ready, then ends quickly.
	in = &intro{ready: make(chan struct{}), done: make(chan struct{}), ver: appVersion, min: introMin}
	go runIntro(scr, in, t.Logf)
	time.Sleep(2500 * time.Millisecond)
	select {
	case <-in.done:
		t.Fatal("intro ended before ready")
	default:
	}
	s = time.Now()
	close(in.ready)
	<-in.done
	if d := time.Since(s); d > introOut+200*time.Millisecond {
		t.Errorf("late ready: intro took %v to land", d)
	}
}

func TestIntroBudget(t *testing.T) {
	f := filepath.Join(t.TempDir(), "launch_uptime")
	if got := introBudget(f, 20); got != introMin {
		t.Errorf("no file: %v", got)
	}
	os.WriteFile(f, []byte("10.00\n"), 0644)
	cases := []struct {
		now  float64
		want time.Duration
	}{
		{11.0, introMin},                // plenty left: full moment
		{13.0, 1550 * time.Millisecond}, // 5 - 3 - 0.35 - 0.1
		{14.6, introFloor},              // already late: still a short visible moment
	}
	for _, c := range cases {
		if got := introBudget(f, c.now); got != c.want {
			t.Errorf("now=%v: got %v want %v", c.now, got, c.want)
		}
	}
}
