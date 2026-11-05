#!/usr/bin/env python3
"""Render FM-STEP's interface in motion as GIFs for the README and press page.

    python3 tools/make_gifs.py [out dir]    (default docs/press/media)

Runs the Go test TestGifFrames (the app's own drawing code, synthetic sound
data), then joins each frame sequence into a looping GIF at 2x size."""
import glob, os, subprocess, sys, tempfile
from PIL import Image

root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(root, "docs", "press", "media")
os.makedirs(out, exist_ok=True)
with tempfile.TemporaryDirectory() as tmp:
    env = dict(os.environ, FMSTEP_GIF=tmp)
    subprocess.run(["go", "test", "./cmd/fmstep", "-run", "TestGifFrames", "-count=1"], cwd=root, env=env, check=True,
                   stdout=subprocess.DEVNULL)
    for seq in sorted(os.listdir(tmp)):
        frames = [Image.open(f).convert("RGB").resize((640, 480), Image.NEAREST)
                  for f in sorted(glob.glob(os.path.join(tmp, seq, "*.png")))]
        pal = [f.quantize(colors=128, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE) for f in frames]
        path = os.path.join(out, f"{seq}.gif")
        pal[0].save(path, save_all=True, append_images=pal[1:], duration=70, loop=0, optimize=True, disposal=1)
        print(path, len(frames), "frames", os.path.getsize(path) // 1024, "KB")
