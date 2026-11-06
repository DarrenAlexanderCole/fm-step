"""FM-STEP launcher icon: dot-matrix FM:STEP in the app's colours."""
import re, sys, math
from PIL import Image, ImageDraw, ImageFilter
src = open(sys.argv[1]).read()
G = {k: [int(x) for x in v.split(',')] for k, v in re.findall(r'"(.)":\[([0-9,]+)\]', src)}
BG, PANEL, LIME, OFF = (11,12,10), (23,24,21), (214,245,92), (40,48,25)
KNOB = [(0x20,0xE8,0xE0), (0xFF,0x9A,0x2E), (0xFF,0x3F,0xA4)]
TRK = [(254,40,20),(254,140,0),(240,220,0),(100,240,20),(0,120,254),(160,40,254),(0,220,220),(254,40,160)]
S = 1024; SS = 2; W = S*SS
img = Image.new('RGBA', (W, W), (0,0,0,0))
# rounded square (macOS icon grid: ~824 px body on 1024 canvas)
m = 100*SS; r = 185*SS
mask = Image.new('L', (W, W), 0); ImageDraw.Draw(mask).rounded_rectangle((m, m, W-m, W-m), r, fill=255)
body = Image.new('RGBA', (W, W), BG+(255,))
d = ImageDraw.Draw(body, 'RGBA')
# knob-colour hills at the bottom
for k, (cx, h, wd) in enumerate([(0.30, 0.17, 0.20), (0.52, 0.21, 0.19), (0.74, 0.15, 0.18)]):
    pts = []
    for i in range(201):
        x = m + (W-2*m)*i/200
        dd = (x - W*cx)/(W*wd)
        pts.append((x, W-m - (W-2*m)*h*math.exp(-dd*dd)))
    poly = pts + [(W-m, W-m), (m, W-m)]
    layer = Image.new('RGBA', (W, W), (0,0,0,0)); ld = ImageDraw.Draw(layer)
    ld.polygon(poly, fill=KNOB[k]+(70,)); ld.line(pts, fill=KNOB[k]+(150,), width=4*SS)
    body = Image.alpha_composite(body, layer)
d = ImageDraw.Draw(body, 'RGBA')
def matrix(text, pitch, y0, dotr):
    cols = len(text)*6 - 1
    x0 = W/2 - (cols-1)*pitch/2
    for i, ch in enumerate(text):
        g = G[ch]
        for gy in range(7):
            for gx in range(5):
                x = x0 + (i*6+gx)*pitch; y = y0 + gy*pitch
                on = g[gy] & (16 >> gx)
                rr = dotr if on else dotr*0.55
                d.ellipse((x-rr, y-rr, x+rr, y+rr), fill=(LIME if on else OFF)+(255,))
matrix("FM", 52*SS, 190*SS, 20*SS)
matrix("STEP", 30*SS, 560*SS, 11.5*SS)
# the loop line with the eight track notes
ly = 830*SS; x0, x1 = 230*SS, W-230*SS
d.line((x0, ly, x1, ly), fill=(154,151,139,200), width=5*SS)
for t in range(8):
    x = x0 + (x1-x0)*(t+0.5)/8
    rr = 17*SS
    d.ellipse((x-rr, ly-rr, x+rr, ly+rr), fill=TRK[t]+(255,))
img.paste(body, (0, 0), mask)
# soft shadow under the body
sh = Image.new('RGBA', (W, W), (0,0,0,0)); ImageDraw.Draw(sh).rounded_rectangle((m, m+12*SS, W-m, W-m+12*SS), r, fill=(0,0,0,110))
sh = sh.filter(ImageFilter.GaussianBlur(18*SS))
out = Image.alpha_composite(sh, img).resize((S, S), Image.LANCZOS)
out.save(sys.argv[2] + '.png')
out.save(sys.argv[2] + '.icns', sizes=[(16,16),(32,32),(64,64),(128,128),(256,256),(512,512),(1024,1024)])
# preview: big + small sizes on a light and dark strip
pv = Image.new('RGB', (1024+40+512+40+256+40+128+40+64+40+32+40, 1064), (236,236,232))
x = 20
for s in (1024, 512, 256, 128, 64, 32):
    pv.paste(out.resize((s, s), Image.LANCZOS), (x, 20 + (1024-s)//2), out.resize((s, s), Image.LANCZOS)); x += s + 40
pv.save(sys.argv[2] + '-preview.png')
print('ok')
