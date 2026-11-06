# Changelog

Dates and times are UTC. The detailed log with measurements and pictures is on the press page (`docs/press`).

## 0.9.22 — MIXTAPE, mixer level dots, the TAPE view removed (6 Nov 2026)
- MIXTAPE: tap SAMPLE RECORD to record the jam to the card (FMSTEP/MIXTAPE/take NNN.wav, 16-bit stereo 48 kHz, up to 20 minutes or what fits). The audio engine only copies the mix into a 21-second ring; a separate writer thread does all the card work, so a slow card can't cause a dropout. The WAV header is rewritten every second, so a crash still leaves a good take.
- The cassette view: reels that move the tape from one side to the other as the take grows, a dot-matrix counter, time left, card space and level. On stop: DELETE (twice) · LISTEN · SAVE on the screen buttons. A red counter stays in the top bar on every view while recording.
- LISTEN streams a take from the card (DATA jumps 5 s).
- The rolling TAPE view and JAM GRAB are gone. PAD FX now opens the LIBRARY: MIXTAPE takes (newest first), then FMSTEP/samples. For a take, K1 picks where the 16 seconds to chop start (the take's overview shows the window) and the middle button listens; PAD FX loads that stretch straight from the file (LOADAT).
- Mixer: each track has a column of level dots that rise and fall with what it plays (orange near the top), with a white peak dot that hangs for a second. The fader is a slim bar beside them.
- Choosing the 4 instruments: on the MODEL page each model shows which of the proposed four it would belong to (ANALOG, FM, CHORDS, DRUMS, or not in the 4); DATA push marks it KEEP (a lime ring on the model grid). The list is saved in FMSTEP/user/keepers.txt and written to app.log.
- The salmon colour is gone; lilac takes its place.
- Engine tests (offline, arm64 emulation): a 58 s take matches the output sample for sample (correlation 0.99999995, level exact); LISTEN plays it back exactly from any point and reports the end; LOADAT loads 16 s from 20.5 s into a take.

## 0.9.21 — the sampler: sequencing, BEATS, MANUAL, your own samples (5 Nov 2026)
- Into the sequence: a chop track (T8 to start, DATA in CHOP: OFF / T1–T8). Its notes play slices (note 36 = pad 1); SEQ REC + PLAY records pad hits onto it; MOTION and DRIFT apply to sequenced hits too.
- CHOP modes: 16 · 8 · BEATS (cut on the hits the engine finds) · MAN (move each slice's marker in SLICE with K1; starts from the slices you had).
- Your own samples: WAVs in FMSTEP/samples (any rate, mono/stereo, 16/24/32-bit, up to 16 s). A file is a loop to chop; a folder is a kit (one sound per pad). SHIFT + PAD FX switches JAM / CARD; DATA picks; PAD FX loads.
- SHIFT + PAD FX in CHOP saves the chop to the card as "chop 001.wav".
- MOTION: ONE SHOT · GATE · LOOP (repeats while held), replacing TAPE.
- Fixes from the 0.9.20 test: bar lines now reset on every PLAY (a 4-bar grab after a stop/start came out 16 s); a silent tape is never grabbed (GRABEMPTY), so a double PAD FX can't wipe your chops; pads with no slice stay silent.
- Engine tests: grab after restart the right length; WAV conversion (44.1k mono 24-bit → 48k stereo); onsets found within 5 ms; kit boundaries; SAVE; sequenced slices on the chop track; LOOP; recording a slice and hearing it on the next loop pass.

## 0.9.20 — FLUX part 2: the sampler, first build (5 Nov 2026)
- Rolling tape: the last 24 s of what you hear (after the effects, before MAIN VOLUME), always recording; bar lines are remembered while the sequencer plays.
- GRAB (PAD FX in TAPE): the last 1, 2 or 4 bars, cut exactly on the bar lines; pressed in the second half of a bar it waits for that bar to finish. The copy runs off the audio thread.
- CHOP: 16 slices (or 8 + 8 reversed) on the pads, 6 slice voices, ONE SHOT / GATE / TAPE motion, DRIFT (late, loose hits), pad pressure drags the slice (slower, tape wow), fader scrub, NOTE REPEAT + fader spins the reel with the low end.
- SLICE: start / end trims, pitch ±12, reverse, with a detailed zoom from the engine (GZ).
- TAPE / CHOP / SLICE screens are live, from engine level data (lows magenta, the rest lime).
- Engine test (offline, under arm64 emulation): slices played in order rebuild the grab (0.96 correlation, cut within 1 ms of the bar line), level within 0.8 dB, no added clicks.
- The PAD FX design preview is replaced by the real views; GRID and ROLL designs stay in the tests until the grid editor is built.

## 0.9.19 — FLUX design preview on the MPC (5 Nov 2026)
- PAD FX steps through the five FLUX part 2 and grid editor design screens (TAPE, CHOP, SLICE, GRID, ROLL), drawn in the style of Darren's references, so spacing can be judged on the real screen. Any other button goes back. GRID and the playheads follow the real pattern and transport; nothing records or edits yet.
- Spacing pass on all five screens.

## 0.9.18 — symmetrical DOTS, approve-once installer (5 Nov 2026)
- DOTS: columns grow up and down from the middle line by the same amount, so the matrix bounces symmetrically.
- Install FM-STEP.app: approve it once and keep it. It installs the newest build it finds: the one inside it, or a newer FM-STEP-<version>-card.zip (or unzipped folder) in Downloads. Card zips are plain files, so macOS doesn't block them. `tools/mac_app.py --card-zip DIR` writes the card zip.

## 0.9.17 — DOTS, quieter GLOBAL, visible intro, installer app (5 Nov 2026)
- DOTS: a third visualizer face (16 LEVELS: spectrum → FLOW → DOTS → pages). The mix as a 36 × 16 dot matrix, low to high, columns rising and falling with the level in the knob colours by frequency zone, with peak-hold dots.
- GLOBAL: no chip over the big BPM while turning a knob, and no repeated big value; a dot slides along the bottom instead.
- Intro: the first 0.9.16 test showed a 40 ms budget on the first launch (the full firmware check used the 5 s), so the moment was invisible. It now always plays at least 1.2 s (1.8 s when the launch is on time). INTRO_DONE logs draw and flush times.
- Install FM-STEP.app (tools/mac_app.py, icon in tools/icon): double-click to save the logs, clear the card, install and eject, with dialogs instead of Terminal.

## 0.9.16 — startup moment (5 Nov 2026, untested on hardware)
- Startup moment: the screen opens first and an animation plays while the sound engine, controls and session load: the track colours land on the loop line, the knob-colour hills rise, FM-STEP lights up dot by dot, and a playhead plays the loop across the screen and the 16 pads. It lasts at least 1.8 s and ends as soon as loading is done.
- Lighter type: small, label, body, value and title text back to Inter Medium (page names stay bold).
- Faster launch: the firmware check remembers a passed result (FMSTEP/state/firmware.cache) and only hashes the stock program again when its size, times or inode change. Saves ~2.4 s per launch (measured 2.44 s on 1.3.0.12). `firmware_check = full` in fmstep.conf turns it off. Two new recovery tests (27/27 pass).
- 5-second budget: the launcher records when it started; the startup moment shortens (and speeds up, to 2.5x) so FM-STEP is playable within 5 s of the launcher starting.
- `tools/card.sh` (or `make card`) for the hardware test loop on a Mac: build, save the card's logs to `test-logs/` with a timing summary, clear logs and launch state, copy the build, eject. Projects in `FMSTEP/user` are never touched.
- Startup timings in app.log: `STARTUP screen_ms dsp_ms surface_ms session_ms playable_ms` and `uptime`, to line up with the launcher and supervisor logs.

## 0.9.15 — first public beta (Nov 2026)
- Heavier small type for readability; graph flags sit below the graph labels.
- Public repo: MIT licence, install / controls / build / compatibility docs, reproducible `make release` (DSP from pinned upstream sources), interface GIFs.

## 0.9.13 — 5 Nov 16:46
- EQ: SHIFT + K1 / K3 move the low (40–400 Hz) and high (2–16 kHz) shelf frequencies, like the mid. FLOW shows the page and knobs.

## 0.9.12 — 5 Nov 14:32
- Screen buttons: left page down, right page up, middle flips to the view's other face. LOOP: hold = loop length (no flash), SHIFT + LOOP = patterns, ERASE + LOOP = clear chain.

## 0.9.11 — 5 Nov 14:07
- The knob flag on every page and visualizer; FLOW watercolour layer.

## 0.9.10 — 5 Nov 13:48
- FLOW visualizer (16 LEVELS twice).

## 0.9.9 — 5 Nov 13:39
- Loop line on every screen; all-tracks notes view next to the mixer; SHIFT + ERASE erase-all with undo.

## 0.9.8 — 5 Nov 09:47
- Mixer knobs; a different dot motion per page.

## 0.9.7 — 5 Nov 09:22
- EQ shows the track's own spectrum; the low kill also removes the sub; visualizer knob flags; ambient dot fields.

## 0.9.6 — 5 Nov 08:48
- Live spectrum (DSP FFT, 48 bands): behind the EQ, and the SPECTRUM visualizer.

## 0.9.1 – 0.9.5 — 5 Nov 07:40 – 08:36
- Partial screen updates (54 ms → a few ms), big page names, knob colours and watercolour washes, every view redesigned.

## 0.9 — 5 Nov ≈07:00
- New interface: drawing engine, live graphics on every page, dot-grid step view.

## 0.7.1 – 0.8.6 — 5 Nov (overnight)
- Gain staging, full-resolution reverb, pop fixes, 64 steps, EQ, resets, effect toggles, USB-C audio, reliability tests on hardware.

## 0.7 — 4 Nov ≈23:10
- 8-track sequencer, patterns, chains, recording, mutes.

## 0.5 – 0.6 — 4 Nov ≈18:00
- Plaits engines, dub layer, warmth and tape, fader effects.

## 0.4 — 4 Nov ≈13:50
- First sound: a four-operator FM voice.

## 0.1 – 0.3 — 3 Nov 21:32
- Safe launch from the SD card, supervisor and recovery; every recovery test passed on hardware.
