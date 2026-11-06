#!/usr/bin/env python3
"""Host-side end-to-end tests of launcher -> supervisor -> app -> stock restore.

Uses mock systemctl/systemd-run (tests/mock) and a fake stock binary (a copy
of `sleep` installed as "<root>/usr/bin/MPC Sample"). These tests validate the
LOGIC only. They are not a substitute for the on-device test plan.
"""
import hashlib, os, shutil, signal, subprocess, sys, tempfile, time, json

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.dirname(HERE)
MOCK = os.path.join(HERE, "mock")
BUILD = os.path.join(SRC, "build", "host")

results = []

def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest()

class Env:
    def __init__(self):
        self.tmp = tempfile.mkdtemp(prefix="fmstep-test-")
        self.root = os.path.join(self.tmp, "root")
        self.mock = os.path.join(self.tmp, "mockstate")
        self.sd = os.path.join(self.tmp, "media", "MPC SAMPLE")
        os.makedirs(os.path.join(self.root, "usr/bin"))
        os.makedirs(os.path.join(self.root, "usr/lib/systemd/system"))
        os.makedirs(os.path.join(self.root, "run"))
        os.makedirs(self.mock)
        self.stock = os.path.join(self.root, "usr/bin/MPC Sample")
        shutil.copy(shutil.which("sleep"), self.stock)
        open(os.path.join(self.root, "usr/lib/systemd/system/ac50.service"), "w").write("[Service]\nExecStart=/usr/bin/launch-ac50\nRestart=always\n")
        self.compat = os.path.join(self.tmp, "compat.txt")
        open(self.compat, "w").write("1.3.0.12-test %s %s\n" % (sha(self.stock), sha(os.path.join(self.root, "usr/lib/systemd/system/ac50.service"))))
        self.env = dict(os.environ, PATH=MOCK + ":" + os.environ["PATH"], MOCK_STATE=self.mock, FMSTEP_TEST_ROOT=self.root)
        self.package()

    def package(self, compat=None):
        subprocess.check_call([sys.executable, os.path.join(SRC, "tools/package.py"), "--version", "test",
                               "--supervisor", os.path.join(BUILD, "fmstep-supervisor"),
                               "--app", os.path.join(BUILD, "fmstep-stub"),
                               "--out", self.sd, "--compat", compat or self.compat, "--enable"],
                              stdout=subprocess.DEVNULL)
        self.fm = os.path.join(self.sd, "FMSTEP")
        shipped = open(os.path.join(SRC, "launcher", "fmstep.conf")).read()
        open(os.path.join(self.fm, "fmstep.conf"), "w").write(shipped + f"""
stock_unit = ac50.service
stock_exe = {self.stock}
systemctl = {MOCK}/systemctl
reboot_cmd = {MOCK}/reboot-mock
ready_timeout = 3s
heartbeat_timeout = 1s
healthy_after = 2s
max_restarts = 1
restart_window = 10m
max_consecutive_failures = 2
stock_stop_timeout = 3s
stock_start_timeout = 3s
stock_stable = 1s
device_free_timeout = 200ms
recovery_ui = none
reboot_prompt_timeout = 1s
""")

    def sysctl(self, *a):
        return subprocess.run([os.path.join(MOCK, "systemctl"), *a], env=self.env, capture_output=True, text=True)

    def killall(self):
        subprocess.run(["pkill", "-9", "-f", self.tmp], stderr=subprocess.DEVNULL)
        time.sleep(0.2)

    def boot(self, keep_run=False):
        """Simulate a power cycle: kill everything, clear tmpfs, start stock."""
        self.killall()
        if not keep_run:
            shutil.rmtree(os.path.join(self.root, "run", "fmstep"), ignore_errors=True)
        for f in os.listdir(self.mock):
            os.remove(os.path.join(self.mock, f))
        self.sysctl("start", "ac50.service")
        assert self.stock_ok(), "stock did not start at boot"

    def stub(self, mode, dur="0"):
        open(os.path.join(self.fm, "stub_mode.txt"), "w").write(f"{mode} {dur}\n")

    def runner_check(self):
        ar = os.path.join(self.sd, "autorun")
        data = open(os.path.join(ar, "autorun.json"), "rb").read() + open(os.path.join(ar, "autorun.sh"), "rb").read() + b"f7bed1c9-7e90-4731-9194-c996666b4331"
        return hashlib.sha256(data).hexdigest() == open(os.path.join(ar, "autorun.check")).read()

    def launch(self):
        assert self.runner_check(), "autorun.check mismatch (firmware runner would refuse)"
        return subprocess.run(["sh", os.path.join(self.sd, "autorun", "autorun.sh")], env=self.env, capture_output=True, text=True, timeout=60)

    def wait_done(self, timeout=40):
        p = os.path.join(self.mock, "fmstep-supervisor.done")
        t = time.time() + timeout
        while time.time() < t:
            if os.path.exists(p):
                return int(open(p).read())
            time.sleep(0.1)
        raise AssertionError("supervisor unit did not finish")

    def supervisor_started(self):
        return os.path.exists(os.path.join(self.mock, "fmstep-supervisor.pid"))

    def stock_ok(self):
        st = json.load(open(os.path.join(self.mock, "units.json")))
        pid = st.get("ac50.service", 0)
        try:
            return os.readlink(f"/proc/{pid}/exe") == self.stock
        except OSError:
            return False

    def unit_active(self, u):
        return self.sysctl("is-active", u).returncode == 0

    def stat(self, name):
        try:
            return open(os.path.join(self.fm, "state", name)).read().strip()
        except OSError:
            return None

    def log(self, name="recovery.log"):
        try:
            return open(os.path.join(self.fm, "logs", name)).read()
        except OSError:
            return ""

    def reset_state(self):
        shutil.rmtree(os.path.join(self.fm, "state"), ignore_errors=True)
        os.makedirs(os.path.join(self.fm, "state"))
        for f in ("DISABLED_BY_BREAKER.txt",):
            p = os.path.join(self.fm, f)
            if os.path.exists(p): os.remove(p)
        open(os.path.join(self.fm, "ENABLE"), "w").close()
        for f in os.listdir(os.path.join(self.fm, "logs")):
            os.remove(os.path.join(self.fm, "logs", f))

def test(name):
    def deco(fn):
        def run(e):
            e.boot(); e.reset_state()
            t0 = time.time()
            try:
                fn(e)
                results.append((name, "PASS", f"{time.time()-t0:.1f}s"))
            except AssertionError as ex:
                results.append((name, "FAIL", str(ex)))
            except Exception as ex:
                results.append((name, "ERROR", repr(ex)))
        run.__name__ = name
        return run
    return deco

def check(cond, msg):
    if not cond:
        raise AssertionError(msg)

# ---------------------------------------------------------------- scenarios
@test("stock-only when ENABLE is absent")
def t_not_enabled(e):
    os.remove(os.path.join(e.fm, "ENABLE"))
    r = e.launch()
    check(r.returncode == 0, "launcher exit")
    check("STAY_STOCK reason=NOT_ENABLED" in e.log("launcher.log"), "no NOT_ENABLED log")
    check(not e.supervisor_started(), "supervisor started anyway")
    check(e.stock_ok(), "stock not running")

@test("normal Exit to Stock")
def t_exit(e):
    e.stub("ok", "3s")
    e.launch()
    check(e.wait_done() == 0, "supervisor exit code")
    lg = e.log()
    for ev in ("STOCK_STOPPED", "APP_READY", "APP_HEALTHY", "EXIT_TO_STOCK_REQUESTED", "STOCK_RESTORED"):
        check(ev in lg, f"missing {ev}")
    check(e.stock_ok(), "stock not restored")
    check(e.unit_active("usb-gadget.service"), "dependency usb-gadget not restored")
    check("DEPENDENCIES_RESTARTED" in lg, "dependency restart not logged")
    check(e.stat("launch_pending") is None, "launch_pending left behind")
    check(e.stat("consecutive_failures") == "0", "failures not reset")
    check(e.stat("last_result") == "exit_to_stock", "last_result")
    check("RESTORE_SH STOCK_ALREADY_ACTIVE" in lg, "ExecStopPost did not run/confirm")

@test("quick Exit to Stock before healthy is not a failed launch")
def t_quick_exit(e):
    open(os.path.join(e.fm, "state", "consecutive_failures"), "w").write("1\n")
    e.stub("ok", "500ms")
    e.launch()
    check(e.wait_done() == 0, "supervisor exit code")
    lg = e.log()
    check("EXIT_TO_STOCK_REQUESTED" in lg and "APP_HEALTHY" not in lg, "should exit before healthy")
    check(e.stock_ok(), "stock not restored")
    check(e.stat("launch_pending") is None, "launch_pending left behind (would count as a failure next boot)")
    check(e.stat("consecutive_failures") == "0", f"failures={e.stat('consecutive_failures')}")

@test("crash before healthy = failed launch, no restart")
def t_crash_early(e):
    e.stub("crash", "500ms")
    e.launch(); e.wait_done()
    lg = e.log()
    check("APP_RESTART" not in lg, "restarted a failing launch")
    check(e.stat("consecutive_failures") == "1", f"failures={e.stat('consecutive_failures')}")
    check(e.stock_ok(), "stock not restored")
    check(os.path.exists(os.path.join(e.fm, "ENABLE")), "breaker tripped too early")

@test("app fails to start (exit 1)")
def t_failstart(e):
    e.stub("failstart")
    e.launch(); e.wait_done()
    check(e.stat("consecutive_failures") == "1", "not counted")
    check(e.stock_ok(), "stock not restored")

@test("app never reports ready (ready timeout)")
def t_nostart(e):
    e.stub("nostart")
    e.launch(); e.wait_done()
    check("why=\"ready_timeout\"" in e.log(), "no ready_timeout kill")
    check(e.stat("consecutive_failures") == "1", "not counted")
    check(e.stock_ok(), "stock not restored")

@test("two consecutive failed launches trip the circuit breaker")
def t_breaker(e):
    e.stub("crash", "500ms")
    e.launch(); e.wait_done()
    e.boot()
    e.launch(); e.wait_done()
    check(e.stat("consecutive_failures") == "2", "count")
    check(not os.path.exists(os.path.join(e.fm, "ENABLE")), "ENABLE not removed")
    check(os.path.exists(os.path.join(e.fm, "DISABLED_BY_BREAKER.txt")), "no DISABLED file")
    check(e.stock_ok(), "stock not restored")
    e.boot()
    e.launch()
    check("STAY_STOCK reason=NOT_ENABLED" in e.log("launcher.log"), "launched while disabled")
    check(not e.supervisor_started(), "supervisor ran while disabled")
    # user explicitly re-enables
    open(os.path.join(e.fm, "ENABLE"), "w").close()
    e.stub("ok", "3s")
    e.boot()
    e.launch(); e.wait_done()
    check("RE_ENABLED_BY_USER" in e.log("launcher.log"), "re-enable not logged")
    check(e.stat("consecutive_failures") == "0", "count not reset after success")
    check(not os.path.exists(os.path.join(e.fm, "DISABLED_BY_BREAKER.txt")), "DISABLED file left")

@test("runtime crash: one bounded restart, second crash -> stock")
def t_runtime_crash(e):
    e.stub("crash", "3s")
    e.launch(); e.wait_done(60)
    lg = e.log()
    check(lg.count("APP_STARTED") == 2, f"expected 2 starts, got {lg.count('APP_STARTED')}")
    check("RESTART_LIMIT_REACHED" in lg, "no restart limit")
    check(e.stat("crash_count") == "2", f"crash_count={e.stat('crash_count')}")
    check(e.stat("consecutive_failures") == "0", "runtime crash counted as failed launch")
    check(e.stock_ok(), "stock not restored")

@test("freeze after healthy: heartbeat timeout, restart, then stock")
def t_freeze(e):
    e.stub("freeze", "3s")
    e.launch(); e.wait_done(60)
    lg = e.log()
    check(lg.count("why=\"heartbeat_timeout\"") == 2, "heartbeat kills")
    check(e.stock_ok(), "stock not restored")

@test("freeze before healthy = failed launch")
def t_freeze_early(e):
    e.stub("freeze", "200ms")
    e.launch(); e.wait_done()
    check(e.stat("consecutive_failures") == "1", "not counted")
    check(e.stock_ok(), "stock not restored")

@test("supervisor crash (SIGKILL) -> ExecStopPost restores stock, app dies")
def t_supervisor_crash(e):
    e.stub("run")
    e.launch()
    t = time.time() + 10
    while time.time() < t and "APP_HEALTHY" not in e.log():
        time.sleep(0.1)
    sup = int(open(os.path.join(e.mock, "fmstep-supervisor.pid")).read())
    os.kill(sup, signal.SIGKILL)
    e.wait_done()
    time.sleep(0.5)
    lg = e.log()
    check("RESTORE_SH STOCK_STARTED" in lg, "restore-stock.sh did not start stock")
    check(e.stock_ok(), "stock not restored")
    alive = subprocess.run(["pgrep", "-f", os.path.join(e.root, "run/fmstep/bin/fmstep-app")], capture_output=True).stdout.strip()
    check(not alive, "app survived supervisor crash")

@test("supervisor killed by the app (device test B5 path)")
def t_killsup(e):
    e.stub("killsup", "3s")
    e.launch(); e.wait_done()
    time.sleep(0.5)
    check("RESTORE_SH STOCK_STARTED" in e.log(), "restore-stock.sh did not start stock")
    check(e.stock_ok(), "stock not restored")

@test("supervisor stopped by signal (systemctl stop) -> stock")
def t_supervisor_term(e):
    e.stub("run")
    e.launch()
    time.sleep(2)
    sup = int(open(os.path.join(e.mock, "fmstep-supervisor.pid")).read())
    os.kill(sup, signal.SIGTERM)
    e.wait_done()
    check("STOCK_RESTORED" in e.log(), "not restored by supervisor")
    check(e.stock_ok(), "stock not restored")

@test("unsupported firmware -> stock")
def t_bad_fw(e):
    bad = os.path.join(e.tmp, "badcompat.txt")
    open(bad, "w").write("9.9 " + "0" * 64 + " " + "0" * 64 + "\n")
    e.package(bad); e.reset_state()
    try:
        e.launch()
        check("UNSUPPORTED_FIRMWARE" in e.log("launcher.log"), "not refused")
        check(not e.supervisor_started(), "supervisor ran")
        check(e.stock_ok(), "stock")
    finally:
        e.package()

@test("firmware check is remembered on the next launch")
def t_fw_cache(e):
    e.stub("ok", "500ms")
    e.launch(); e.wait_done()
    check("FIRMWARE_OK version=1.3.0.12-test cached=0" in e.log("launcher.log"), "first launch should hash")
    check(e.stat("firmware.cache") is not None, "no firmware.cache written")
    e.boot()
    e.launch(); e.wait_done()
    check("cached=1" in e.log("launcher.log"), "second launch did not use the cache")
    check(open(os.path.join(e.root, "run/fmstep/launch_uptime")).read().strip() != "", "no launch_uptime")
    check(e.stock_ok(), "stock")

@test("changed stock program forces a full check -> stock")
def t_fw_cache_changed(e):
    e.stub("ok", "500ms")
    e.launch(); e.wait_done()
    check(e.stat("firmware.cache") is not None, "no firmware.cache written")
    # a "firmware update": the stock program changes (different size)
    e.killall()
    with open(e.stock, "ab") as f:
        f.write(b"\0" * 16)
    try:
        e.boot()
        e.launch()
        lg = e.log("launcher.log")
        check("UNSUPPORTED_FIRMWARE" in lg, "changed program was not re-checked")
        check(not e.supervisor_started(), "supervisor ran")
        check(e.stat("firmware.cache") is None, "stale cache kept")
    finally:
        e.killall()
        with open(e.stock, "r+b") as f:
            f.truncate(os.path.getsize(e.stock) - 16)
        e.boot()

@test("tampered payload -> stock")
def t_tampered(e):
    with open(os.path.join(e.fm, "bin", "fmstep-app"), "ab") as f:
        f.write(b"x")
    try:
        e.launch()
        check("HASH_MISMATCH:bin/fmstep-app" in e.log("launcher.log"), "tamper not detected")
        check(not e.supervisor_started(), "supervisor ran")
        check(e.stock_ok(), "stock")
    finally:
        e.package()

@test("missing application file -> stock")
def t_missing(e):
    os.remove(os.path.join(e.fm, "bin", "fmstep-supervisor"))
    try:
        e.launch()
        check("MISSING:bin/fmstep-supervisor" in e.log("launcher.log"), "missing not detected")
        check(not e.supervisor_started(), "supervisor ran")
    finally:
        e.package()

@test("edited autorun.sh is refused by firmware check")
def t_check(e):
    p = os.path.join(e.sd, "autorun", "autorun.sh")
    with open(p, "a") as f: f.write("\n# edit\n")
    try:
        check(not e.runner_check(), "check still matches after edit")
    finally:
        e.package()

@test("forced power-off during startup counts as failure")
def t_powercut_startup(e):
    open(os.path.join(e.fm, "state", "launch_pending"), "w").write("x\n")
    e.stub("ok", "3s")
    e.launch(); e.wait_done()
    check("PREVIOUS_LAUNCH_UNFINISHED consecutive_failures=1" in e.log("launcher.log"), "pending not counted")
    check(e.stat("consecutive_failures") == "0", "successful launch should reset")

@test("forced power-off after healthy is not a failure")
def t_powercut_healthy(e):
    e.stub("run")
    e.launch()
    t = time.time() + 10
    while time.time() < t and "APP_HEALTHY" not in e.log():
        time.sleep(0.1)
    e.boot()  # power cut: everything killed, /run cleared
    check(e.stock_ok(), "stock after power cycle")
    e.stub("ok", "3s")
    e.launch(); e.wait_done()
    check("PREVIOUS_LAUNCH_UNFINISHED" not in e.log("launcher.log"), "healthy session counted as failure")

@test("one launch attempt per boot")
def t_once(e):
    e.stub("ok", "2s")
    e.launch(); e.wait_done()
    os.remove(os.path.join(e.mock, "fmstep-supervisor.done"))
    e.launch()
    check("ALREADY_ATTEMPTED_THIS_BOOT" in e.log("launcher.log"), "second launch allowed")

@test("stock restart fails once, retry succeeds")
def t_restore_retry(e):
    e.stub("ok", "3s")
    e.launch()
    time.sleep(1.5)
    open(os.path.join(e.mock, "fail_start"), "w").write("1")
    e.wait_done()
    lg = e.log()
    check("STOCK_RESTORE_FAILED" in lg and "attempt=\"2\"" in lg, "no retry")
    check(e.stock_ok(), "stock not restored")
    check(not os.path.exists(os.path.join(e.mock, "rebooted")), "rebooted unnecessarily")

@test("stock restart keeps failing -> controlled reboot")
def t_restore_reboot(e):
    e.stub("ok", "3s")
    e.launch()
    time.sleep(1.5)
    open(os.path.join(e.mock, "fail_start"), "w").write("99")
    e.wait_done(90)
    check("REBOOT why=\"stock_restore_failed\"" in e.log().replace("REBOOT why", "REBOOT why"), "no reboot event")
    check(os.path.exists(os.path.join(e.mock, "rebooted")), "reboot command not run")

@test("SD card removed while running -> stock")
def t_sd_removed(e):
    e.stub("run")
    e.launch()
    time.sleep(2.5)
    moved = e.sd + ".removed"
    os.rename(e.sd, moved)
    try:
        e.wait_done()
        runlog = open(os.path.join(e.root, "run/fmstep/recovery.log")).read()
        check("SD_REMOVED" in runlog and "STOCK_RESTORED" in runlog, "not restored after SD removal")
        check(e.stock_ok(), "stock")
    finally:
        os.rename(moved, e.sd)

@test("app requests controlled reboot")
def t_app_reboot(e):
    e.stub("reboot", "3s")
    e.launch(); e.wait_done()
    check(os.path.exists(os.path.join(e.mock, "rebooted")), "no reboot")

@test("invalid config -> stock")
def t_bad_conf(e):
    open(os.path.join(e.fm, "fmstep.conf"), "a").write("bogus_key = 1\n")
    e.stub("ok", "3s")
    try:
        e.launch(); e.wait_done()
        lg = e.log()
        check("CONFIG_ERROR" in lg, "config error not logged")
        check("STOCK_STOPPED" not in lg, "stock stopped despite bad config")
        check(e.stock_ok(), "stock")
    finally:
        e.package()

def main():
    if os.geteuid() != 0:
        print("tests must run as root (launcher requires uid 0)"); sys.exit(2)
    e = Env()
    tests = [v for k, v in globals().items() if k.startswith("t_")]
    only = sys.argv[1:]
    try:
        for t in tests:
            if only and not any(o in t.__name__ for o in only):
                continue
            t(e)
            r = results[-1]
            print(f"{r[1]:5} {r[0]}  ({r[2]})", flush=True)
    finally:
        e.killall()
    bad = [r for r in results if r[1] != "PASS"]
    print(f"\n{len(results) - len(bad)}/{len(results)} passed")
    sys.exit(1 if bad else 0)

if __name__ == "__main__":
    main()
