package main

// Choosing the 4 instruments (0.9.22): on the MODEL page, DATA push marks
// the model as a keeper. The list lives in FMSTEP/user/keepers.txt and goes
// into app.log (KEEPERS ...) so it comes back with the test logs.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// modelFamily: which of the 4 proposed instruments a model belongs to.
func modelFamily(eng int) string {
	switch eng {
	case 0, 8, 9: // VA + FILTER, ANALOG, WAVESHAPER
		return "ANALOG"
	case 2, 3, 4, 10: // the DX7 banks, 2-OP FM
		return "FM"
	case 6, 14: // STRING MACHINE, CHORDS
		return "CHORDS"
	case 20, 21, 22, 23: // MODAL, BASS DRUM, SNARE, HI-HAT
		return "DRUMS"
	}
	return ""
}

func (a *app) keepPath() string { return filepath.Join(a.user, "keepers.txt") }

func (a *app) loadKeep() {
	if a.keep != nil {
		return
	}
	a.keep = map[int]bool{}
	b, err := os.ReadFile(a.keepPath())
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			if v, err := strconv.Atoi(f[0]); err == nil && v >= 0 && v < len(engineNames) {
				a.keep[v] = true
			}
		}
	}
}

func (a *app) keepList() []string {
	ids := []int{}
	for k, on := range a.keep {
		if on {
			ids = append(ids, k)
		}
	}
	sort.Ints(ids)
	out := []string{}
	for _, k := range ids {
		out = append(out, engineNames[k])
	}
	return out
}

func (a *app) toggleKeep(eng int) {
	if eng < 0 || eng >= len(engineNames) {
		return
	}
	a.loadKeep()
	a.keep[eng] = !a.keep[eng]
	var sb strings.Builder
	sb.WriteString("# FM-STEP: the models Darren is keeping (DATA push on the MODEL page)\n")
	for k := range engineNames {
		if a.keep[k] {
			fmt.Fprintf(&sb, "%d %s\n", k, engineNames[k])
		}
	}
	os.WriteFile(a.keepPath(), []byte(sb.String()), 0644)
	n := len(a.keepList())
	if a.keep[eng] {
		a.say(fmt.Sprintf("KEEP %s  ·  %d KEPT", engineNames[eng], n))
	} else {
		a.say(fmt.Sprintf("%s NOT KEPT  ·  %d KEPT", engineNames[eng], n))
	}
	a.log("KEEPERS %s", strings.Join(a.keepList(), ", "))
}
