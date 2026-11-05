"""Render the press illustrations in art.html to PNGs (1600x1000 at 2x)."""
import json, os, sys
from playwright.sync_api import sync_playwright
here = os.path.dirname(os.path.abspath(__file__))
out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(here, "..", "..", "docs", "press", "art")
os.makedirs(out, exist_ok=True)
html = open(os.path.join(here, "art.html")).read().replace("GLYPHS", open(os.path.join(here, "glyphs.json")).read().strip())
tmp = os.path.join(here, ".art_built.html"); open(tmp, "w").write(html)
with sync_playwright() as p:
    b = p.chromium.launch(); pg = b.new_page(viewport={"width": 1600, "height": 1000}, device_scale_factor=float(os.environ.get("SCALE", "1.5")))
    errs = []; pg.on("pageerror", lambda e: errs.append(str(e)))
    pg.goto("file://" + tmp); pg.wait_for_timeout(500)
    if errs: print(errs); sys.exit(1)
    for el in pg.query_selector_all(".art"):
        el.screenshot(path=os.path.join(out, el.get_attribute("id") + ".png"))
    b.close()
os.remove(tmp)
print("ok", sorted(os.listdir(out)))
