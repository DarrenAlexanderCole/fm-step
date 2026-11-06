# FM-STEP controls (v0.9.22)

The bottom pill on every screen says what the controls do there. This is the full list.

## Everywhere

| Control | Does |
|---|---|
| Screen buttons (top) | **left** page down · **right** page up · **middle** the other face of this view (a page ↔ its visualizer, tracks notes ↔ mixer, step view follow ↔ hold bar; elsewhere back to the play view) |
| DATA encoder | turn: page (play view), length (step view), notes ↔ mixer (tracks) · SHIFT + turn: fine-tune the last knob · SHIFT + push: reset the last knob to neutral (twice quickly: the whole page) |
| K1 · K2 · K3 | the three settings shown (colours: K1 cyan, K2 orange, K3 magenta); SHIFT for the alternates shown in orange |
| PLAY / STOP | start from the top / stop the sequencer (echoes ring out); STOP again = hard stop, silence in 30 ms |
| SHIFT + STOP, then STOP | **Exit to stock** (saves first) |
| TAP TEMPO | tap the tempo · hold + DATA: 1 BPM per click |
| − / + | octave |
| NOTE REPEAT (hold) | dub throw into the delay |
| SEQ RECORD | arm recording: pads record notes, knobs record locks |
| MUTE + pad 1–8 | mute / unmute a track |
| KNOB FX | effects view · hold KNOB FX + pad from any view: pads 1–8 pick the fader effect, 9–16 switch effects on/off · SHIFT + KNOB FX: all effects off |
| Fader | rides the picked effect: LOW PASS · HIGH PASS · SLICER · STUTTER · SPACE ECHO · REVERB WASH · PAN · VOLUME |
| DATA push on the MODEL page | mark the model **KEEP** (choosing the 4 instruments; the list goes to the logs) |
| Hold LOOP + pad | loop length of the playing pattern: pads 1–7 = 1, 2, 4, 8 steps, 1, 2, 4 bars; pad 15 / 16 = one step less / more |
| SHIFT + LOOP | patterns view |
| ERASE + LOOP | clear the chain |
| SHIFT + ERASE (twice) | erase all: the selected track in the step view, or the whole pattern elsewhere · a third press within 5 s undoes |
| 16 LEVELS | play view faces: pages → SPECTRUM → FLOW → pages (knobs keep working; the page and knobs are shown at the bottom) |
| PAD FX | **Sampler**: from any view opens the LIBRARY · in the LIBRARY = load the pick and CHOP it · in CHOP / SLICE = back to the LIBRARY |
| SAMPLE RECORD (tap) | **MIXTAPE**: start recording the jam to the card · tap again: stop, then DELETE · LISTEN · SAVE on the screen buttons · SHIFT + tap: show the cassette · (holding it with a pad still saves a sound / project) |
| MAIN VOLUME | output level |

## Views

| Button | View | Pads |
|---|---|---|
| (start) | **Play**: 12 sound pages (MODEL, EQ, AMP, SUB + FILTER, SOUND, WARMTH, WOBBLE, DUB DELAY, REVERB, TAPE, GLOBAL, GROOVE) | play notes on the selected track |
| SEQ | **Steps**: 4 bars × 16 dots | tap = step on/off · hold a step + K1/K2/K3 = note / velocity / length · hold + DATA = timing · hold + SHIFT + K = lock a setting · ERASE + pad = clear step · PAD BANK = next bar |
| SAMPLE | **Tracks**: all tracks' notes, or the mixer (middle button / DATA) | 1–8 select a track, 9–16 mute · K1 level, K2 pan, K3 delay send (SHIFT: reverb send) |
| SHIFT + LOOP | **Patterns** | tap = play that pattern · SHIFT + pads = chain · ERASE + pad = clear · CHOP + pad = copy here |
| KNOB FX | **Effects** | 1–8 pick, 9–16 on/off |
| SAMPLE SELECT | **Sounds** (banks A–D) | load · hold SAMPLE RECORD + pad = save · PAD BANK = next bank |
| SHIFT + SAMPLE SELECT | **Projects** (16 slots) | load · hold SAMPLE RECORD + pad = save · ERASE + pad = new project with these sounds |

## EQ page

K1 LOW, K2 MID, K3 HIGH: centre is flat, right boosts to +18 dB, left cuts to a full kill (−36 dB). SHIFT + K1 / K2 / K3 move each band's frequency: low shelf 40–400 Hz, mid 200 Hz–5 kHz, high shelf 2–16 kHz. The low band also acts on the track's sub oscillator.

## MIXTAPE (the cassette recorder)

Tap SAMPLE RECORD and everything you hear is recorded to the card (after the effects, before MAIN VOLUME) as `FMSTEP/MIXTAPE/take 001.wav`, `take 002.wav`, … (16-bit stereo 48 kHz). A take can run 20 minutes, or less when the card is nearly full; the cassette shows the time left. Keep playing and switching views while it records: a red counter stays in the top bar. Tap SAMPLE RECORD again to stop.

| On stop | Does |
|---|---|
| left: DELETE | throw the take away (press twice) |
| middle: LISTEN | play it back (again: stop) · DATA jumps 5 s |
| right: SAVE | keep it; PAD FX opens the LIBRARY with it at the top, ready to chop |

The file is safe on the card even if FM-STEP stops unexpectedly (good up to the last second). Takes shorter than half a second aren't kept.

## Sampler (LIBRARY · CHOP · SLICE)

PAD FX opens the **LIBRARY**: your MIXTAPE takes (newest first), then the WAVs in `FMSTEP/samples`. DATA picks, PAD FX loads it into CHOP.

- **A take**: the screen shows the whole take and the 16 seconds that will load. K1 moves them (slow: tenths of a second, fast: seconds, SHIFT: 10 s); the middle button LISTENs from there.
- **A file** is a loop to chop; a **folder** of WAVs is a **kit**, one sound per pad (name order, up to 16). Any rate, mono or stereo, 16/24/32-bit; up to 16 s.

| Control | LIBRARY | CHOP | SLICE |
|---|---|---|---|
| K1 | a take: where the 16 s start | DRIFT: how late and loose the hits land, and how much pad pressure drags | START trim · in MAN: moves this slice's marker (SHIFT: fine) |
| K2 | — | CHOP: 16 even · 8 (pads 9–16 = the same 8 reversed) · BEATS (cut on the hits) · MAN (your own markers) | END trim |
| K3 | — | MOTION: ONE SHOT · GATE (plays while held) · LOOP (repeats while held) | PITCH ±12 (SHIFT + K3: reverse) |
| DATA | pick a take, file or kit | the **chop track** (OFF, T1–T8; T8 to start) | same |
| Pads | — | play the slices | pick and play a slice |
| Pad pressure | — | DRIFT: leaning on a pad drags the slice slower, with tape wow | same |
| Fader | — | scrub the reel forward and backward | same |
| NOTE REPEAT + fader | — | the reel spins with the low end; the fader sets how hard | — |
| SHIFT + PAD FX | — | save the chop to the card (`chop 001.wav`, …) | same |
| Middle button | a take: LISTEN / stop · else → CHOP | → SLICE | → CHOP |
| Left / right | — | — | previous / next slice |

**Into the sequence:** the chop track's notes play slices (pad 1 = C1 / note 36 … pad 16 = note 51), with the MOTION and DRIFT you set. Arm SEQ REC, press PLAY and play the pads in CHOP: the hits are recorded onto the chop track like any notes. Edit them in the step view (SEQ, select the chop track). While a track is the chop track, its synth is silent.

PLAY, STOP, SEQ REC, SAMPLE RECORD (MIXTAPE), TAP TEMPO and − / + keep working inside the sampler; any other view button leaves it.
