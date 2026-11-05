// Package config parses the simple key=value supervisor configuration.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SDRoot    string // e.g. "/media/MPC SAMPLE"
	RunDir    string // e.g. /run/fmstep
	AppPath   string // application binary (copied to RAM by the launcher)
	AppArgs   []string
	StockUnit string
	StockExe  string
	Systemctl string
	RebootCmd []string

	ReadyTimeout           time.Duration // app must send 'R' within this time
	HeartbeatTimeout       time.Duration // max gap between heartbeats once ready
	HealthyAfter           time.Duration // ready + heartbeats this long = launch succeeded
	MaxRestarts            int           // restarts allowed per session after a runtime crash
	RestartWindow          time.Duration // a 2nd crash within this window is never restarted
	MaxConsecutiveFailures int           // circuit breaker threshold

	StockStopTimeout    time.Duration
	StockStartTimeout   time.Duration
	StockStable         time.Duration
	DeviceFreeTimeout   time.Duration
	RebootPromptTimeout time.Duration
	RecoveryUI          string // "panel" (DRM + control surface) or "none"
	PanelInitSysex      bool
	WatchdogPing        time.Duration
}

func Defaults() Config {
	return Config{
		RunDir:    "/run/fmstep",
		StockUnit: "ac50.service",
		StockExe:  "/usr/bin/MPC Sample",
		Systemctl: "/usr/bin/systemctl",
		RebootCmd: []string{"/usr/bin/systemctl", "reboot"},

		ReadyTimeout:           20 * time.Second,
		HeartbeatTimeout:       3 * time.Second,
		HealthyAfter:           10 * time.Second,
		MaxRestarts:            1,
		RestartWindow:          10 * time.Minute,
		MaxConsecutiveFailures: 2,

		StockStopTimeout:    15 * time.Second,
		StockStartTimeout:   30 * time.Second,
		StockStable:         5 * time.Second,
		DeviceFreeTimeout:   5 * time.Second,
		RebootPromptTimeout: 60 * time.Second,
		RecoveryUI:          "panel",
		PanelInitSysex:      true,
		WatchdogPing:        2 * time.Second,
	}
}

func Load(path string, c *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		// Strip inline comments (" # ..."). Values never contain '#'.
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key=value", path, n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if err := c.set(k, v); err != nil {
			return fmt.Errorf("%s:%d: %v", path, n, err)
		}
	}
	return sc.Err()
}

func (c *Config) set(k, v string) error {
	dur := func(d *time.Duration) error {
		x, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if x <= 0 {
			return fmt.Errorf("%s must be > 0", k)
		}
		*d = x
		return nil
	}
	num := func(i *int) error {
		x, err := strconv.Atoi(v)
		if err != nil || x < 0 {
			return fmt.Errorf("%s: bad integer %q", k, v)
		}
		*i = x
		return nil
	}
	switch k {
	case "sd_root":
		c.SDRoot = v
	case "run_dir":
		c.RunDir = v
	case "app_path":
		c.AppPath = v
	case "app_args":
		c.AppArgs = strings.Fields(v)
	case "stock_unit":
		c.StockUnit = v
	case "stock_exe":
		c.StockExe = v
	case "systemctl":
		c.Systemctl = v
	case "reboot_cmd":
		c.RebootCmd = strings.Fields(v)
	case "ready_timeout":
		return dur(&c.ReadyTimeout)
	case "heartbeat_timeout":
		return dur(&c.HeartbeatTimeout)
	case "healthy_after":
		return dur(&c.HealthyAfter)
	case "max_restarts":
		return num(&c.MaxRestarts)
	case "restart_window":
		return dur(&c.RestartWindow)
	case "max_consecutive_failures":
		if err := num(&c.MaxConsecutiveFailures); err != nil {
			return err
		}
		if c.MaxConsecutiveFailures < 1 {
			return fmt.Errorf("max_consecutive_failures must be >= 1")
		}
	case "stock_stop_timeout":
		return dur(&c.StockStopTimeout)
	case "stock_start_timeout":
		return dur(&c.StockStartTimeout)
	case "stock_stable":
		return dur(&c.StockStable)
	case "device_free_timeout":
		return dur(&c.DeviceFreeTimeout)
	case "reboot_prompt_timeout":
		return dur(&c.RebootPromptTimeout)
	case "recovery_ui":
		if v != "panel" && v != "none" {
			return fmt.Errorf("recovery_ui must be panel or none")
		}
		c.RecoveryUI = v
	case "panel_init_sysex":
		c.PanelInitSysex = v == "1" || v == "true"
	case "watchdog_ping":
		return dur(&c.WatchdogPing)
	default:
		return fmt.Errorf("unknown key %q", k)
	}
	return nil
}
