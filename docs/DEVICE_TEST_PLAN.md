# On-device test plan (firmware 1.3.0.12)

Run in order. Record result, date, and attach `FMSTEP/logs/*` for each.
Stop at the first failure. Back up the SD card first.

## A. Assumptions to confirm before anything else

| # | Check | How |
|---|---|---|
| A1 | Firmware 1.3.0.12 | SHIFT + PAD 8 (MIDI Config), top right |
| A2 | Stock boots with no SD | Power on without card |
| A3 | Runner shows our prompt and runs `autorun.sh` as root | Install package, no ENABLE: `launcher.log` shows `STAY_STOCK reason=NOT_ENABLED` |
| A4 | Hashes match | With ENABLE: `launcher.log` shows `FIRMWARE_OK version=1.3.0.12` |
| A5 | `/run` is executable, `systemd-run` with `ExecStopPost` + `WatchdogSec` accepted | `SUPERVISOR_STARTED`, then `SUPERVISOR_START` in `recovery.log` |

## B. Recovery tests (stub app; set mode in `FMSTEP/stub_mode.txt`)

| # | Spec item | stub_mode.txt | Expected |
|---|---|---|---|
| B1 | Normal exit to stock | `ok 30s` | Screen dark ~30 s, stock returns, `STOCK_RESTORED`; pads, audio, USB work in stock |
| B2 | Exit during audio playback | (Phase 3 app) | Pending FM app |
| B3 | Application crash | `crash 30s` | Restart once, crash again → stock; `crash_count=2` |
| B4 | Application freeze | `freeze 30s` | `heartbeat_timeout` ×2 → stock |
| B5 | Supervisor crash | `killsup 30s` (stub SIGKILLs the supervisor) | `RESTORE_SH STOCK_STARTED`, stock returns |
| B6 | Invalid SD card | edit 1 byte of `FMSTEP/bin/fmstep-app` | `HASH_MISMATCH`, stock untouched |
| B7 | Missing files | delete `FMSTEP/bin/fmstep-supervisor` | `MISSING:...`, stock untouched |
| B8 | Repeated startup failures | `crash 2s`, boot twice | ENABLE removed, `DISABLED_BY_BREAKER.txt`; 3rd boot stock |
| B9 | Forced power-off | `run`, hold POWER during the dark startup | Next boot: stock; then `PREVIOUS_LAUNCH_UNFINISHED` counted |
| B10 | Failed stock restart | needs a way to make ac50 fail (TBD on device) | Recovery screen, pad → reboot |
| B11 | SD removed while running | `run`, pull card | Stock returns within ~5 s |
| B12 | Emergency procedure | any | Hold POWER, remove SD, power on → stock |
| B13 | Stock services after return | after B1 | Compare `SERVICES_BEFORE` list with `systemctl list-units` (via SSH) |

Results go into `COMPATIBILITY.md`.
