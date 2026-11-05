# Installing FM-STEP

FM-STEP runs **from the SD card**, next to the stock MPC Sample software. It never flashes or changes the MPC: it is copied into RAM at start-up and is gone after power-off. Default boot is always the stock software.

## You need

- An Akai MPC Sample on stock firmware **1.3.0.12** (see [COMPATIBILITY.md](COMPATIBILITY.md)). On any other firmware the launcher refuses and the stock software starts normally.
- An SD card (FAT32/exFAT) — ideally a spare one, or back yours up first.
- `fmstep-0.9.15-sd.zip` from the Releases page (or your own build, see [BUILD.md](BUILD.md)).

## Steps

1. Check the download: `shasum -a 256 fmstep-0.9.15-sd.zip` must match the `.sha256` file.
2. Unzip. Copy the two folders **`autorun`** and **`FMSTEP`** to the **root** of the SD card (merge with any existing `FMSTEP` folder to keep your projects).
3. **Enable it:** create an empty file named `ENABLE` (no extension) inside `FMSTEP`.
   - macOS: `touch /Volumes/<CARD>/FMSTEP/ENABLE`
   - Windows: in the `FMSTEP` folder, New → Text Document, rename to `ENABLE` with no `.txt`.
4. macOS only: remove the hidden `._` files Finder adds, then eject properly:
   ```sh
   dot_clean /Volumes/<CARD>/autorun
   dot_clean /Volumes/<CARD>/FMSTEP
   sync; diskutil unmount /Volumes/<CARD>
   ```
5. Insert the card into the MPC and power on. The stock software asks whether to run the card's autorun: **Yes** starts FM-STEP; **No** (or waiting) keeps the stock software.

A short guided tour runs the first time. Your projects and sounds are saved on the card (`FMSTEP/user/`), every 30 s and on exit.

## Leaving FM-STEP

- **SHIFT + STOP, then STOP**: saves and returns to the stock software in about 10 s, no power cycle.
- If FM-STEP crashes or freezes, the supervisor brings the stock software back by itself.

## Turning it off / removing it

- Delete `FMSTEP/ENABLE` (FM-STEP stays off, your projects stay), or rename `autorun` to `autorun-off`, or delete both folders.
- Emergency, even with a frozen or black screen: hold **POWER** until off → remove the card → power on. Nothing on the MPC itself was changed.

More: [RECOVERY.md](RECOVERY.md) (all recovery layers and the automatic safety stop) and the logs in `FMSTEP/logs/` (launcher, recovery, app) — please attach them to any issue you report.
