#!/usr/bin/env python3
"""Sequencer audition: 8 tracks from the factory sounds, 3 chained patterns
(90s jazz hip hop, steppa dub, deep house), parameter locks, mute and a live
recorded note. Usage: make_seq_demo.py factory.json > demo5.txt"""
import json, sys

pre = {p["name"]: p["p"] for p in json.load(open(sys.argv[1]))}
TRACK_KEYS = {"engine", "harmonics", "timbre", "morph", "decay", "lpg_colour", "attack", "decay2", "sustain",
              "release", "sub_level", "sub_oct", "cutoff", "resonance", "drive", "wobble_div", "wobble_depth",
              "delay_send", "reverb_send", "level", "voices", "pan"}
tracks = ["BOOM KICK", "BRUSH SNARE", "JAZZ HAT", "JAZZ RIDE", "DUB SUB", "JAZZ CHORDS", "STEPPA SKANK", "MODAL BELL"]
pans = [0.5, 0.48, 0.62, 0.38, 0.5, 0.42, 0.6, 0.55]
out = []
G = {"tempo": 94, "swing": 0.45, "delay_div": 5, "delay_feedback": 0.55, "delay_tone": 0.4, "reverb_size": 0.55,
     "reverb_tone": 0.45, "drift": 0.35, "chorus": 0.3, "softness": 0.35, "tape_drive": 0.45, "tape_age": 0.3,
     "hiss": 0.15, "crackle": 0.1, "glue": 0.55, "master": 0.8}
for k, v in G.items():
    out.append(f"P {k} {v}")
for t, name in enumerate(tracks):
    for k, v in pre[name].items():
        if k in TRACK_KEYS:
            out.append(f"T {t} {k} {v}")
    out.append(f"T {t} pan {pans[t]}")
out.append("T 4 voices 1")

steps = {}   # (pat, track, step) -> dict, for the --json starter project
lens = {}

def step(p, t, s, note, vel=0.8, gate=0.5, micro=0.0):
    out.append(f"S {p} {t} {s} 1 {note} {vel} {gate} {micro}")
    steps[(p, t, s)] = {"t": t, "s": s, "on": True, "n": note, "v": vel, "g": gate, "m": micro}

def lock(p, t, s, name, v):
    out.append(f"L {p} {t} {s} {name} {v}")
    st = steps.setdefault((p, t, s), {"t": t, "s": s, "n": 60, "v": 0.8, "g": 0.5})
    st.setdefault("l", []).append({"k": name, "v": v})

# chord engine: 11 chords across HARMONICS (OCT 5 sus4 m m7 m9 m11 69 M9 M7 M)
CH = {c: (i + 0.5) / 11 for i, c in enumerate(["OCT", "5", "sus4", "m", "m7", "m9", "m11", "69", "M9", "M7", "M"])}

# ---- pattern 0: 90s jazz hip hop, 2 bars ---------------------------------
P = 0
out.append(f"LEN {P} 32"); lens[P] = 32
for s, v in [(0, 1), (7, 0.7), (10, 0.9), (16, 1), (23, 0.6), (26, 0.95), (30, 0.5)]:
    step(P, 0, s, 36, v)
for s in (4, 12, 20, 28):
    step(P, 1, s, 50, 0.9)
step(P, 1, 15, 50, 0.35, micro=0.1)   # ghost note, a little late
step(P, 1, 31, 50, 0.3)
for s in range(0, 32, 2):
    step(P, 2, s, 60, 0.85 if s % 4 == 0 else 0.55)
for s in (11, 27):
    step(P, 2, s, 60, 0.4)
lock(P, 2, 14, "decay", 0.55)          # open hat
lock(P, 2, 30, "decay", 0.55)
for s, n, g in [(0, 36, 3), (6, 36, 1), (10, 39, 2), (16, 41, 4), (22, 43, 2), (26, 34, 3)]:
    step(P, 4, s, n, 0.9, g)
for s, n, c, g in [(2, 60, "m9", 2), (9, 60, "m9", 0.5), (14, 58, "M9", 3), (18, 56, "M7", 2), (25, 56, "M7", 0.5), (30, 55, "m7", 1.5)]:
    step(P, 5, s, n, 0.7, g)
    lock(P, 5, s, "harmonics", round(CH[c], 4))
step(P, 7, 13, 79, 0.6, 1)
step(P, 7, 29, 75, 0.5, 1)
lock(P, 7, 29, "delay_send", 0.6)

# ---- pattern 1: steppa dub, 2 bars ---------------------------------------
P = 1
out.append(f"LEN {P} 32"); lens[P] = 32
for s in range(0, 32, 4):
    step(P, 0, s, 36, 1)
for s in (8, 24):
    step(P, 1, s, 52, 0.95)
lock(P, 1, 24, "delay_send", 0.85)      # dub throw on the second snare
for s in range(32):
    step(P, 2, s, 60, [0.9, 0.35, 0.6, 0.35][s % 4])
for s in range(2, 32, 4):
    step(P, 6, s, 62, 0.8, 0.4)
    lock(P, 6, s, "harmonics", round(CH["m"], 4))
lock(P, 6, 30, "delay_send", 0.7)
bass = [(0, 38), (3, 38), (6, 41), (8, 43), (11, 41), (14, 36), (16, 38), (19, 38), (22, 45), (24, 43), (27, 41), (30, 36)]
for s, n in bass:
    step(P, 4, s, n, 0.9, 1.6)
lock(P, 4, 22, "cutoff", 0.75)
step(P, 7, 6, 74, 0.5, 1)
lock(P, 7, 6, "delay_send", 0.7)

# ---- pattern 2: deep house, 1 bar ----------------------------------------
P = 2
out.append(f"LEN {P} 16"); lens[P] = 16
for s in range(0, 16, 4):
    step(P, 0, s, 36, 1)
for s in (4, 12):
    step(P, 1, s, 50, 0.8)
for s in range(2, 16, 4):
    step(P, 2, s, 60, 0.85)
    lock(P, 2, s, "decay", 0.45)
for s in range(16):
    if s % 4 != 2:
        step(P, 3, s, 64, 0.35)
for s, n in [(3, 60), (6, 60), (10, 63)]:
    step(P, 5, s, n, 0.8, 0.4)
    lock(P, 5, s, "harmonics", round(CH["m7"], 4))
for s, n in [(2, 36), (6, 36), (10, 39), (14, 36)]:
    step(P, 4, s, n, 0.85, 1)

if len(sys.argv) > 2 and sys.argv[2] == "--json":
    names = [p["name"] for p in json.load(open(sys.argv[1]))]
    notes = [36, 50, 60, 64, 36, 60, 62, 72]
    tr = []
    for t, name in enumerate(tracks):
        p = dict(pre[name]); p["pan"] = pans[t]
        if t == 4: p["voices"] = 1
        tr.append({"sound": {"name": name, "p": p}, "slot": names.index(name) + 1, "muted": False, "note": notes[t]})
    pats = []
    for i in range(16):
        pats.append({"len": lens.get(i, 16), "steps": [v for k, v in sorted(steps.items()) if k[0] == i]})
    g = {k: v for k, v in G.items() if k in ("tempo", "swing", "delay_div", "delay_feedback", "delay_tone", "reverb_size", "reverb_tone")}
    g["rec_quant"] = 1
    print(json.dumps({"version": 1, "name": "STARTER GROOVES", "tracks": tr, "g": g, "patterns": pats,
                      "chain": [0, 0, 1, 1, 2, 2, 2, 2], "sel": 0, "track": 0}, indent=1))
    sys.exit(0)

BPM = 94
step_s = 60 / BPM / 4
out.append("CHAIN 0 0 1 1 2 2 2 2 ")
out.append("PLAY")
t = 0.0
def wait(sec):
    global t
    out.append(f"W {sec:.4f}")
    t += sec
wait(32 * step_s * 2)               # pattern 0 twice
wait(32 * step_s)                   # pattern 1, first pass
out.append("U 7")                   # live-record a bell into the second pass
wait(5 * step_s)
out.append("R 72 0.7")
wait(2 * step_s)
out.append("r 72")
out.append("TL cutoff 0.45")        # record a knob move on the bell track
wait(32 * step_s - 7 * step_s)
out.append("M 4 1")                 # mute the bass for the first house bar
wait(16 * step_s)
out.append("M 4 0")
wait(16 * step_s * 3)
out.append("STOP")
wait(3.0)
print("\n".join(out))
