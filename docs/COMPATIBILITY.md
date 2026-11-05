# Compatibility

| Firmware | `/usr/bin/MPC Sample` SHA-256 | `ac50.service` SHA-256 | Launcher | Tested on hardware |
|---|---|---|---|---|
| **1.3.0.12** | b6584cba76e416e432ad202509099d34373dc2c3d6a8fb625f5678bf0193b508 | 6b2dc52826263daf19c6f1a945c94bd7ba69b15eb7843df68eebdd63ea311e5d | starts FM-STEP | **yes** — one MPC Sample, 3–5 Nov 2026, every build 0.1 → 0.9.14 |
| any other | — | — | refuses, stock starts | — |

Stock hashes from am-mod (MIT); confirmed on the test unit (launcher log `FIRMWARE_OK version=1.3.0.12`).

## Verified on hardware

- Recovery: normal exit, app crash, app freeze, supervisor crash, SD card pulled, power cut during start-up, circuit breaker, cold boot without a card — the stock software returned every time.
- Exit to stock during playback; forced power-off mid-jam with the project intact.
- Audio: sessions of up to 34 min with **0 dropouts**; worst audio block 2.8 ms of the 5 ms budget.
- USB-C audio to a computer: works (44.1 kHz, clock-tracked); recording level is still too hot — being fixed.

If you run FM-STEP on another unit, please open an issue with your firmware version and the three logs from `FMSTEP/logs/`.
