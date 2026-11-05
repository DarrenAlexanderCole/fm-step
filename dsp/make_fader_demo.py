#!/usr/bin/env python3
"""Fader-suite audition: plays pattern 1 and moves each performance effect the
way the UI does (0.05 per 5 ms glide), plus live length jumps."""
import subprocess, sys
base = subprocess.run([sys.executable, "make_seq_demo.py", sys.argv[1]], capture_output=True, text=True).stdout.splitlines()
setup = [l for l in base[:base.index("PLAY")] if not l.startswith("CHAIN")]
out = setup + ["CHAIN", "PAT 0", "PLAY"]
STEP = 60 / 94 / 4
cur = {"fx_lp": 1, "fx_hp": 0, "fx_slicer": 0, "fx_stutter": 0, "fx_space": 0, "fx_wash": 0, "fx_pan": 0.5, "fx_vol": 1}
def glide(key, target):
    v = cur[key]
    while abs(target - v) > 1e-9:
        v = target if abs(target - v) <= 0.05 else v + (0.05 if target > v else -0.05)
        out.append(f"P {key} {v:.3f}"); out.append("W 0.005")
    cur[key] = v
def wait(steps): out.append(f"W {steps * STEP:.4f}")
wait(16)
# 1 low pass sweep down and back, 2 high pass
glide("fx_lp", 0.25); wait(8); glide("fx_lp", 1); wait(4)
glide("fx_hp", 0.6); wait(8); glide("fx_hp", 0); wait(4)
# 3 slicer: depth, then faster
for v in (0.3, 0.6, 0.9): glide("fx_slicer", v); wait(8)
glide("fx_slicer", 0); wait(4)
# 4 stutter through every zone, then out
for v in (0.2, 0.45, 0.7, 0.95, 0.45): glide("fx_stutter", v); wait(6)
glide("fx_stutter", 0); wait(6)
# 5 space echo throw, 6 reverb wash
glide("fx_space", 0.8); wait(8); glide("fx_space", 0); wait(10)
glide("fx_wash", 1); wait(10); glide("fx_wash", 0); wait(10)
# live length jumps (hold LOOP + pads): 32 -> 4 -> 2 -> 1 -> 8 -> 32
for n, w in ((4, 8), (2, 6), (1, 6), (8, 8), (32, 16)):
    out.append(f"LEN 0 {n}"); wait(w)
out += ["STOP", "W 2.5"]
print("\n".join(out))
