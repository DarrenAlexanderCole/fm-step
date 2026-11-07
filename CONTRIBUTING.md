# Contributing to FM-STEP

Thanks for wanting to help. FM-STEP is a community project for the Akai MPC Sample, and every kind of help counts: playing it and reporting what happens, ideas, sounds and kits, design, docs and code.

## Before anything: keep the MPC safe

FM-STEP never flashes firmware, never writes to the MPC's system partitions and always returns to the stock software. Changes that break any of these rules can't be accepted:

- no flashing, no overwriting stock binaries or partitions, no disabling signature checks
- code runs from RAM; user data lives on the SD card
- stock is the default boot; FM-STEP only runs when the player enables it
- Exit to Stock (SHIFT + STOP) and crash recovery must always work

## Reporting a problem or an idea

Open an issue. For problems, include your firmware version and the three logs from the card (`FMSTEP/logs`: app.log, launcher.log, recovery.log) and what you pressed.

## Sending changes

1. Fork, make a branch, keep the change focused.
2. `make test` must pass (Go tests and the recovery simulation). Engine changes: describe how you tested the sound (an offline render with `fmstep-dsp --render` is great).
3. Anything visual: include a screenshot (the `FMSTEP_MOCK` tests render screens from the real code).
4. Add a line to `CHANGELOG.md`.

### Sign your work (Developer Certificate of Origin)

Add a `Signed-off-by` line to each commit (`git commit -s`). It says you wrote the change, or have the right to submit it, under the project's licence — see https://developercertificate.org.

```
Signed-off-by: Your Name <you@example.com>
```

## Licence of contributions

FM-STEP is licensed under the GNU General Public License v3.0 or later. By contributing you agree your contribution is licensed the same way. Code copied from elsewhere must be compatible with GPL-3.0 and credited in `THIRD_PARTY.md`; code from projects under incompatible licences (or under none) can't be included, but ideas are always welcome.

## Being kind

Be respectful and patient; many people here are musicians first and coders second. That's the point.
