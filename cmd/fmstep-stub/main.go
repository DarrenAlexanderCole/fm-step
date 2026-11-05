// fmstep-stub: a stand-in for the FM-STEP application, used to test the
// launcher and supervisor on the device before any audio code exists.
// It touches NO hardware. Behaviour is chosen with -mode:
//
//	ok           ready + heartbeats until -exit-after, then Exit to Stock
//	crash        ready + heartbeats, then SIGSEGV-style exit after -after
//	freeze       ready + heartbeats, then stops heartbeating (hangs)
//	nostart      never sends ready (hangs)
//	failstart    exits with code 1 immediately
//	reboot       ready, then requests a controlled reboot after -after
//	killsup      ready, then SIGKILLs the supervisor after -after (supervisor-crash test)
//	run          ready + heartbeats forever (exit with the supervisor)
//
// The mode can also come from <SD>/FMSTEP/stub_mode.txt so it can be changed
// on the card without rebuilding (and without touching autorun.check).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "", "behaviour (see source)")
	after := flag.Duration("after", 15*time.Second, "when crash/freeze/reboot happens")
	exitAfter := flag.Duration("exit-after", 30*time.Second, "ok mode: when to exit to stock")
	flag.Parse()

	if *mode == "" {
		*mode = "ok"
		if b, err := os.ReadFile(filepath.Join(os.Getenv("FMSTEP_SD"), "FMSTEP", "stub_mode.txt")); err == nil {
			f := strings.Fields(string(b))
			if len(f) > 0 {
				*mode = f[0]
			}
			if len(f) > 1 {
				if d, err := time.ParseDuration(f[1]); err == nil {
					*after, *exitAfter = d, d
				}
			}
		}
	}
	link := os.NewFile(3, "link")
	send := func(c byte) {
		if link != nil {
			link.Write([]byte{c})
		}
	}
	fmt.Printf("stub: mode=%s after=%s pid=%d\n", *mode, *after, os.Getpid())

	switch *mode {
	case "failstart":
		os.Exit(1)
	case "nostart":
		for {
			time.Sleep(time.Hour) // hang (a bare select{} would trip the Go deadlock detector)
		}
	}
	time.Sleep(500 * time.Millisecond) // pretend to open devices
	send('R')
	start := time.Now()
	for {
		send('H')
		time.Sleep(200 * time.Millisecond)
		el := time.Since(start)
		switch *mode {
		case "ok":
			if el >= *exitAfter {
				// Real app: stop sequencer, release voices, save, close audio/midi/drm.
				fmt.Println("stub: exit to stock")
				send('X')
				os.Exit(0)
			}
		case "crash":
			if el >= *after {
				fmt.Println("stub: crashing")
				os.Exit(139)
			}
		case "freeze":
			if el >= *after {
				fmt.Println("stub: freezing")
				for {
					time.Sleep(time.Hour) // hang (a bare select{} would trip the Go deadlock detector)
				}
			}
		case "killsup":
			if el >= *after {
				// Simulates a supervisor crash: SIGKILL our parent (the supervisor).
				fmt.Println("stub: killing supervisor")
				syscall.Kill(os.Getppid(), syscall.SIGKILL)
				*mode = "run"
			}
		case "reboot":
			if el >= *after {
				send('B')
				os.Exit(0)
			}
		}
	}
}
