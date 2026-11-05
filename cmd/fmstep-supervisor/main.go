// fmstep-supervisor: runs the FM-STEP application in place of the stock
// MPC Sample software and guarantees a return to stock.
//
// It is started by the SD launcher as a transient systemd unit:
//
//	systemd-run --unit=fmstep-supervisor -p KillMode=control-group \
//	  -p WatchdogSec=10s -p NotifyAccess=main \
//	  -p ExecStopPost="/bin/sh /run/fmstep/bin/restore-stock.sh" ...
//
// If this process crashes or hangs, systemd kills the whole unit (including
// the application) and runs restore-stock.sh, which starts the stock unit.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fmstep/internal/config"
	"fmstep/internal/link"
	"fmstep/internal/logx"
	"fmstep/internal/panel"
	"fmstep/internal/state"
	"fmstep/internal/stock"
)

type kind int

const (
	kExitStock kind = iota
	kReboot
	kFailedLaunch // died/froze before reaching "healthy"
	kRuntimeCrash // died/froze after reaching "healthy"
	kSDRemoved
	kSignal
)

func (k kind) String() string {
	return [...]string{"exit_to_stock", "reboot_requested", "failed_launch", "runtime_crash", "sd_removed", "supervisor_signal"}[k]
}

type result struct {
	kind   kind
	reason string
}

type sup struct {
	cfg          config.Config
	log          *logx.Logger
	st           state.Store
	stock        *stock.Controller
	sigs         chan os.Signal
	before       []string
	stockStopped bool
}

func main() {
	cfgPath := flag.String("config", "/run/fmstep/fmstep.conf", "config file")
	sd := flag.String("sd", "", "SD card root (overrides sd_root)")
	flag.Parse()

	cfg := config.Defaults()
	cfgErr := config.Load(*cfgPath, &cfg)
	if *sd != "" {
		cfg.SDRoot = *sd
	}
	os.MkdirAll(cfg.RunDir, 0755)
	fm := filepath.Join(cfg.SDRoot, "FMSTEP")
	os.MkdirAll(filepath.Join(fm, "logs"), 0755)
	log := logx.New(filepath.Join(fm, "logs", "recovery.log"), filepath.Join(cfg.RunDir, "recovery.log"))
	s := &sup{
		cfg: cfg, log: log, st: state.Store{Root: fm},
		stock: &stock.Controller{Systemctl: cfg.Systemctl, Unit: cfg.StockUnit, Exe: cfg.StockExe, Log: log},
		sigs:  make(chan os.Signal, 4),
	}
	signal.Notify(s.sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go s.watchdog()

	log.Event("SUPERVISOR_START", "pid", os.Getpid(), "sd", cfg.SDRoot, "app", cfg.AppPath)
	if cfgErr != nil {
		log.Event("CONFIG_ERROR", "err", cfgErr)
		s.restoreAndExit("config_error")
	}
	if cfg.AppPath == "" || cfg.SDRoot == "" {
		log.Event("CONFIG_INCOMPLETE")
		s.restoreAndExit("config_incomplete")
	}
	s.st.SetLastResult("running")

	// 0. Let the launcher (and the stock autorun runner waiting on it) finish
	// before the stock app is stopped, as am-mod does.
	if b, err := os.ReadFile(filepath.Join(cfg.RunDir, "launcher.pid")); err == nil {
		var lp int
		fmt.Sscanf(string(b), "%d", &lp)
		for i := 0; lp > 1 && i < 100; i++ {
			if syscall.Kill(lp, 0) != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// 1. Record which services are active, then stop the stock software.
	before, err := s.stock.ActiveServices()
	if err != nil {
		log.Event("SNAPSHOT_FAILED", "err", err)
	}
	s.before = before
	os.WriteFile(filepath.Join(cfg.RunDir, "services.before"), []byte(strings.Join(before, "\n")+"\n"), 0644)
	log.Event("SERVICES_BEFORE", "count", len(before), "list", strings.Join(before, ","))

	if err := s.stock.Stop(cfg.StockStopTimeout); err != nil {
		log.Event("STOCK_STOP_FAILED", "err", err)
		s.st.ClearPending() // nothing was launched; do not count as a failure
		s.restoreAndExit("stock_stop_failed")
	}
	s.stockStopped = true
	log.Event("STOCK_STOPPED")
	s.waitDevicesFree()

	// 2. Run the application with bounded restarts.
	restarts := 0
	var lastCrash time.Time
	for {
		r := s.runApp(restarts)
		log.Event("APP_ENDED", "outcome", r.kind, "reason", r.reason)
		switch r.kind {
		case kExitStock:
			// A deliberate Exit to Stock shows the app worked, even if it came
			// before the "healthy" mark: never count it as a failed launch.
			if s.sdPresent() {
				s.st.ClearPending()
				s.st.SetConsecutiveFailures(0)
			}
			s.restoreAndExit("exit_to_stock")
		case kReboot:
			s.st.SetLastResult("reboot_requested")
			s.reboot("user_requested")
		case kFailedLaunch:
			n, tripped, err := s.st.RecordFailedLaunch(cfg.MaxConsecutiveFailures, r.reason)
			log.Event("FAILED_LAUNCH_RECORDED", "consecutive", n, "breaker_tripped", tripped, "err", err)
			s.restoreAndExit("failed_launch:" + r.reason)
		case kRuntimeCrash:
			n, _ := s.st.IncCrashCount()
			now := time.Now()
			if restarts < cfg.MaxRestarts && (lastCrash.IsZero() || now.Sub(lastCrash) > cfg.RestartWindow) {
				restarts++
				lastCrash = now
				log.Event("APP_RESTART", "attempt", restarts, "max", cfg.MaxRestarts, "crash_count", n)
				s.markPending()
				s.waitDevicesFree()
				continue
			}
			log.Event("RESTART_LIMIT_REACHED", "restarts", restarts, "crash_count", n)
			s.restoreAndExit("runtime_crash:" + r.reason)
		case kSDRemoved:
			s.restoreAndExit("sd_removed")
		case kSignal:
			s.restoreAndExit("supervisor_signal")
		}
	}
}

func (s *sup) markPending() {
	p := filepath.Join(s.st.Root, "state", "launch_pending")
	os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644)
}

// watchdog pings systemd (WatchdogSec) so a hung supervisor is killed and
// ExecStopPost restores stock.
func (s *sup) watchdog() {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	if strings.HasPrefix(sock, "@") {
		sock = "\x00" + sock[1:]
	}
	addr := &net.UnixAddr{Name: sock, Net: "unixgram"}
	for {
		if c, err := net.DialUnix("unixgram", nil, addr); err == nil {
			c.Write([]byte("WATCHDOG=1"))
			c.Close()
		}
		time.Sleep(s.cfg.WatchdogPing)
	}
}

func (s *sup) sdPresent() bool {
	st, err := os.Stat(filepath.Join(s.cfg.SDRoot, "FMSTEP"))
	return err == nil && st.IsDir()
}

func (s *sup) waitDevicesFree() {
	deadline := time.Now().Add(s.cfg.DeviceFreeTimeout)
	var h []string
	for time.Now().Before(deadline) {
		if h = stock.DeviceHolders(); len(h) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.log.Event("DEVICES_STILL_HELD", "holders", strings.Join(h, ","))
}

func (s *sup) runApp(attempt int) result {
	cfg := s.cfg
	r, w, err := os.Pipe()
	if err != nil {
		return result{kFailedLaunch, "pipe: " + err.Error()}
	}
	defer r.Close()

	var out io.Writer = io.Discard
	if f, err := os.OpenFile(filepath.Join(cfg.SDRoot, "FMSTEP", "logs", "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		out = f
	}
	cmd := exec.Command(cfg.AppPath, cfg.AppArgs...)
	cmd.ExtraFiles = []*os.File{w} // becomes fd 3
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("FMSTEP_LINK_FD=%d", link.FD),
		"FMSTEP_SD="+cfg.SDRoot,
		"FMSTEP_DATA="+filepath.Join(cfg.SDRoot, "FMSTEP", "user"),
		fmt.Sprintf("FMSTEP_RESTART=%d", attempt),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		w.Close()
		return result{kFailedLaunch, "start: " + err.Error()}
	}
	w.Close()
	pid := cmd.Process.Pid
	s.log.Event("APP_STARTED", "pid", pid)

	msgs := make(chan byte, 64)
	go func() {
		b := make([]byte, 64)
		for {
			n, err := r.Read(b)
			for _, c := range b[:n] {
				msgs <- c
			}
			if err != nil {
				close(msgs)
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var (
		ready, healthy, exitReq, rebootReq bool
		readyAt, lastBeat, exitAt          time.Time
		killedFor                          string
		exited                             bool
		waitErr                            error
	)
	kill := func(why string) {
		if killedFor != "" || exited {
			return
		}
		killedFor = why
		s.log.Event("APP_KILL", "pid", pid, "why", why)
		syscall.Kill(-pid, syscall.SIGTERM)
		go func() {
			time.Sleep(2 * time.Second)
			syscall.Kill(-pid, syscall.SIGKILL)
		}()
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	sdCheck := time.Now()
	var signalled, sdGone bool

	for !exited {
		select {
		case c, ok := <-msgs:
			if !ok {
				msgs = nil
				continue
			}
			lastBeat = time.Now()
			switch c {
			case link.Ready:
				if !ready {
					ready, readyAt = true, time.Now()
					s.log.Event("APP_READY", "after_ms", time.Since(start).Milliseconds())
				}
			case link.ExitStock:
				if !exitReq {
					exitReq, exitAt = true, time.Now()
					s.log.Event("EXIT_TO_STOCK_REQUESTED")
				}
			case link.Reboot:
				if !rebootReq {
					rebootReq, exitAt = true, time.Now()
					s.log.Event("REBOOT_REQUESTED")
				}
			}
		case waitErr = <-done:
			exited = true
		case sig := <-s.sigs:
			s.log.Event("SUPERVISOR_SIGNAL", "signal", sig)
			signalled = true
			kill("supervisor_signal")
		case now := <-tick.C:
			switch {
			case exitReq || rebootReq:
				if now.Sub(exitAt) > 10*time.Second {
					kill("exit_hung")
				}
			case !ready && now.Sub(start) > cfg.ReadyTimeout:
				kill("ready_timeout")
			case ready && now.Sub(lastBeat) > cfg.HeartbeatTimeout:
				kill("heartbeat_timeout")
			}
			if ready && !healthy && !exitReq && now.Sub(readyAt) >= cfg.HealthyAfter && now.Sub(lastBeat) <= cfg.HeartbeatTimeout {
				healthy = true
				s.st.ClearPending()
				s.st.SetConsecutiveFailures(0)
				s.log.Event("APP_HEALTHY", "launch_counted_as_success", true)
			}
			if now.Sub(sdCheck) >= time.Second {
				sdCheck = now
				if !s.sdPresent() {
					s.log.Event("SD_REMOVED")
					sdGone = true
					kill("sd_removed")
				}
			}
		}
	}
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	syscall.Kill(-pid, syscall.SIGKILL) // reap any leftover children of the app
	reason := killedFor
	if reason == "" {
		reason = fmt.Sprintf("exit_code=%d err=%v", code, waitErr)
	}
	s.log.Event("APP_EXIT", "code", code, "reason", reason, "ready", ready, "healthy", healthy)

	switch {
	case signalled:
		return result{kSignal, reason}
	case sdGone:
		return result{kSDRemoved, reason}
	case rebootReq:
		return result{kReboot, reason}
	case exitReq:
		return result{kExitStock, reason}
	case code == 0 && healthy:
		s.log.Event("UNEXPECTED_CLEAN_EXIT_TREATED_AS_EXIT")
		return result{kExitStock, "clean_exit_without_X"}
	case !healthy:
		return result{kFailedLaunch, reason}
	default:
		return result{kRuntimeCrash, reason}
	}
}

func (s *sup) shuttingDown() bool {
	out, _ := exec.Command(s.cfg.Systemctl, "is-system-running").Output()
	st := strings.TrimSpace(string(out))
	return st == "stopping"
}

// restoreAndExit returns the device to the stock application and exits.
// It never returns.
func (s *sup) restoreAndExit(why string) {
	if s.shuttingDown() {
		s.log.Event("SYSTEM_STOPPING_SKIP_RESTORE", "why", why)
		os.Exit(0)
	}
	for attempt := 1; ; attempt++ {
		if s.stockStopped {
			s.waitDevicesFree()
		}
		pid, err := s.stock.Start(s.cfg.StockStartTimeout, s.cfg.StockStable)
		if err == nil {
			s.log.Event("STOCK_RUNNING", "pid", pid)
			restarted, failed := s.stock.RestoreDependencies(s.before)
			if len(restarted) > 0 {
				s.log.Event("DEPENDENCIES_RESTARTED", "units", strings.Join(restarted, ","), "failed", strings.Join(failed, ","))
				pid, err = s.stock.Verify(s.cfg.StockStartTimeout, s.cfg.StockStable)
			}
		}
		if err == nil {
			s.log.Event("STOCK_RESTORED", "pid", pid, "why", why, "attempt", attempt)
			if s.sdPresent() {
				s.st.SetLastResult(why)
			}
			os.Exit(0)
		}
		s.log.Event("STOCK_RESTORE_FAILED", "attempt", attempt, "err", err)
		if attempt < 2 {
			s.stock.Stop(s.cfg.StockStopTimeout)
			continue
		}
		if s.promptRecovery() {
			attempt = 0 // user chose retry: two more attempts
			continue
		}
		s.reboot("stock_restore_failed")
	}
}

// promptRecovery shows a recovery screen. Returns true to retry, false to reboot.
func (s *sup) promptRecovery() bool {
	deadline := time.Now().Add(s.cfg.RebootPromptTimeout)
	if s.cfg.RecoveryUI != "panel" {
		s.log.Event("RECOVERY_PROMPT_SKIPPED", "ui", s.cfg.RecoveryUI)
		return false
	}
	scr, err := panel.OpenScreen("/dev/dri/card0")
	if err != nil {
		s.log.Event("RECOVERY_SCREEN_FAILED", "err", err)
	} else {
		scr.Fill(0x00200000)
		scr.Text(8, 12, 2, 0x00FFFFFF, "STOCK RESTART FAILED")
		scr.Text(8, 60, 2, 0x00FFFF80, "PAD: REBOOT NOW")
		scr.Text(8, 90, 2, 0x00FFFF80, "PUSH DATA: RETRY")
		scr.Text(8, 140, 1, 0x00C0C0C0, fmt.Sprintf("AUTO REBOOT IN %d SECONDS", int(s.cfg.RebootPromptTimeout.Seconds())))
		scr.Text(8, 160, 1, 0x00C0C0C0, "LOG: FMSTEP/LOGS/RECOVERY.LOG")
		scr.Text(8, 180, 1, 0x00C0C0C0, "IF STUCK: HOLD POWER, REMOVE SD, POWER ON")
		scr.Flush()
		defer scr.Close()
	}
	cs, dev, err := panel.OpenControlSurface(s.cfg.PanelInitSysex)
	if err != nil {
		s.log.Event("RECOVERY_INPUT_FAILED", "err", err)
		time.Sleep(time.Until(deadline))
		return false
	}
	defer cs.Close()
	s.log.Event("RECOVERY_PROMPT", "input", dev)
	switch cs.Wait(deadline) {
	case panel.KeyEncoderPush:
		s.log.Event("RECOVERY_CHOICE", "choice", "retry")
		return true
	case panel.KeyPad:
		s.log.Event("RECOVERY_CHOICE", "choice", "reboot")
	default:
		s.log.Event("RECOVERY_CHOICE", "choice", "timeout_reboot")
	}
	return false
}

func (s *sup) reboot(why string) {
	s.log.Event("REBOOT", "why", why, "cmd", strings.Join(s.cfg.RebootCmd, " "))
	syscall.Sync()
	if len(s.cfg.RebootCmd) > 0 {
		if out, err := exec.Command(s.cfg.RebootCmd[0], s.cfg.RebootCmd[1:]...).CombinedOutput(); err != nil {
			s.log.Event("REBOOT_FAILED", "err", err, "out", string(out))
			os.Exit(3) // unit stops -> ExecStopPost restore-stock.sh tries again
		}
	}
	os.Exit(0)
}
