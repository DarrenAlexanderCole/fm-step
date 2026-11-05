// Package state holds the persistent launch state on the SD card.
//
// Layout (shared with launcher/autorun.sh, so plain text only):
//
//	FMSTEP/ENABLE                   user-created: custom startup allowed
//	FMSTEP/DISABLED_BY_BREAKER.txt  written when the circuit breaker trips
//	FMSTEP/state/launch_pending     exists from launch until "healthy"
//	FMSTEP/state/consecutive_failures   integer
//	FMSTEP/state/crash_count            integer, total runtime crashes
//	FMSTEP/state/last_result            one word, last session outcome
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Store struct{ Root string } // Root = <SD>/FMSTEP

func (s Store) dir() string          { return filepath.Join(s.Root, "state") }
func (s Store) p(name string) string { return filepath.Join(s.dir(), name) }

func (s Store) Available() bool {
	st, err := os.Stat(s.dir())
	return err == nil && st.IsDir()
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func (s Store) readInt(name string) int {
	b, err := os.ReadFile(s.p(name))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s Store) writeInt(name string, n int) error {
	return writeAtomic(s.p(name), []byte(strconv.Itoa(n)+"\n"))
}

func (s Store) ConsecutiveFailures() int { return s.readInt("consecutive_failures") }
func (s Store) CrashCount() int          { return s.readInt("crash_count") }

func (s Store) SetConsecutiveFailures(n int) error { return s.writeInt("consecutive_failures", n) }

func (s Store) IncCrashCount() (int, error) {
	n := s.CrashCount() + 1
	return n, s.writeInt("crash_count", n)
}

func (s Store) ClearPending() error {
	err := os.Remove(s.p("launch_pending"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s Store) Pending() bool {
	_, err := os.Stat(s.p("launch_pending"))
	return err == nil
}

func (s Store) SetLastResult(r string) error {
	return writeAtomic(s.p("last_result"), []byte(r+"\n"))
}

// RecordFailedLaunch counts a launch that never became healthy and trips the
// circuit breaker at the threshold. Returns the new count and whether the
// breaker tripped.
func (s Store) RecordFailedLaunch(threshold int, reason string) (int, bool, error) {
	n := s.ConsecutiveFailures() + 1
	if err := s.SetConsecutiveFailures(n); err != nil {
		return n, false, err
	}
	// The failure is now counted; the pending marker must not be counted again
	// by the launcher on the next boot.
	if err := s.ClearPending(); err != nil {
		return n, false, err
	}
	if n < threshold {
		return n, false, nil
	}
	return n, true, s.TripBreaker(n, reason)
}

// TripBreaker disables automatic custom startup until the user re-enables it.
func (s Store) TripBreaker(n int, reason string) error {
	msg := fmt.Sprintf("FM-STEP custom startup was DISABLED at %s\n"+
		"after %d consecutive failed launches (last: %s).\n\n"+
		"The MPC Sample will start the stock software.\n"+
		"To re-enable FM-STEP: create an empty file named ENABLE in this FMSTEP folder.\n"+
		"Logs are in FMSTEP/logs/.\n", time.Now().UTC().Format(time.RFC3339), n, reason)
	if err := writeAtomic(filepath.Join(s.Root, "DISABLED_BY_BREAKER.txt"), []byte(msg)); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.Root, "ENABLE"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
