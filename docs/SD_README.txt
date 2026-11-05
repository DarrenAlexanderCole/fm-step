FM-STEP for MPC Sample  -  READ BEFORE USE
==========================================
Controls, build and source: the "fm-step" repository on GitHub.
Unofficial. Not affiliated with Akai or inMusic. Use at your own risk.
Supported stock firmware: 1.3.0.12 only. Any other firmware starts stock.
FM-STEP never flashes or modifies the MPC. It runs from RAM until power-off.

INSTALL
  1. Back up your SD card.
  2. Copy the "autorun" and "FMSTEP" folders to the root of the card.
  3. FM-STEP is OFF until you create an empty file named ENABLE
     inside the FMSTEP folder (FMSTEP/ENABLE).

START
  Power on with the card inserted. The MPC asks "Start FM-STEP?".
  Choose Stock (or do nothing) to use the normal MPC software.

EXIT TO STOCK (no power cycle)
  Press SHIFT + STOP, then STOP. FM-STEP stops playback, saves, and the
  normal MPC software comes back in about 10 seconds. If FM-STEP crashes or freezes, the normal
  software is started automatically.

IF THE STOCK SOFTWARE CANNOT BE RESTARTED
  A red screen appears: press any PAD to reboot now, or push the DATA
  encoder to retry. With no input the MPC reboots after 60 seconds.

EMERGENCY RECOVERY (works even if the screen is frozen or black)
  1. Hold the POWER button until the MPC switches off.
  2. Remove the SD card.
  3. Power on. The MPC starts the normal software. Nothing on the MPC
     itself was changed, so no further steps are needed.
  4. To keep the card in without FM-STEP: on a computer, delete
     FMSTEP/ENABLE (or rename the "autorun" folder to "autorun-off").

AUTOMATIC SAFETY STOP
  After 2 failed starts in a row, FM-STEP disables itself and writes
  FMSTEP/DISABLED_BY_BREAKER.txt. Read FMSTEP/logs/, then create
  FMSTEP/ENABLE again to re-enable.

LOGS
  FMSTEP/logs/launcher.log   startup checks
  FMSTEP/logs/recovery.log   supervisor and stock-restore events
  FMSTEP/logs/app.log        FM-STEP application output
