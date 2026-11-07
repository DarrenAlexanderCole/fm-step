# FM-STEP handoff — 0.9.28 (7 Nov 2026)

Start a new conversation in this project with: "continue FM-STEP from handoff-0.9.28".

## Where things are
- Repo branches: `stable-0.9.27` (tag `keep-0.9.27`, hardware-tested return point), `dev-0.9.28` (this build, tag v0.9.28), `master` = 0.9.27 + notes until 0.9.28 is tested. Merge dev-0.9.28 → master after "logs ready" shows it healthy.
- Push bundle: Downloads/FMSTEP-build/release/fm-step-push-0.9.28.bundle (GitHub not linked to the session yet).
- Keep folder: Downloads/FMSTEP-build/keep-0.9.27 (tested card zip + fork bundle).
- Engine sources build on the Mac VM in Downloads/FMSTEP-build/dsp0928 (see workflow doc for the g++ line).

## 0.9.28 contents
16 tracks (1-8 synths, 9-16 = pad banks A-H, note 36+pad); pads mix through their track (level, pan, sends); PMUTE pad mutes; MUTE → PAD MUTES → SOLO; SEQ TRACKS↔STEP; KNOB FX MIXER↔RACK (8 strips a page); SAMPLE = synth pages or SHAPER; K1-K3 pick-up everywhere values live; COLLAGE (shapes + gravity per band); MELT (page K1-K3: flow, count, texture); TRIM opens with no marker picked; no pause bars on MIXTAPE. Projects copied once to projects/before_0.9.28.

## Next (Darren's list)
- INPUT RECORD (1/4" inputs, threshold / tap) — research the capture device first.
- Chromatic pad playing; WAV export; remove the old grab/chop code; update the guide + press kit.
