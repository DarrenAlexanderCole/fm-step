package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Regression: device run 2026-11-03 failed with CONFIG_ERROR because inline
// comments in the shipped fmstep.conf were parsed as part of the value.
func TestShippedConfigParses(t *testing.T) {
	c := Defaults()
	if err := Load(filepath.Join("..", "..", "launcher", "fmstep.conf"), &c); err != nil {
		t.Fatal(err)
	}
	if c.ReadyTimeout != 20*time.Second || c.StockExe != "/usr/bin/MPC Sample" || c.MaxConsecutiveFailures != 2 {
		t.Fatalf("unexpected values: %+v", c)
	}
}

func TestInlineComment(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(p, []byte("heartbeat_timeout = 4s   # note\nstock_exe = /usr/bin/MPC Sample # x\n"), 0644)
	c := Defaults()
	if err := Load(p, &c); err != nil {
		t.Fatal(err)
	}
	if c.HeartbeatTimeout != 4*time.Second || c.StockExe != "/usr/bin/MPC Sample" {
		t.Fatalf("%v %q", c.HeartbeatTimeout, c.StockExe)
	}
}
