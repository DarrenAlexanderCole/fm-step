package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBreaker(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "state"), 0755)
	os.WriteFile(filepath.Join(root, "ENABLE"), nil, 0644)
	os.WriteFile(filepath.Join(root, "state", "launch_pending"), []byte("x"), 0644)
	s := Store{Root: root}
	if n, tripped, err := s.RecordFailedLaunch(2, "crash"); n != 1 || tripped || err != nil {
		t.Fatalf("first: %d %v %v", n, tripped, err)
	}
	if s.Pending() {
		t.Fatal("pending must be cleared once the failure is counted")
	}
	if n, tripped, err := s.RecordFailedLaunch(2, "crash"); n != 2 || !tripped || err != nil {
		t.Fatalf("second: %d %v %v", n, tripped, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ENABLE")); !os.IsNotExist(err) {
		t.Fatal("ENABLE should be removed when the breaker trips")
	}
	if _, err := os.Stat(filepath.Join(root, "DISABLED_BY_BREAKER.txt")); err != nil {
		t.Fatal("DISABLED_BY_BREAKER.txt missing")
	}
}
