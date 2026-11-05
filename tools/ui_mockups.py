#!/usr/bin/env python3
"""FM-STEP UI direction mockups, drawn at the MPC Sample's real 320x240
resolution (supersampled 4x, then reduced, which is what anti-aliased drawing
on the device will look like). Usage: ui_mockups.py outdir"""
import math, os, re, sys
from PIL import Image, ImageDraw, ImageFont, ImageFilter

W, H, SS = 320, 240, 4
OUT = sys.argv[1] if len(sys.argv) > 1 else "."
INTER = "/usr/share/fonts/opentype/inter/"

BG = (11, 12, 10)
PANEL = (23, 24, 21)
PANEL2 = (32, 33, 29)
LINE = (52, 54, 47)
TEXT = (238, 235, 226)
DIM = (128, 126, 116)
FAINT = (70, 70, 64)
LIME = (214, 245, 92)
RED = (255, 84, 64)
TRACK = [(254, 40, 20), (254, 140, 0), (240, 220, 0), (100, 240, 20),
         (0, 120, 254), (160, 40, 254), (0, 220, 220), (254, 40, 160)]
TRACK = [tuple(min(255, int(c * 0.92)) for c in t) for t in TRACK]

# 5x7 dot font from internal/panel/font.go
src = open(os.path.join(os.path.dirname(__file__), "..", "internal", "panel", "font.go")).read()
GLYPH = {}
for m in re.finditer(r"'(.)': \{([^}]*)\}", src):
    GLYPH[m.group(1)] = [int(x, 0) for x in m.group(2).split(",")]
GLYPH.setdefault(".", [0, 0, 0, 0, 0, 0, 4])


def font(size, weight="Medium", display=False):
    name = ("InterDisplay-" if display else "Inter-") + weight + ".otf"
    return ImageFont.truetype(INTER + name, int(size * SS))


class Screen:
    def __init__(self):
        self.im = Image.new("RGB", (W * SS, H * SS), BG)
        self.d = ImageDraw.Draw(self.im, "RGBA")

    def s(self, *v):
        return [x * SS for x in v]

    def rect(self, x, y, w, h, col, r=0, outline=None, width=1):
        self.d.rounded_rectangle(self.s(x, y, x + w, y + h), radius=r * SS, fill=col, outline=outline, width=int(width * SS))

    def circle(self, x, y, r, col, outline=None, width=1):
        self.d.ellipse(self.s(x - r, y - r, x + r, y + r), fill=col, outline=outline, width=int(width * SS))

    def text(self, x, y, t, f, col, anchor="la"):
        self.d.text((x * SS, y * SS), t, font=f, fill=col, anchor=anchor)

    def line(self, pts, col, width=1):
        self.d.line([(x * SS, y * SS) for x, y in pts], fill=col, width=int(width * SS), joint="curve")

    def poly(self, pts, col):
        self.d.polygon([(x * SS, y * SS) for x, y in pts], fill=col)

    def arc(self, x, y, r, a0, a1, col, width):
        self.d.arc(self.s(x - r, y - r, x + r, y + r), a0, a1, fill=col, width=int(width * SS))

    def fill_under(self, pts, base, col, top_alpha=110):
        """vertical gradient fill under a curve (line-graph style)"""
        mask = Image.new("L", self.im.size, 0)
        ImageDraw.Draw(mask).polygon([(x * SS, y * SS) for x, y in pts] + [(pts[-1][0] * SS, base * SS), (pts[0][0] * SS, base * SS)], fill=255)
        top = min(p[1] for p in pts)
        grad = Image.new("L", self.im.size, 0)
        gd = ImageDraw.Draw(grad)
        for yy in range(int(top * SS), int(base * SS)):
            a = top_alpha * (1 - (yy - top * SS) / max(1, (base - top) * SS)) ** 1.4
            gd.line([(0, yy), (W * SS, yy)], fill=int(a))
        alpha = Image.composite(grad, Image.new("L", self.im.size, 0), mask)
        layer = Image.new("RGB", self.im.size, col)
        self.im.paste(layer, (0, 0), alpha)

    def dots(self, x, y, t, pitch, col, off=None, r=None):
        """dot-matrix text: every pixel of the 5x7 font is a round dot"""
        r = r or pitch * 0.42
        for ch in t.upper():
            g = GLYPH.get(ch, GLYPH.get("?"))
            for gy in range(7):
                for gx in range(5):
                    on = g[gy] & (0x10 >> gx)
                    if on:
                        self.circle(x + gx * pitch + pitch / 2, y + gy * pitch + pitch / 2, r, col)
                    elif off:
                        self.circle(x + gx * pitch + pitch / 2, y + gy * pitch + pitch / 2, r * 0.55, off)
            x += 6 * pitch
        return x

    def save(self, name):
        small = self.im.resize((W, H), Image.LANCZOS)
        small.save(os.path.join(OUT, name + ".png"))
        return small


def mix(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


# ---------------------------------------------------------------- shared parts
def header(sc, track, name, right_pos="P1  3.2"):
    c = TRACK[track]
    sc.rect(6, 6, 18, 14, c, r=7)
    sc.text(15, 13.5, str(track + 1), font(9, "Bold"), BG, "mm")
    sc.text(30, 13.5, name, font(11, "SemiBold"), TEXT, "lm")
    # transport chip: playing, bar.beat in dot matrix
    sc.rect(226, 5, 88, 17, PANEL, r=8.5)
    sc.poly([(233, 9.5), (233, 17.5), (239.5, 13.5)], LIME)
    sc.dots(245, 8.5, right_pos, 1.45, LIME, off=(40, 46, 30))


def page_dots(sc, idx, n, y=33, label=""):
    x0 = 160 - (n * 7) / 2
    for i in range(n):
        if i == idx:
            sc.rect(x0 + i * 7 - 2, y - 1.5, 9, 3, LIME, r=1.5)
        else:
            sc.circle(x0 + i * 7 + 2.5, y, 1.3, FAINT)


def knob(sc, x, y, r, v, label, val, col, active=False, neutral=None):
    sc.arc(x, y, r, 135, 405, PANEL2, 3.2)
    start = 135 if neutral is None else 135 + 270 * neutral
    end = 135 + 270 * v
    a0, a1 = sorted([start, end])
    if abs(a1 - a0) > 0.5:
        sc.arc(x, y, r, a0, a1, col, 3.2)
    ang = math.radians(end)
    sc.circle(x + r * math.cos(ang), y + r * math.sin(ang), 2.6, TEXT if active else col)
    fs = 10.5 if len(val) <= 4 else 8 if len(val) <= 6 else 7
    sc.text(x, y + 0.5, val, font(fs, "SemiBold"), TEXT, "mm")
    sc.text(x, y + r + 9, label, font(7, "SemiBold"), DIM if not active else TEXT, "mm")


def hint(sc, t, y=228):
    f = font(7, "Medium")
    w = sc.d.textlength(t, font=f) / SS + 16
    sc.rect(160 - w / 2, y - 7.5, w, 15, PANEL, r=7.5)
    sc.circle(160 - w / 2 + 7.5, y, 2, LIME)
    sc.text(160 + 4, y, t, f, DIM, "mm")


def freq_x(f, x0, x1):
    return x0 + (math.log10(f) - math.log10(20)) / (math.log10(20000) - math.log10(20)) * (x1 - x0)


def grid(sc, x0, y0, x1, y1, dbs, dbmin, dbmax):
    for f in (50, 100, 200, 500, 1000, 2000, 5000, 10000):
        x = freq_x(f, x0, x1)
        sc.line([(x, y0), (x, y1)], (255, 255, 255, 14), 0.5)
    for db in dbs:
        y = y0 + (dbmax - db) / (dbmax - dbmin) * (y1 - y0)
        sc.line([(x0, y), (x1, y)], (255, 255, 255, 22 if db == 0 else 10), 0.5)
    for f, t in ((100, "100"), (1000, "1K"), (10000, "10K")):
        sc.text(freq_x(f, x0, x1), y1 + 5, t, font(6, "Medium"), FAINT, "mm")


# ---------------------------------------------------------------- 1 filter
def biquad_db(kind, f0, db, q, f):
    A = 10 ** (db / 40); w0 = 2 * math.pi * f0 / 48000; c = math.cos(w0); s = math.sin(w0)
    al = s / (2 * q); sq = 2 * math.sqrt(A) * al
    if kind == "peak":
        b = [1 + al * A, -2 * c, 1 - al * A]; a = [1 + al / A, -2 * c, 1 - al / A]
    elif kind == "low":
        b = [A * ((A + 1) - (A - 1) * c + sq), 2 * A * ((A - 1) - (A + 1) * c), A * ((A + 1) - (A - 1) * c - sq)]
        a = [(A + 1) + (A - 1) * c + sq, -2 * ((A - 1) + (A + 1) * c), (A + 1) + (A - 1) * c - sq]
    else:
        b = [A * ((A + 1) + (A - 1) * c + sq), -2 * A * ((A - 1) + (A + 1) * c), A * ((A + 1) + (A - 1) * c - sq)]
        a = [(A + 1) - (A - 1) * c + sq, 2 * ((A - 1) - (A + 1) * c), (A + 1) - (A - 1) * c - sq]
    w = 2 * math.pi * f / 48000
    z1 = complex(math.cos(-w), math.sin(-w)); z2 = z1 * z1
    hh = (b[0] + b[1] * z1 + b[2] * z2) / (a[0] + a[1] * z1 + a[2] * z2)
    return 20 * math.log10(abs(hh) + 1e-12)


def ladder_db(fc, res, f):
    s = complex(0, f / fc)
    k = res * 3.8
    h = 1 / ((1 + s) ** 4 + k)
    return 20 * math.log10(abs(h) * (1 + k) + 1e-12)


def screen_filter():
    sc = Screen()
    header(sc, 4, "DUB SUB")
    sc.text(10, 33, "SUB + FILTER", font(8, "SemiBold"), DIM, "lm")
    page_dots(sc, 3, 12, 33)
    x0, y0, x1, y1 = 14, 44, 306, 132
    sc.rect(8, 40, 304, 104, PANEL, r=10)
    grid(sc, x0, y0, x1, y1, (-24, -12, 0, 12), -30, 15)
    fc, res = 420, 0.55
    pts = []
    for i in range(240):
        f = 20 * (1000 ** (i / 239))
        db = max(-30, min(15, ladder_db(fc, res, f)))
        pts.append((freq_x(f, x0, x1), y0 + (15 - db) / 45 * (y1 - y0)))
    col = TRACK[4]
    sc.fill_under(pts, y1, col, 120)
    sc.line(pts, col, 1.6)
    # cutoff marker + label
    cx = freq_x(fc, x0, x1)
    sc.line([(cx, y0), (cx, y1)], (214, 245, 92, 90), 0.8)
    sc.rect(cx + 4, y0 + 2, 46, 13, (214, 245, 92, 230), r=6.5)
    sc.text(cx + 27, y0 + 8.5, "420 HZ", font(7, "Bold"), BG, "mm")
    # sub oscillator glow at the bottom left: a sine "under" the graph
    sp = [(x0 + i * 0.5, y1 - 4 - 3 * math.sin(i * 0.25)) for i in range(70)]
    sc.line(sp, (*col, 160), 1)
    sc.text(x0 + 2, y1 - 16, "SUB -1", font(6, "SemiBold"), col)
    knob(sc, 70, 176, 17, 0.85, "SUB", "85", col)
    knob(sc, 160, 176, 17, 0.42, "CUTOFF", "420", LIME, active=True)
    knob(sc, 250, 176, 17, 0.55, "RESONANCE", "55", col)
    hint(sc, "SHIFT + PUSH  RESET   ·   SHIFT + K1  SUB OCTAVE")
    return sc.save("1-filter")


# ---------------------------------------------------------------- 2 eq
def screen_eq():
    sc = Screen()
    header(sc, 1, "BRUSH SNARE")
    sc.text(10, 33, "EQ", font(8, "SemiBold"), DIM, "lm")
    page_dots(sc, 1, 12, 33)
    x0, y0, x1, y1 = 14, 44, 306, 132
    sc.rect(8, 40, 304, 104, PANEL, r=10)
    grid(sc, x0, y0, x1, y1, (-36, -18, 0, 18), -36, 18)
    bands = [("low", 150, -9, 0.707), ("peak", 1800, 6.5, 0.9), ("high", 6000, -36, 0.707)]
    pts = []
    for i in range(260):
        f = 20 * (1000 ** (i / 259))
        db = sum(biquad_db(k, f0, g, q, f) for k, f0, g, q in bands)
        db = max(-36, min(18, db))
        pts.append((freq_x(f, x0, x1), y0 + (18 - db) / 54 * (y1 - y0)))
    zero = y0 + 18 / 54 * (y1 - y0)
    col = TRACK[1]
    # fill between curve and 0 dB line (boosts bright, cuts darker)
    sc.fill_under(pts, y1, col, 70)
    sc.line(pts, TEXT, 1.4)
    for (k, f0, g, q), lab in zip(bands, "LMH"):
        x = freq_x(f0 if k != "high" else 6000, x0, x1)
        db = max(-36, min(18, sum(biquad_db(kk, ff, gg, qq, f0) for kk, ff, gg, qq in bands)))
        y = y0 + (18 - db) / 54 * (y1 - y0)
        act = lab == "M"
        sc.circle(x, y, 6, LIME if act else PANEL2, outline=LIME if act else TEXT, width=1)
        sc.text(x, y + 0.3, lab, font(6.5, "Bold"), BG if act else TEXT, "mm")
    sc.text(304, 50, "KILL", font(6, "Bold"), RED, "rm")
    knob(sc, 70, 176, 17, 0.375, "LOW", "-9 DB", col, neutral=0.5)
    knob(sc, 160, 176, 17, 0.68, "MID 1.8K", "+6.5", LIME, active=True, neutral=0.5)
    knob(sc, 250, 176, 17, 0.0, "HIGH", "KILL", RED, neutral=0.5)
    hint(sc, "SHIFT + K2  MID FREQ   ·   SHIFT + PUSH  FLAT")
    return sc.save("2-eq")


# ---------------------------------------------------------------- 3 amp
def screen_amp():
    sc = Screen()
    header(sc, 7, "MODAL BELL")
    sc.text(10, 33, "AMP", font(8, "SemiBold"), DIM, "lm")
    page_dots(sc, 2, 12, 33)
    sc.rect(8, 40, 304, 104, PANEL, r=10)
    x0, y0, x1, y1 = 18, 52, 302, 130
    a, d, s, r = 0.06, 0.28, 0.55, 0.38
    sus_w = 0.28
    tot = a + d + sus_w + r
    X = lambda t: x0 + t / tot * (x1 - x0)
    Y = lambda v: y1 - v * (y1 - y0)
    pts = [(X(0), Y(0))]
    for i in range(1, 20):
        t = a * i / 19; pts.append((X(t), Y((i / 19) ** 0.6)))
    for i in range(1, 30):
        t = a + d * i / 29; pts.append((X(t), Y(s + (1 - s) * math.exp(-4 * i / 29))))
    pts.append((X(a + d + sus_w), Y(s)))
    for i in range(1, 30):
        t = a + d + sus_w + r * i / 29; pts.append((X(t), Y(s * math.exp(-4.5 * i / 29))))
    col = TRACK[7]
    sc.fill_under(pts, y1, col, 120)
    sc.line(pts, col, 1.6)
    for t, lab in ((a, "A"), (a + d, "D"), (a + d + sus_w, "S"), (tot, "R")):
        sc.line([(X(t), y0 - 4), (X(t), y1)], (255, 255, 255, 18), 0.5)
        sc.text(X(t) - 3, y0 - 5, lab, font(6, "Bold"), FAINT, "rm")
    # live playhead dot: where the last note's envelope is right now
    tp = a + d + 0.12
    sc.circle(X(tp), Y(s + (1 - s) * math.exp(-4)), 3.2, LIME)
    sc.circle(X(tp), Y(s + (1 - s) * math.exp(-4)), 6, None, outline=(214, 245, 92, 90), width=1)
    knob(sc, 70, 176, 17, 0.3, "ATTACK", "6 MS", col)
    knob(sc, 160, 176, 17, 0.55, "DECAY", "640 MS", col)
    knob(sc, 250, 176, 17, 0.55, "SUSTAIN", "55", LIME, active=True)
    hint(sc, "SHIFT + K3  RELEASE")
    return sc.save("3-amp")


# ---------------------------------------------------------------- 4 steps
def screen_steps():
    sc = Screen()
    header(sc, 4, "DUB SUB")
    sc.text(10, 33, "STEPS  P1  ·  64", font(8, "SemiBold"), DIM, "lm")
    sc.text(310, 33, "FOLLOW", font(7, "Bold"), LIME, "rm")
    col = TRACK[4]
    notes = {0: (1, 3), 6: (0.6, 1), 10: (0.9, 2), 16: (1, 4), 22: (0.8, 2), 26: (0.5, 1), 32: (1, 3), 38: (0.6, 0.5),
             42: (0.9, 2), 48: (1, 6), 58: (0.7, 1), 61: (0.4, 0.25)}
    locks = {22, 42}
    play = 41
    x0, y0, dx, dy = 31, 50, 17.6, 22
    for row in range(4):
        y = y0 + row * dy
        if row == play // 16:  # the bar the pads show: soft panel
            sc.rect(6, y - 10, 308, 20, PANEL, r=10)
        sc.text(12, y, str(row + 1), font(7, "Bold"), LIME if row == play // 16 else FAINT, "mm")
    # tails first (capsules), then heads
    for s, (v, g) in notes.items():
        if g > 1:
            row, c = s // 16, s % 16
            end = min(c + g, 16)
            sc.rect(x0 + c * dx, y0 + row * dy - 2.5, (end - c - 1) * dx + 0.5, 5, (*col, 110), r=2.5)
    for s in range(64):
        row, c = s // 16, s % 16
        x, y = x0 + c * dx, y0 + row * dy
        if s in notes:
            v, g = notes[s]
            r = 3 + 3.2 * v
            sc.circle(x, y, r, (*col, int(120 + 135 * v)))
        else:
            sc.circle(x, y, 1.6 if c % 4 == 0 else 1.0, (90, 90, 82) if c % 4 == 0 else (60, 60, 55))
        if s in locks:
            sc.circle(x, y, 8.5, None, outline=LIME, width=1)
    # playhead: glow column + ring
    px, py = x0 + (play % 16) * dx, y0 + (play // 16) * dy
    glow = Image.new("RGBA", sc.im.size, (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse([(px - 10) * SS, (py - 10) * SS, (px + 10) * SS, (py + 10) * SS], fill=(255, 255, 255, 70))
    glow = glow.filter(ImageFilter.GaussianBlur(5 * SS))
    sc.im.paste(glow, (0, 0), glow)
    sc.circle(px, py, 4, TEXT)
    # held step detail card
    sc.rect(8, 142, 304, 50, PANEL, r=10)
    sc.dots(16, 150, "42", 2.2, TEXT, off=(40, 40, 36))
    sc.text(50, 154, "D#2", font(13, "SemiBold", True), TEXT, "lm")
    sc.text(50, 168, "STEP 42 · BAR 3", font(6.5, "Medium"), DIM, "lm")
    # velocity bar, gate capsule, timing marker
    sc.text(128, 151, "VEL", font(6, "Bold"), FAINT, "lm")
    sc.rect(150, 148.5, 60, 5, PANEL2, r=2.5); sc.rect(150, 148.5, 54, 5, col, r=2.5)
    sc.text(128, 163, "LEN", font(6, "Bold"), FAINT, "lm")
    sc.rect(150, 160.5, 60, 5, PANEL2, r=2.5); sc.rect(150, 160.5, 30, 5, col, r=2.5)
    sc.text(128, 175, "TIME", font(6, "Bold"), FAINT, "lm")
    sc.line([(150, 177), (210, 177)], PANEL2, 1); sc.line([(180, 173), (180, 181)], FAINT, 0.6)
    sc.circle(186, 177, 2.5, TEXT)
    for i, (k, v) in enumerate((("CUTOFF", "62"), ("DELAY", "70"))):
        sc.rect(222, 146 + i * 17, 84, 14, (214, 245, 92, 40), r=7)
        sc.circle(230, 153 + i * 17, 2, LIME)
        sc.text(237, 153 + i * 17, k, font(6.5, "Bold"), LIME, "lm")
        sc.text(301, 153 + i * 17, v, font(6.5, "Bold"), TEXT, "rm")
    # mini overview of all 8 tracks: one thin row each, a dot per note
    pat = [lambda s: s % 4 == 0, lambda s: s % 16 in (4, 12), lambda s: s % 2 == 0, lambda s: s % 8 == 6,
           lambda s: s in notes, lambda s: s % 16 in (2, 9, 14), lambda s: s % 4 == 2, lambda s: s % 32 in (13, 29)]
    for t in range(8):
        for s in range(64):
            if pat[t](s):
                a = 1.0 if t == 4 else 0.55
                sc.circle(12 + s * 4.6 + (s // 16) * 1.2, 199 + t * 3, 0.95, (*TRACK[t], int(255 * a)))
    sc.line([(12 + play * 4.6 + 2 * 1.2, 196), (12 + play * 4.6 + 2 * 1.2, 223)], TEXT, 0.7)
    hint(sc, "HOLD PAD + K1-K3  EDIT   ·   SHIFT + K  LOCK", 231)
    return sc.save("4-steps")


# ---------------------------------------------------------------- 5 mixer
def screen_mixer():
    sc = Screen()
    header(sc, 2, "JAZZ HAT")
    sc.text(10, 33, "MIXER", font(8, "SemiBold"), DIM, "lm")
    names = ["KICK", "SNR", "HAT", "RIDE", "SUB", "CHRD", "SKNK", "BELL"]
    lv = [0.82, 0.6, 0.45, 0.0, 0.9, 0.55, 0.38, 0.25]
    fad = [0.85, 0.7, 0.62, 0.5, 0.8, 0.66, 0.6, 0.7]
    pans = [0.5, 0.45, 0.65, 0.3, 0.5, 0.4, 0.62, 0.58]
    for i in range(8):
        x = 6 + i * 38.6
        sel = i == 2
        muted = i == 3
        sc.rect(x, 42, 35, 182, PANEL if not sel else PANEL2, r=9, outline=(214, 245, 92, 150) if sel else None, width=1)
        c = TRACK[i] if not muted else (80, 80, 74)
        sc.rect(x + 9.5, 47, 16, 13, c, r=6.5)
        sc.text(x + 17.5, 53.5, str(i + 1), font(8, "Bold"), BG, "mm")
        sc.text(x + 17.5, 69, names[i], font(6.5, "Bold"), TEXT if not muted else FAINT, "mm")
        # pan: tiny arc with a dot
        sc.arc(x + 17.5, 86, 7, 200, 340, PANEL2 if not sel else LINE, 1.6)
        a = math.radians(200 + 140 * pans[i])
        sc.circle(x + 17.5 + 7 * math.cos(a), 86 + 7 * math.sin(a), 1.8, TEXT if not muted else FAINT)
        # dot LED meter (16 dots) + fader line
        for k in range(16):
            y = 192 - k * 6.2
            on = k < lv[i] * 16
            cc = LIME if k < 11 else ((255, 190, 40) if k < 14 else RED)
            sc.circle(x + 12, y, 1.7, cc if on else (45, 46, 40))
        fy = 196 - fad[i] * 96
        sc.line([(x + 22, 100), (x + 22, 196)], (60, 60, 54), 1)
        sc.rect(x + 18, fy - 3, 10, 6, TEXT if not muted else FAINT, r=2)
        sc.text(x + 17.5, 212, "M" if muted else f"{int(fad[i]*100)}", font(7, "Bold"), RED if muted else DIM, "mm")
    hint(sc, "PADS 1-8 SELECT  ·  9-16 MUTE  ·  K1 LEVEL  K2 PAN  K3 SEND", 231)
    return sc.save("5-mixer")


# ---------------------------------------------------------------- 6 fx overlay
def screen_fx(base):
    sc = Screen()
    sc.im = base.resize((W * SS, H * SS), Image.LANCZOS)
    sc.d = ImageDraw.Draw(sc.im, "RGBA")
    blur = sc.im.filter(ImageFilter.GaussianBlur(3 * SS))
    sc.im.paste(blur)
    sc.d.rectangle([0, 0, W * SS, H * SS], fill=(0, 0, 0, 150))
    # bottom sheet
    sc.rect(10, 86, 300, 150, (20, 21, 18), r=16)
    sc.rect(146, 91, 28, 3, (70, 70, 64), r=1.5)
    sc.text(22, 108, "STUTTER", font(15, "Bold", True), TEXT, "lm")
    sc.text(22, 124, "1/16  ·  FADER", font(7, "SemiBold"), DIM, "lm")
    # big arc + dot-matrix percentage
    cx, cy = 246, 128
    sc.arc(cx, cy, 30, 135, 405, PANEL2, 5)
    sc.arc(cx, cy, 30, 135, 135 + 270 * 0.7, (240, 220, 0), 5)
    sc.dots(cx - 17, cy - 7, "70", 2.4, TEXT, off=(45, 45, 40))
    sc.text(cx, cy + 16, "%", font(7, "Bold"), DIM, "mm")
    # the 8 effects as pads (pick row), on/off state as rings
    names = ["LP", "HP", "SLICE", "STUTR", "SPACE", "WASH", "PAN", "VOL"]
    cols = [(254, 90, 0), (0, 120, 254), (254, 0, 180), (240, 220, 0), (0, 240, 100), (120, 80, 254), (0, 220, 220), (200, 200, 200)]
    on = [True, False, False, True, False, False, False, False]
    for i in range(8):
        x = 30 + i * 37
        y = 186
        c = cols[i]
        if i == 3:
            sc.circle(x, y, 13, c)
            sc.text(x, y, str(i + 1), font(9, "Bold"), BG, "mm")
        else:
            sc.circle(x, y, 11, (*c, 60 if not on[i] else 200))
            sc.text(x, y, str(i + 1), font(8, "Bold"), TEXT, "mm")
        if on[i] and i != 3:
            sc.circle(x + 8, y - 8, 2.5, LIME)
        sc.text(x, y + 19, names[i], font(5.8, "Bold"), DIM if i != 3 else TEXT, "mm")
    sc.text(160, 226, "PADS 9-16 SWITCH ON / OFF   ·   SHIFT + KNOB FX  ALL OFF", font(6, "Medium"), FAINT, "mm")
    return sc.save("6-fx-overlay")


def sheet(imgs, names, out):
    sc = 2
    pad = 36
    cols = 2
    rows = (len(imgs) + 1) // 2
    S = Image.new("RGB", (cols * (W * sc + pad) + pad, rows * (H * sc + pad + 22) + pad), (30, 30, 28))
    d = ImageDraw.Draw(S)
    f = ImageFont.truetype(INTER + "Inter-SemiBold.otf", 18)
    for i, (im, n) in enumerate(zip(imgs, names)):
        x = pad + (i % cols) * (W * sc + pad)
        y = pad + (i // cols) * (H * sc + pad + 22)
        d.text((x, y), n, font=f, fill=(220, 218, 210))
        S.paste(im.resize((W * sc, H * sc), Image.NEAREST), (x, y + 26))
    S.save(out)


if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    a = screen_filter(); b = screen_eq(); c = screen_amp(); d = screen_steps(); e = screen_mixer(); f = screen_fx(d)
    sheet([a, b, c, d, e, f], ["1  Sub + Filter: response curve + knobs", "2  EQ: live curve, band handles, KILL",
                               "3  Amp: envelope with live position", "4  Steps: dot grid, 4 bars, held step card",
                               "5  Mixer: dot LED meters, pan, faders", "6  Fader effect: overlay sheet"],
          os.path.join(OUT, "fmstep-ui-direction.png"))
