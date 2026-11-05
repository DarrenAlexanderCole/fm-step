# FM-STEP recovery procedure

## Layers (each works if the ones above it fail)

1. **In-app Exit to Stock**: app stops, saves, sends `X`, exits; supervisor restarts `ac50.service`, restarts any service that was active before and is now stopped, and confirms the stock binary is running with a stable PID.
2. **Supervisor**: on crash, freeze (no heartbeat for 3 s), ready timeout, SD removal or SIGTERM it kills the app and restores stock. One automatic restart per session after a runtime crash; never for a launch that has not become healthy.
3. **Recovery screen**: if stock fails to restart twice, the supervisor draws its own screen (DRM) and reads the pads directly: any PAD = reboot now, DATA push = retry, 60 s = reboot.
4. **restore-stock.sh**: if the supervisor itself dies or hangs (systemd watchdog 10 s), systemd kills the unit (including the app) and runs this script, which starts `ac50.service` (3 tries) or reboots.
5. **Power cycle**: nothing persistent was changed, so a reboot always starts stock first. Custom code only runs again if the user says Yes at the prompt and ENABLE still exists and the breaker has not tripped.
6. **Physical (no screen, no supervisor needed)**: hold POWER until off → remove SD → power on → stock.

## Circuit breaker

- `FMSTEP/state/launch_pending` is written before each launch and removed when the app has been ready and heartbeating for 10 s.
- A launch that dies before that, or a pending marker found at the next boot (power cut during startup), increments `consecutive_failures`.
- At 2: `FMSTEP/ENABLE` is deleted and `FMSTEP/DISABLED_BY_BREAKER.txt` written. Only the user re-creating ENABLE re-enables it (counter reset).
- Runtime crashes after a healthy start increment `crash_count` (lifetime) and use the bounded-restart rule instead.

## Logs (on the SD card)

- `FMSTEP/logs/launcher.log` — every launcher decision (`STAY_STOCK reason=...`).
- `FMSTEP/logs/recovery.log` — supervisor and restore-stock.sh events.
- `FMSTEP/logs/app.log` — app stdout/stderr.
- `/run/fmstep/recovery.log` — RAM mirror (survives SD removal until power-off).
