// FM-STEP: rate converter for the USB audio copy (original code).
// Reads a 48 kHz stereo ring buffer (written on the codec clock) and produces
// frames at the USB gadget's rate (read on the USB host clock) with cubic
// (Catmull-Rom) interpolation. The step is nudged by how full the ring is,
// so the two clocks never drift apart.
#pragma once
#include <algorithm>
#include <cmath>

struct UsbResampler {
  double step = 1, frac = 0, adj = 1, fill_s = 0;
  int target = 2048;
  unsigned ip = 0;  // integer read position in ring frames (wraps with the writer)
  bool started = false;

  void Init(double s, unsigned period) {
    step = s;
    target = (int)std::max(2048.0, period * s * 4);
    started = false;
  }
  unsigned Consumed() const { return ip - 2; }  // keep 2 frames of history for the cubic

  // Fills n stereo frames into out. Returns -1 if not enough input yet,
  // 2 if it had to jump to catch up, else 0. ring_size must be a power of 2.
  int Pull(const float* ring, unsigned ring_size, unsigned w, unsigned r, unsigned n, float* out) {
    if (!started) {
      if (w - r < (unsigned)target * 2) return -1;
      ip = w - target; frac = 0; adj = 1; fill_s = target; started = true;
    }
    int rc = 0;
    double fill = (double)(unsigned)(w - ip) - frac;
    if (fill < n * step * adj + 4) return -1;
    if (fill > target * 6) { ip = w - target; frac = 0; fill = target; rc = 2; }
    // smooth the fill level (writes and reads come in different-sized bursts)
    // over ~1 s, then correct the speed by at most +-0.5 %
    fill_s += (fill - fill_s) * 0.01;
    double err = (fill_s - target) / target;
    adj += (1.0 + 0.002 * err - adj) * 0.02;
    adj = std::max(0.995, std::min(1.005, adj));
    const unsigned m = ring_size - 1;
    for (unsigned i = 0; i < n; ++i) {
      float t = (float)frac;
      for (int ch = 0; ch < 2; ++ch) {
        float y0 = ring[2 * ((ip - 1) & m) + ch], y1 = ring[2 * (ip & m) + ch];
        float y2 = ring[2 * ((ip + 1) & m) + ch], y3 = ring[2 * ((ip + 2) & m) + ch];
        float c1 = 0.5f * (y2 - y0), c2 = y0 - 2.5f * y1 + 2.0f * y2 - 0.5f * y3, c3 = 0.5f * (y3 - y0) + 1.5f * (y1 - y2);
        out[2 * i + ch] = ((c3 * t + c2) * t + c1) * t + y1;
      }
      frac += step * adj;
      unsigned a = (unsigned)frac;
      ip += a;
      frac -= a;
    }
    return rc;
  }
};
