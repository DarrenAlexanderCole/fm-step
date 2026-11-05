// Package stock stops, restarts and verifies the stock MPC Sample service.
//
// Only runtime systemctl operations are used (stop/start/reset-failed).
// Nothing is enabled, disabled, masked or written to persistent storage.
package stock

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fmstep/internal/logx"
)

type Controller struct {
	Systemctl string
	Unit      string
	Exe       string
	Log       *logx.Logger
}

func (c *Controller) run(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Systemctl, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ActiveServices returns the sorted list of active service units.
func (c *Controller) ActiveServices() ([]string, error) {
	out, err := c.run(10*time.Second, "list-units", "--type=service", "--state=active", "--plain", "--no-legend", "--no-pager")
	if err != nil {
		return nil, fmt.Errorf("list-units: %v: %s", err, out)
	}
	var units []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.HasSuffix(f[0], ".service") {
			units = append(units, f[0])
		}
	}
	sort.Strings(units)
	return units, nil
}

func (c *Controller) IsActive(unit string) bool {
	out, _ := c.run(5*time.Second, "is-active", unit)
	return out == "active"
}

func (c *Controller) MainPID() int {
	out, err := c.run(5*time.Second, "show", "-p", "MainPID", "--value", c.Unit)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

func exeOf(pid int) string {
	p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(p, " (deleted)")
}

// StockRunning reports whether the unit's main process is the stock binary.
// launch-ac50 execs the binary, so MainPID's exe is the binary itself once
// startup has passed the (MCU firmware tool) pre-step.
func (c *Controller) StockRunning() (int, bool) {
	if !c.IsActive(c.Unit) {
		return 0, false
	}
	pid := c.MainPID()
	if pid <= 0 {
		return 0, false
	}
	return pid, exeOf(pid) == c.Exe
}

func (c *Controller) Stop(timeout time.Duration) error {
	out, err := c.run(timeout, "stop", c.Unit)
	if err != nil {
		return fmt.Errorf("stop %s: %v: %s", c.Unit, err, out)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !c.IsActive(c.Unit) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s still active after stop", c.Unit)
}

// Start starts the stock unit and verifies that the stock binary is running
// and stays up (same PID) for `stable`.
func (c *Controller) Start(timeout, stable time.Duration) (int, error) {
	c.run(5*time.Second, "reset-failed", c.Unit)
	if out, err := c.run(timeout, "start", c.Unit); err != nil {
		return 0, fmt.Errorf("start %s: %v: %s", c.Unit, err, out)
	}
	return c.Verify(timeout, stable)
}

func (c *Controller) Verify(timeout, stable time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	lastReport := time.Now()
	for time.Now().Before(deadline) {
		pid, ok := c.StockRunning()
		if ok {
			t := time.Now().Add(stable)
			same := true
			for time.Now().Before(t) {
				time.Sleep(250 * time.Millisecond)
				if p2, ok2 := c.StockRunning(); !ok2 || p2 != pid {
					same = false
					if c.Log != nil {
						c.Log.Event("STOCK_PID_CHANGED", "was", pid, "now", p2, "running", ok2)
					}
					break
				}
			}
			if same {
				return pid, nil
			}
		}
		if c.Log != nil && time.Since(lastReport) >= 4*time.Second {
			lastReport = time.Now()
			mp := c.MainPID()
			c.Log.Event("STOCK_NOT_READY_YET", "active", c.IsActive(c.Unit), "main_pid", mp, "exe", exeOf(mp))
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0, fmt.Errorf("stock binary %q not running stably under %s", c.Exe, c.Unit)
}

// RestoreDependencies starts every service that was active before the stock
// stop and is not active now (e.g. a USB gadget service bound to ac50).
func (c *Controller) RestoreDependencies(before []string) (restarted, failed []string) {
	now, err := c.ActiveServices()
	if err != nil {
		c.Log.Event("DEPS_LIST_FAILED", "err", err)
		return nil, nil
	}
	active := map[string]bool{}
	for _, u := range now {
		active[u] = true
	}
	for _, u := range before {
		if active[u] || u == c.Unit || strings.HasPrefix(u, "fmstep") {
			continue
		}
		if out, err := c.run(30*time.Second, "start", u); err != nil {
			c.Log.Event("DEP_RESTART_FAILED", "unit", u, "err", err, "out", out)
			failed = append(failed, u)
		} else {
			restarted = append(restarted, u)
		}
	}
	return
}

// DeviceHolders lists processes (other than ours) holding audio, DRM or
// control-surface device nodes open.
func DeviceHolders(skip ...int) []string {
	skipm := map[int]bool{os.Getpid(): true}
	for _, s := range skip {
		skipm[s] = true
	}
	var holders []string
	procs, _ := filepath.Glob("/proc/[0-9]*")
	for _, p := range procs {
		pid, _ := strconv.Atoi(filepath.Base(p))
		if skipm[pid] {
			continue
		}
		fds, _ := filepath.Glob(p + "/fd/*")
		for _, fd := range fds {
			t, err := os.Readlink(fd)
			if err != nil {
				continue
			}
			// /dev/dri is not checked: a system helper keeps card0 open
			// permanently on the MPC Sample (device runs 2026-11-03), and DRM
			// allows several openers anyway.
			if strings.HasPrefix(t, "/dev/snd/pcm") || strings.HasPrefix(t, "/dev/snd/midi") {
				holders = append(holders, fmt.Sprintf("%d:%s", pid, t))
			}
		}
	}
	return holders
}
