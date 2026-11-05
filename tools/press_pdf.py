"""Render docs/press/index.html to a PDF: python3 tools/press_pdf.py OUT.pdf"""
import os, sys
from playwright.sync_api import sync_playwright
here = os.path.dirname(os.path.abspath(__file__))
page = os.path.join(here, "..", "docs", "press", "index.html")
with sync_playwright() as p:
    b = p.chromium.launch(); pg = b.new_page(viewport={"width": 1200, "height": 900})
    pg.goto("file://" + os.path.abspath(page), wait_until="networkidle"); pg.wait_for_timeout(1500)
    pg.add_style_tag(content="""
      .dots16 b{animation:none!important}
      section{padding:40px 0} footer{padding:24px 0 0}
      section,figure,.feature,.big div,tr,footer,.timeline .row,.entry,.chartbox{break-inside:avoid}
      body{-webkit-print-color-adjust:exact;print-color-adjust:exact}""")
    pg.evaluate("document.querySelectorAll('details').forEach(d => d.open = true)")  # every build entry expanded
    pg.emulate_media(media="screen")
    pg.pdf(path=sys.argv[1], width="1200px", height="1600px", print_background=True,
           margin={"top": "36px", "bottom": "36px", "left": "0", "right": "0"})
    b.close()
