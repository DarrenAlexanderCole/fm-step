// Package logx writes an append-only, fsync'd recovery log.
// It writes to the SD card when available and always mirrors to /run so a
// log survives SD removal for the rest of the boot.
package logx

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu    sync.Mutex
	paths []string
}

func New(paths ...string) *Logger { return &Logger{paths: paths} }

func uptime() string {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "?"
	}
	for i, c := range b {
		if c == ' ' {
			return string(b[:i])
		}
	}
	return "?"
}

// Event logs one line: "<wallclock> up=<uptime> EVENT key=value ...".
func (l *Logger) Event(event string, kv ...any) {
	line := fmt.Sprintf("%s up=%s %s", time.Now().UTC().Format(time.RFC3339), uptime(), event)
	for i := 0; i+1 < len(kv); i += 2 {
		line += fmt.Sprintf(" %v=%q", kv[i], fmt.Sprint(kv[i+1]))
	}
	line += "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(os.Stderr, line)
	for _, p := range l.paths {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			continue
		}
		f.WriteString(line)
		f.Sync()
		f.Close()
	}
}
