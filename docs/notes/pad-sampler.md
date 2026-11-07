# FM-STEP pad sampler (0.9.27, 7 Nov 2026)

Decision (Darren, 7 Nov): the old sampler (one 16 s sound auto-chopped across 16 pads, GRAB, CHOP modes, SLICE, LIBRARY) is retired. Every pad holds its own sound. Built following the infographic FM-STEP-new-sampler.png.

- 8 banks × 16 pads = 128 slots (slot = bank*16 + pad). Engine: dsp/padsampler.h — int16 stereo 48 kHz, up to 20 s per pad, loader thread (PLOAD), 16 voices, per-pad PSET (in/out, ADSR, mode, pitch, fine, level, pan, rev), PTRK = sampler track (T8, note = slot), PREC/PRECSTOP = SAMPLE IN (records the mix).
- App: cmd/fmstep/pads.go (kit model, kit.json autosave, FMSTEP/kits, WAV peaks read in Go), pads_ui.go (screens).
- Fill: SAMPLE SELECT (card folders, wave, K1 START / K3 TAKE, push onto pad, SHIFT+push chops over the bank), SAMPLE IN, MIXTAPE takes (same browser).
- Shape (CHOP): ENV (wave coloured by envelope stage) → TRIM (MIXTAPE-style IN/OUT, locks, DATA acceleration, zero-cross snap, zoom) → TUNE.
- PAD BANK: 8-bank overview. ERASE + pad twice: empty.
- Button lights: B0 <button note> 08 on / 00 off (verified by mpc-sample-lab RE).
