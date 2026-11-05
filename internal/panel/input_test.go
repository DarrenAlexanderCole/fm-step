package panel

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

// A real record from the device (lab run, 2026-11-03): pad 1 note on.
const devRecord = "00 03 00 00 CC 78 7A 1A 21 00 00 00 00 00 00 00 99 25 02 00 00 00 00 00 00 00 00 00 00 00 00 00"

func TestDeframe(t *testing.T) {
	b, _ := hex.DecodeString(strings.ReplaceAll(devRecord, " ", ""))
	two := append(append([]byte{}, b...), b...)
	if !isFramed(two) {
		t.Fatal("device record not recognised as framed")
	}
	if isFramed([]byte{0x99, 0x25, 0x02}) {
		t.Fatal("plain MIDI misdetected as framed")
	}
	var out []byte
	for i := 0; i < len(two); i += 32 {
		out = append(out, two[i+16:i+16+int(two[i+1])]...)
	}
	if !bytes.Equal(out, []byte{0x99, 0x25, 0x02, 0x99, 0x25, 0x02}) {
		t.Fatalf("got % X", out)
	}
}

// Regression: Read must return promptly when no MIDI is pending.
func TestReadDoesNotBlockWithoutInput(t *testing.T) {
	r, w, err := os.Pipe() // pollable, like the rawmidi device
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	c := &ControlSurface{f: r}
	buf := make([]byte, 64)
	start := time.Now()
	if got := c.Read(buf); len(got) != 0 {
		t.Fatalf("unexpected data %v", got)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("Read blocked for %v", d)
	}
	w.Write([]byte{0x99, 0x25, 0x40})
	if got := c.Read(buf); len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}
