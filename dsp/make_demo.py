#!/usr/bin/env python3
"""Generate an audition script: every factory preset plays a short riff."""
import json, sys
presets = json.load(open(sys.argv[1]))
bpm = 92
beat = 60 / bpm
DEF = {"engine":8,"harmonics":0.3,"timbre":0.5,"morph":0.4,"decay":0.5,"lpg_colour":0.5,"attack":0.1,"decay2":0.4,
       "sustain":0.8,"release":0.35,"sub_level":0,"sub_oct":1,"cutoff":0.75,"resonance":0.2,"drive":0.2,"wobble_div":4,
       "wobble_depth":0,"delay_div":5,"delay_feedback":0.45,"delay_send":0,"delay_tone":0.5,"reverb_send":0.15,
       "reverb_size":0.5,"reverb_tone":0.5,"tempo":bpm,"voices":6,"level":0.8,"master":0.8,"throw":0,"fader_cutoff":1}
CHAR = {"drift":0.35,"chorus":0.3,"softness":0.35,"tape_drive":0.4,"tape_age":0.25,"hiss":0.15,"crackle":0,"glue":0.5}
for kv in sys.argv[2:]:  # character overrides, e.g. tape_age=0.6
    k, v = kv.split("="); CHAR[k] = float(v)
riffs = {  # (note offset or chord, length in beats)
 "bass": [(0,1),(0,0.5),(3,0.5),(5,1),(-2,1)],
 "chord": [([0,3,7,10],0.5),(None,0.5),([0,3,7,10],0.5),(None,1),([-2,2,5,9],0.5),(None,1)],
 "pad": [([0,3,7],4)],
 "kick": [(0,1)]*4,
 "hats": [(0,0.25)]*8 + [(0,0.5)]*2,
}
sw = []
for i in range(4): sw += [(0,0.66),(0,0.34)]
riffs["boom"] = [(0,0.75),(None,0.25),(0,0.5),(None,1.0),(0,0.5),(0,0.5),(None,0.5)]
riffs["snare"] = [(None,1),(0,1),(None,1),(0,1)]
riffs["hat"] = sw
riffs["ride"] = [(0,1),(0,0.66),(0,0.34),(0,1),(0,0.66),(0,0.34)]
riffs["keys"] = [([2,5,9,12,16],1.5),(None,0.5),([7,11,14,17,21],1.5),(None,0.5),([0,4,7,11,14],3),(None,1)]
riffs["walk"] = [(0,1),(3,1),(5,1),(6,1),(7,1),(5,1),(3,1),(-2,1)]
riffs["stab"] = [([0,3,7,10],0.5),(None,1),([0,3,7,10],0.5),(None,0.5),([0,3,7,10],0.25),(None,1.25)]
kind = {"ELEC BASS":("walk",36),"BOOM KICK":("boom",36),"BRUSH SNARE":("snare",50),"JAZZ HAT":("hat",60),
        "JAZZ RIDE":("ride",64),"WOOD PERC":("bass",60),"DUSTY RHODES":("keys",50),"JAZZ CHORDS":("chord",57),
        "VIBES":("keys",62),"SOUL ORGAN":("keys",50),"FM HOUSE STAB":("stab",60),
        "DUB SUB":("bass",36),"WOBBLE BASS":("bass",36),"STEPPA SKANK":("chord",60),"DEEP CHORD":("chord",57),
        "WARM PAD":("pad",57),"DX KEYS":("chord",60),"ACID LINE":("bass",48),"808 KICK":("kick",36),
        "DUB SNARE":("kick",50),"HI HATS":("hats",60),"MODAL BELL":("chord",72),"STRING PLUCK":("bass",55)}
out = []
for pr in presets:
    out.append("A"); out.append("C")
    for k, v in {**DEF, **pr["p"], **CHAR}.items():
        out.append(f"P {k} {v}")
    r, root = kind[pr["name"]]
    for _ in range(2):
        for notes, beats in riffs[r]:
            if notes is None:
                out.append(f"W {beats*beat:.4f}"); continue
            ns = notes if isinstance(notes, list) else [notes]
            for n in ns: out.append(f"N {root+n} {0.6 + 0.35*((hash((root,n,beats))%7)/6):.2f}")
            gate = beats*beat*0.8
            out.append(f"W {gate:.4f}")
            for n in ns: out.append(f"O {root+n}")
            out.append(f"W {beats*beat-gate:.4f}")
    out.append("W 1.5")
print("\n".join(out))
