# FM-STEP controls (v0.9.15)

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
| Hold LOOP + pad | loop length of the playing pattern: pads 1–7 = 1, 2, 4, 8 steps, 1, 2, 4 bars; pad 15 / 16 = one step less / more |
| SHIFT + LOOP | patterns view |
| ERASE + LOOP | clear the chain |
| SHIFT + ERASE (twice) | erase all: the selected track in the step view, or the whole pattern elsewhere · a third press within 5 s undoes |
| 16 LEVELS | play view faces: pages → SPECTRUM → FLOW → pages (knobs keep working; the page and knobs are shown at the bottom) |
| PAD FX | reserved for GRAB / slicing (FLUX part 2) |
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
