// Package gfx is FM-STEP's small drawing kit for the 320x240 screen:
// anti-aliased shapes, alpha blending, gradient fills, Inter text from
// pre-rasterised glyphs (fonts_gen.go) and dot-matrix text. Everything draws
// into a Canvas of 0x00RRGGBB pixels; the panel package copies it to the
// display.
package gfx

import "math"

type Canvas struct {
	W, H int
	Pix  []uint32
}

func NewCanvas(w, h int) *Canvas { return &Canvas{W: w, H: h, Pix: make([]uint32, w*h)} }

// RGB builds a colour from 8-bit channels.
func RGB(r, g, b uint8) uint32 { return uint32(r)<<16 | uint32(g)<<8 | uint32(b) }

// Mix blends a toward b by t (0..1).
func Mix(a, b uint32, t float64) uint32 {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ch := func(s uint) uint32 {
		x := float64((a>>s)&255) + (float64((b>>s)&255)-float64((a>>s)&255))*t
		return uint32(x+0.5) << s
	}
	return ch(16) | ch(8) | ch(0)
}

// Scale darkens a colour (f < 1) or brightens it (f > 1, clipped).
func Scale(c uint32, f float64) uint32 {
	ch := func(s uint) uint32 {
		x := float64((c>>s)&255) * f
		if x > 255 {
			x = 255
		}
		return uint32(x+0.5) << s
	}
	return ch(16) | ch(8) | ch(0)
}

func (c *Canvas) Clear(col uint32) {
	for i := range c.Pix {
		c.Pix[i] = col
	}
}

// Blend paints one pixel with coverage/alpha a (0..1).
func (c *Canvas) Blend(x, y int, col uint32, a float64) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H || a <= 0 {
		return
	}
	i := y*c.W + x
	if a >= 1 {
		c.Pix[i] = col
		return
	}
	c.Pix[i] = Mix(c.Pix[i], col, a)
}

// Add brightens pixel (x,y) by col scaled by a (additive light, clamped).
func (c *Canvas) Add(x, y int, col uint32, a float64) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H || a <= 0 {
		return
	}
	i := y*c.W + x
	p := c.Pix[i]
	ch := func(sh uint) uint32 {
		v := float64(p>>sh&0xFF) + float64(col>>sh&0xFF)*a
		if v > 255 {
			v = 255
		}
		return uint32(v) << sh
	}
	c.Pix[i] = ch(16) | ch(8) | ch(0)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Rect fills an axis-aligned rectangle (fractional edges are anti-aliased).
func (c *Canvas) Rect(x, y, w, h float64, col uint32, alpha float64) {
	c.RoundRect(x, y, w, h, 0, col, alpha)
}

// RoundRect fills a rectangle with rounded corners of radius r.
func (c *Canvas) RoundRect(x, y, w, h, r float64, col uint32, alpha float64) {
	if w <= 0 || h <= 0 {
		return
	}
	r = math.Min(r, math.Min(w, h)/2)
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := int(math.Ceil(x+w)), int(math.Ceil(y+h))
	for py := max(0, y0); py < min(c.H, y1); py++ {
		fy := float64(py) + 0.5
		for px := max(0, x0); px < min(c.W, x1); px++ {
			fx := float64(px) + 0.5
			// signed distance to the rounded box
			dx := math.Max(math.Abs(fx-(x+w/2))-(w/2-r), 0)
			dy := math.Max(math.Abs(fy-(y+h/2))-(h/2-r), 0)
			d := math.Hypot(dx, dy) - r
			if r == 0 {
				d = math.Max(math.Abs(fx-(x+w/2))-w/2, math.Abs(fy-(y+h/2))-h/2)
			}
			cov := clamp01(0.5 - d)
			if cov > 0 {
				c.Blend(px, py, col, cov*alpha)
			}
		}
	}
}

// RoundRectLine draws a 1px-ish outline of a rounded rectangle.
func (c *Canvas) RoundRectLine(x, y, w, h, r, width float64, col uint32, alpha float64) {
	r = math.Min(r, math.Min(w, h)/2)
	x0, y0 := int(math.Floor(x-1)), int(math.Floor(y-1))
	x1, y1 := int(math.Ceil(x+w+1)), int(math.Ceil(y+h+1))
	for py := max(0, y0); py < min(c.H, y1); py++ {
		fy := float64(py) + 0.5
		for px := max(0, x0); px < min(c.W, x1); px++ {
			fx := float64(px) + 0.5
			dx := math.Max(math.Abs(fx-(x+w/2))-(w/2-r), 0)
			dy := math.Max(math.Abs(fy-(y+h/2))-(h/2-r), 0)
			d := math.Abs(math.Hypot(dx, dy) - r)
			if dx == 0 && dy == 0 {
				d = math.Min(w/2-math.Abs(fx-(x+w/2)), h/2-math.Abs(fy-(y+h/2)))
			}
			cov := clamp01(width/2 + 0.5 - d)
			if cov > 0 {
				c.Blend(px, py, col, cov*alpha)
			}
		}
	}
}

// Circle fills a disc.
func (c *Canvas) Circle(cx, cy, r float64, col uint32, alpha float64) {
	for py := max(0, int(cy-r-1)); py <= min(c.H-1, int(cy+r+1)); py++ {
		for px := max(0, int(cx-r-1)); px <= min(c.W-1, int(cx+r+1)); px++ {
			d := math.Hypot(float64(px)+0.5-cx, float64(py)+0.5-cy)
			if cov := clamp01(r + 0.5 - d); cov > 0 {
				c.Blend(px, py, col, cov*alpha)
			}
		}
	}
}

// Ring draws a circle outline of thickness w.
func (c *Canvas) Ring(cx, cy, r, w float64, col uint32, alpha float64) {
	c.Arc(cx, cy, r, w, 0, 360, col, alpha)
}

// Arc draws a thick arc from degree a0 to a1 (0 = right, clockwise, like the
// screen's y-down coordinates) with round caps.
func (c *Canvas) Arc(cx, cy, r, w, a0, a1 float64, col uint32, alpha float64) {
	if a1 < a0 {
		a0, a1 = a1, a0
	}
	full := a1-a0 >= 359.9
	R := r + w/2 + 1
	for py := max(0, int(cy-R)); py <= min(c.H-1, int(cy+R)); py++ {
		for px := max(0, int(cx-R)); px <= min(c.W-1, int(cx+R)); px++ {
			fx, fy := float64(px)+0.5-cx, float64(py)+0.5-cy
			d := math.Abs(math.Hypot(fx, fy) - r)
			cov := clamp01(w/2 + 0.5 - d)
			if cov <= 0 {
				continue
			}
			if !full {
				ang := math.Atan2(fy, fx) * 180 / math.Pi
				for ang < a0 {
					ang += 360
				}
				for ang > a0+360 {
					ang -= 360
				}
				if ang > a1 {
					continue
				}
			}
			c.Blend(px, py, col, cov*alpha)
		}
	}
	if !full {
		for _, a := range []float64{a0, a1} { // round caps
			rad := a * math.Pi / 180
			c.Circle(cx+r*math.Cos(rad), cy+r*math.Sin(rad), w/2, col, alpha)
		}
	}
}

// Line draws an anti-aliased line of width w with round ends.
func (c *Canvas) Line(x0, y0, x1, y1, w float64, col uint32, alpha float64) {
	minx, maxx := math.Min(x0, x1)-w, math.Max(x0, x1)+w
	miny, maxy := math.Min(y0, y1)-w, math.Max(y0, y1)+w
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	for py := max(0, int(miny)); py <= min(c.H-1, int(maxy)); py++ {
		for px := max(0, int(minx)); px <= min(c.W-1, int(maxx)); px++ {
			fx, fy := float64(px)+0.5, float64(py)+0.5
			t := 0.0
			if l2 > 0 {
				t = clamp01(((fx-x0)*dx + (fy-y0)*dy) / l2)
			}
			d := math.Hypot(fx-(x0+t*dx), fy-(y0+t*dy))
			if cov := clamp01(w/2 + 0.5 - d); cov > 0 {
				c.Blend(px, py, col, cov*alpha)
			}
		}
	}
}

// Polyline draws a curve through the points. Overlapping segment ends are
// drawn with "max" coverage so joints do not double up.
func (c *Canvas) Polyline(xs, ys []float64, w float64, col uint32, alpha float64) {
	if len(xs) < 2 {
		return
	}
	minx, maxx, miny, maxy := xs[0], xs[0], ys[0], ys[0]
	for i := range xs {
		minx, maxx = math.Min(minx, xs[i]), math.Max(maxx, xs[i])
		miny, maxy = math.Min(miny, ys[i]), math.Max(maxy, ys[i])
	}
	bx0, by0 := max(0, int(minx-w-1)), max(0, int(miny-w-1))
	bx1, by1 := min(c.W-1, int(maxx+w+1)), min(c.H-1, int(maxy+w+1))
	bw := bx1 - bx0 + 1
	if bw <= 0 || by1 < by0 {
		return
	}
	cov := make([]float64, bw*(by1-by0+1))
	for i := 0; i+1 < len(xs); i++ {
		x0, y0, x1, y1 := xs[i], ys[i], xs[i+1], ys[i+1]
		dx, dy := x1-x0, y1-y0
		l2 := dx*dx + dy*dy
		for py := max(by0, int(math.Min(y0, y1)-w-1)); py <= min(by1, int(math.Max(y0, y1)+w+1)); py++ {
			for px := max(bx0, int(math.Min(x0, x1)-w-1)); px <= min(bx1, int(math.Max(x0, x1)+w+1)); px++ {
				fx, fy := float64(px)+0.5, float64(py)+0.5
				t := 0.0
				if l2 > 0 {
					t = clamp01(((fx-x0)*dx + (fy-y0)*dy) / l2)
				}
				d := math.Hypot(fx-(x0+t*dx), fy-(y0+t*dy))
				v := clamp01(w/2 + 0.5 - d)
				k := (py-by0)*bw + (px - bx0)
				if v > cov[k] {
					cov[k] = v
				}
			}
		}
	}
	for py := by0; py <= by1; py++ {
		for px := bx0; px <= bx1; px++ {
			if v := cov[(py-by0)*bw+(px-bx0)]; v > 0 {
				c.Blend(px, py, col, v*alpha)
			}
		}
	}
}

// FillUnder fills from a curve down to y=base with a vertical gradient:
// topAlpha at the curve's highest point fading to 0 at base. xs must rise.
func (c *Canvas) FillUnder(xs, ys []float64, base float64, col uint32, topAlpha float64) {
	if len(xs) < 2 {
		return
	}
	top := ys[0]
	for _, y := range ys {
		top = math.Min(top, y)
	}
	span := math.Max(1, base-top)
	j := 0
	for px := max(0, int(xs[0])); px <= min(c.W-1, int(xs[len(xs)-1])); px++ {
		fx := float64(px) + 0.5
		for j+1 < len(xs)-1 && xs[j+1] < fx {
			j++
		}
		t := 0.0
		if xs[j+1] != xs[j] {
			t = clamp01((fx - xs[j]) / (xs[j+1] - xs[j]))
		}
		y := ys[j] + (ys[j+1]-ys[j])*t
		for py := max(0, int(y)); py < min(c.H, int(base)); py++ {
			fy := float64(py) + 0.5
			a := topAlpha * math.Pow(1-(fy-top)/span, 1.4)
			if fy < y {
				a *= clamp01(fy - y + 1)
			}
			c.Blend(px, py, col, a)
		}
	}
}

// FillBetween fills, column by column, the area between the curve (xs, ys;
// xs increasing) and the horizontal line y = base, in one flat colour with an
// anti-aliased edge along the curve. Flat fills stay smooth on the panel.
func (c *Canvas) FillBetween(xs, ys []float64, base float64, col uint32, alpha float64) {
	if len(xs) < 2 || alpha <= 0 {
		return
	}
	j := 0
	for px := max(0, int(xs[0])); px <= min(c.W-1, int(xs[len(xs)-1])); px++ {
		fx := float64(px) + 0.5
		for j+1 < len(xs)-1 && xs[j+1] < fx {
			j++
		}
		t := 0.0
		if xs[j+1] != xs[j] {
			t = clamp01((fx - xs[j]) / (xs[j+1] - xs[j]))
		}
		y := ys[j] + (ys[j+1]-ys[j])*t
		lo, hi := math.Min(y, base), math.Max(y, base)
		for py := max(0, int(lo)); py <= min(c.H-1, int(hi)); py++ {
			fy := float64(py)
			cov := math.Min(fy+1, hi) - math.Max(fy, lo) // pixel coverage
			if cov > 0 {
				c.Blend(px, py, col, alpha*math.Min(1, cov))
			}
		}
	}
}

// Poly fills a polygon (4x4 supersampled coverage at the edges).
func (c *Canvas) Poly(xs, ys []float64, col uint32, alpha float64) {
	minx, maxx, miny, maxy := xs[0], xs[0], ys[0], ys[0]
	for i := range xs {
		minx, maxx = math.Min(minx, xs[i]), math.Max(maxx, xs[i])
		miny, maxy = math.Min(miny, ys[i]), math.Max(maxy, ys[i])
	}
	inside := func(px, py float64) bool {
		in := false
		for i, j := 0, len(xs)-1; i < len(xs); j, i = i, i+1 {
			if (ys[i] > py) != (ys[j] > py) && px < (xs[j]-xs[i])*(py-ys[i])/(ys[j]-ys[i])+xs[i] {
				in = !in
			}
		}
		return in
	}
	for py := max(0, int(miny)); py <= min(c.H-1, int(maxy)); py++ {
		for px := max(0, int(minx)); px <= min(c.W-1, int(maxx)); px++ {
			n := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					if inside(float64(px)+(float64(sx)+0.5)/4, float64(py)+(float64(sy)+0.5)/4) {
						n++
					}
				}
			}
			if n > 0 {
				c.Blend(px, py, col, alpha*float64(n)/16)
			}
		}
	}
}

// Glow paints a soft round highlight (a cheap blur).
func (c *Canvas) Glow(cx, cy, r float64, col uint32, alpha float64) {
	for py := max(0, int(cy-r)); py <= min(c.H-1, int(cy+r)); py++ {
		for px := max(0, int(cx-r)); px <= min(c.W-1, int(cx+r)); px++ {
			d := math.Hypot(float64(px)+0.5-cx, float64(py)+0.5-cy) / r
			if d < 1 {
				c.Blend(px, py, col, alpha*(1-d)*(1-d))
			}
		}
	}
}

// Dim darkens a region (overlays).
func (c *Canvas) Dim(x, y, w, h int, f float64) {
	for py := max(0, y); py < min(c.H, y+h); py++ {
		for px := max(0, x); px < min(c.W, x+w); px++ {
			c.Pix[py*c.W+px] = Scale(c.Pix[py*c.W+px], f)
		}
	}
}

// ------------------------------------------------------------------ text

type Glyph struct {
	Adv        float64
	X, Y, W, H int
	Cov        string
}

type Font struct {
	Ascent, Descent, Cap, CapTop int
	Glyphs                       map[rune]Glyph
}

// Anchors for Text: horizontal L/C/R combined with vertical Top or Mid
// (middle of the capital letters, so mixed text lines up with shapes).
const (
	Left   = 0
	Center = 1
	Right  = 2
	Top    = 0
	Mid    = 4
)

func (f *Font) Width(s string) float64 {
	w := 0.0
	for _, r := range s {
		if g, ok := f.Glyphs[r]; ok {
			w += g.Adv
		} else {
			w += f.Glyphs['?'].Adv
		}
	}
	return w
}

// Text draws s and returns its width.
func (c *Canvas) Text(x, y float64, s string, f *Font, col uint32, anchor int) float64 {
	w := f.Width(s)
	switch anchor & 3 {
	case Center:
		x -= w / 2
	case Right:
		x -= w
	}
	if anchor&Mid != 0 {
		y -= float64(f.CapTop) + float64(f.Cap)/2
	}
	ix, iy := int(math.Round(x)), int(math.Round(y))
	pen := 0.0
	for _, r := range s {
		g, ok := f.Glyphs[r]
		if !ok {
			g = f.Glyphs['?']
		}
		gx := ix + int(math.Round(pen)) + g.X
		for yy := 0; yy < g.H; yy++ {
			for xx := 0; xx < g.W; xx++ {
				if a := g.Cov[yy*g.W+xx]; a > 0 {
					c.Blend(gx+xx, iy+g.Y+yy, col, float64(a)/255)
				}
			}
		}
		pen += g.Adv
	}
	return w
}

// Dots draws dot-matrix text from a 5x7 glyph table: every lit pixel is a
// round dot of the given pitch; unlit dots are drawn faintly in off (0 = none).
func (c *Canvas) Dots(x, y float64, s string, glyph func(rune) [7]uint8, pitch float64, col, off uint32) float64 {
	r := pitch * 0.42
	start := x
	for _, ch := range s {
		g := glyph(ch)
		for gy := 0; gy < 7; gy++ {
			for gx := 0; gx < 5; gx++ {
				cx, cy := x+float64(gx)*pitch+pitch/2, y+float64(gy)*pitch+pitch/2
				if g[gy]&(0x10>>gx) != 0 {
					c.Circle(cx, cy, r, col, 1)
				} else if off != 0 {
					c.Circle(cx, cy, r*0.55, off, 1)
				}
			}
		}
		x += 6 * pitch
	}
	return x - start
}
