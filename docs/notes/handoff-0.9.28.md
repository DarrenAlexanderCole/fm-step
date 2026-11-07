# FM-STEP handoff, 0.9.28 (7 Nov 2026)

How to continue: start a new conversation in the "AKai Sample firmware FM-STEP" project, pick the GitHub repository `DarrenAlexanderCole/fm-step` when starting it, and say "continue FM-STEP from handoff-0.9.28". Attach the 0.9.28 logs (app.log, launcher.log, recovery.log) with your notes if you have tested.

## 1. What FM-STEP is
An SD-card-launched groovebox for the Akai MPC Sample (stock firmware 1.3.0.12): Plaits-based synth tracks, a step sequencer, a pad sampler, MIXTAPE recorder, RACK effects. Stock firmware is never touched; the app runs from RAM and Exit to Stock (SHIFT + STOP) always brings the stock software back.

Safety rules (never break): no flashing, no overwriting stock binaries or partitions, no disabling signature checks, run from RAM, user data on the SD card, default boot is stock, custom mode only when enabled, Exit to Stock must always work, crashes must fall back to stock.

Licences: FM-STEP is MIT. Behringer KICK (0700-AAM) is AGPL: ideas only, never code. Mutable Instruments Plaits/Clouds/stmlib are MIT (fetched at pinned commits). The mpc-sample-lab reverse engineering is used for protocol facts only (e.g. button LEDs).

## 2. The workflow (agreed with Darren, keep to it)
1. Review the logs ("logs ready": app.log, launcher.log, recovery.log; also readable from the card under FMSTEP/logs when the SAMPLES card is connected).
2. Collect references and input.
3. Mock-ups for anything visual (FMSTEP_MOCK tests), sent as one sheet.
4. Restate exactly what the next build will have; wait for "go".
5. Build: engine (C++) on the Mac's Linux VM, Go in the cloud, `make test`, card zip with tools/mac_app.py.
6. CHANGELOG, commit as Claude (noreply@anthropic.com), tag, push (or a bundle if pushing is blocked).
7. Write FM-STEP-<v>-card.zip to ~/Downloads and say "READY: <v>". Darren then runs Install FM-STEP.app (it saves logs, clears the card, copies the newest zip, ejects).
8. Darren tests. 9. "logs ready". 10. Repeat.

Card messages end with:
```
dot_clean /Volumes/SAMPLES/autorun
dot_clean /Volumes/SAMPLES/FMSTEP
sync; diskutil unmount force /Volumes/SAMPLES
```

Design principles from Darren: infectious controls (big graphic, small text); translucent glass layering; never say the same thing twice; faint line + node, no flag labels; the machine should breathe and feel analog. Recent references: halftone / checker / wave textures, Bauhaus-brutalist shapes, orange (#E8582A) + black on paper.

## 3. Repo, branches, return points
- GitHub: `DarrenAlexanderCole/fm-step` (private until launch). Darren's Mac clone is the folder `fm-step-gh` (not `fm-step`).
- Branches: `stable-0.9.27` + tag `keep-0.9.27` = the hardware-tested return point. `dev-0.9.28` = this build (tag v0.9.28), already on GitHub. `main` on GitHub is still at 0.9.24; the local `master` = 0.9.27 + notes. After 0.9.28 tests healthy: move dev-0.9.28 onto main.
- Old commits c9bffb8 / 8dd6165 / 9c2ff53 are Darren's published commits: never rewrite or force-push them, even though a stop hook asks to re-sign them. All later commits are signed as Claude.
- Downloads/FMSTEP-build/keep-0.9.27: the tested 0.9.27 card zip and a bundle with every branch. Downloads/FMSTEP-build/release: push bundles (fm-step-all.bundle has all branches).
- If pushing from the session is refused ("not in this session's authorized repository set"), the session was started without the repo: start a new conversation with the repo selected, or hand Darren a bundle and the command (run inside fm-step-gh):
  `git fetch ~/Downloads/FMSTEP-build/release/fm-step-all.bundle dev-0.9.28:dev-0.9.28 --tags && git push origin dev-0.9.28 --tags`

## 4. Building
- Cloud: repo at /home/claude/fm-step. `make test` (Go tests + 27/27 recovery tests), `make arm64 club`, then
  `python3 tools/mac_app.py --card dist/club-sd --version X --out <dir>/app --card-zip <dir>`.
  Version lives in Makefile (APP_VERSION) and cmd/fmstep/intro.go (appVersion); autorun.sh gets it from the build.
- Engine (dsp/fmstep_dsp.cc + sampler.h, mixtape.h, padsampler.h, scope.h, reverb_f32.h, usb_resampler.h) builds natively on Darren's Mac Linux VM through the device bridge, in ~/Downloads/FMSTEP-build/dspNNNN:
  `g++ -DTEST -O2 -mcpu=cortex-a55 -ffast-math -fno-exceptions -fno-rtti -include cstdio -Wall -Wno-unused-local-typedefs -Wno-unused-variable -Wno-format-truncation -I. -I../src -static -pthread -o fmstep-dsp-NNNN fmstep_dsp.cc ../build/libplaits.a -lm` then `strip`, then copy back to build/arm64/fmstep-dsp.
  Offline test: `./fmstep-dsp-NNNN --render out.wav script.txt` (engine commands, `W <seconds>` renders).
- Gotcha: committing a file to the same device path twice can silently keep the old one. Use a fresh staging folder name each time and compare md5 on both sides before building.
- Never run a render that leaves threads blocked at exit (the pad loader's mutex/condvar are deliberately never destroyed).

## 5. Architecture (0.9.28)
launcher (autorun.sh) → fmstep-supervisor (recovery, Exit to Stock) → fmstep-app (Go, cmd/fmstep) → fmstep-dsp (C++). App ↔ engine: text commands on stdin/stdout.

Engine: 16 tracks (kTracks 16, kSynthTracks 8). Tracks 9-16 = pad banks A-H: a note 36+pad on track 8+b plays slot b*16+pad. padsampler.h: 128 slots, int16 stereo 48 kHz, up to 20 s each, a loader thread (PLOAD), 16 pad voices, PSET per pad (in/out, ADSR, mode, pitch, fine, level, pan, rev), PMUTE (pad mutes in the sequence), PREC/PRECSTOP (SAMPLE IN = record the mix). Pads mix through their sample track (level, pan, send A/B). RACK: send A/B pick DUB DELAY, REVERB, SPACE ECHO, CHORUS, PHASER, CRUSH; master: TAPE + GLUE, MASTER COMP (mc_*), LIMITER (lim_*), MGR replies. MIXTAPE: recorder with its own TAPE FX chain (MTFX 0-13: comp, EQ, drive, width) + MTSENDA, MTG gain-reduction replies. LV reports 16 tracks + the mix.

App files (cmd/fmstep): main.go (app, buttons, knobs, pick-up via knobPick in mtknobs.go), sequi.go (sequencer UI, SEQ/KNOB FX/PAD FX/SAMPLE buttons), tracks16.go (TRACKS grid, MUTE pages, sample-track page), ui.go/ui2.go (pages, mixer 8 strips a page), pads.go (kit model, kit.json, WAV peaks), pads_ui.go (SHAPER ENV/TRIM/TUNE, SAMPLE SELECT browser, PAD BANKS, SAMPLE IN), shaper.go (envelope maths + colours), rack.go, mixtape.go/mtknobs.go/mtcomp.go/recorder.go/supatape.go (MIXTAPE), util.go (SHIFT + pad functions, PROJECT menu), vis.go (MELT, COLLAGE), scope.go/flow.go/dots.go (other visualizers). internal/seq: project model (16 tracks, PadMute, IsSample). sampler.go/trim.go: old grab/chop code, no longer reachable (to delete).

Button LEDs: CC on channel 1, controller = button note, value 8 on / 0 off (B0 <note> 08).

## 6. Controls (0.9.28)
- SEQ: TRACKS (16 boxes as the pads sit) → STEP EDIT of the selected track → TRACKS.
- SAMPLE: the selected track's sound: synth pages (1-8) or the SHAPER (9-16).
- KNOB FX: MIXER (K1 level, K2 send A, K3 send B, SHIFT+K1 pan; pads 1-16 pick the track) → RACK → MIXER. SHIFT + KNOB FX: pad effects off.
- PAD FX: pad effects (the fader effects; hold + pad 1-8 pick, 9-16 switch). SHIFT + PAD FX: RACK.
- CHOP: SHAPER pages ENV → TRIM → TUNE → out. SAMPLE SELECT: the card browser (push loads onto the pad, SHIFT + push chops over the bank). PAD BANK: 8-bank overview. ERASE + pad twice: empty a pad.
- MUTE: TRACK MUTES → PAD MUTES → SOLO → off. SHIFT + MUTE clears all.
- 16 LEVELS: SPECTRUM → FLOW → DOTS → MELT → COLLAGE.
- SAMPLE RECORD: MIXTAPE take (SHIFT + tap shows the recorder); right button there: TAPE FX.
- SHIFT + pad: the printed functions (FULL LEVEL, HALF/DOUBLE SEQ, COMPRESSOR, HALF/DOUBLE SPEED, FADER, REC QUANTIZE, RESAMPLE, SONG, TRIM SAMPLE, TIME CORRECT, PROJECT menu with SAVE KIT / LOAD KIT / UNDO...). COUNT-IN, MIDI CONFIG, WARP not built.
- K1-K3 pick up: a knob does nothing until it passes the stored value, then follows over its whole range (hollow tick shows where the knob is).
- SHIFT + STOP: Exit to Stock.

## 7. Status
- 0.9.27 tested on hardware 7 Nov: pad sampler worked (loads, SAMPLE IN, kit restored), 0 xruns, render max ~2 ms of 5 ms, drawing ~10 ms median, record lights work.
- 0.9.28 built 7 Nov, not yet tested: 16 tracks, mute/pad-mute/solo, knob pick-up, SEQ/KNOB FX/SAMPLE cycles, mixer paging, TRIM opens with no marker picked, no pause bars on MIXTAPE, new COLLAGE + MELT. First start copies projects to projects/before_0.9.28.

## 8. Next (Darren's list)
1. Review the 0.9.28 logs; fix what the test finds; merge to main.
2. INPUT RECORD: SAMPLE IN from the 1/4" inputs (threshold or tap start). Research the capture device first (only playback is known: card 2 I2S AK4558).
3. Chromatic pad playing (a pad across the keys), WAV export, COUNT-IN.
4. Delete the old sampler code (sampler.go grab/chop/slice, trim.go, engine grab ring) once nothing uses it.
5. Update the synth guide, controls map and press kit for 16 tracks and the pad sampler.
