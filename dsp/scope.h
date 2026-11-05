// Spectrum and level analysis for the FM-STEP visualizer (0.9.6).
//
// The audio thread pushes the mix (mono) into a ring and keeps L/R peaks;
// the stdout thread calls Spectrum() ~25 times a second: Hann-windowed
// 2048-point FFT of the newest samples, folded into kScopeBands log-spaced
// bands from 35 Hz to 16 kHz, in dB. Nothing here allocates or locks.
#pragma once
#include <atomic>
#include <cmath>
#include <complex>

static const int kScopeN = 2048, kScopeRing = 4096, kScopeBands = 48;

struct Scope {
  float ring[kScopeRing] = {};
  std::atomic<unsigned> w{0};
  std::atomic<float> peak_l{0}, peak_r{0};

  // audio thread
  inline void Push(float l, float r) {
    unsigned i = w.load(std::memory_order_relaxed);
    ring[i & (kScopeRing - 1)] = 0.5f * (l + r);
    w.store(i + 1, std::memory_order_release);
  }
  inline void Peaks(float l, float r) {  // per block
    if (l > peak_l.load(std::memory_order_relaxed)) peak_l.store(l, std::memory_order_relaxed);
    if (r > peak_r.load(std::memory_order_relaxed)) peak_r.store(r, std::memory_order_relaxed);
  }

  // reader thread: band levels in dB (about -90..0) into out[kScopeBands]
  void Spectrum(float* out, float rate) {
    static std::complex<float> x[kScopeN];
    unsigned end = w.load(std::memory_order_acquire);
    for (int i = 0; i < kScopeN; ++i) {
      float hann = 0.5f - 0.5f * cosf(2.0f * (float)M_PI * i / (kScopeN - 1));
      x[i] = ring[(end - kScopeN + i) & (kScopeRing - 1)] * hann;
    }
    FFT(x, kScopeN);
    float bin_hz = rate / kScopeN;
    for (int b = 0; b < kScopeBands; ++b) {
      float f0 = 35.0f * powf(16000.0f / 35.0f, (float)b / kScopeBands);
      float f1 = 35.0f * powf(16000.0f / 35.0f, (float)(b + 1) / kScopeBands);
      int k0 = (int)(f0 / bin_hz), k1 = (int)(f1 / bin_hz);
      if (k1 <= k0) k1 = k0 + 1;
      float p = 0;
      for (int k = k0; k < k1 && k < kScopeN / 2; ++k) p = fmaxf(p, std::norm(x[k]));
      // Hann coherent gain 0.5 -> full-scale sine ~ 0 dB
      float amp = 2.0f * sqrtf(p) / (0.5f * kScopeN);
      out[b] = 20.0f * log10f(amp + 1e-5f);
    }
  }

  static void FFT(std::complex<float>* a, int n) {
    for (int i = 1, j = 0; i < n; ++i) {
      int bit = n >> 1;
      for (; j & bit; bit >>= 1) j ^= bit;
      j ^= bit;
      if (i < j) std::swap(a[i], a[j]);
    }
    for (int len = 2; len <= n; len <<= 1) {
      float ang = -2.0f * (float)M_PI / len;
      std::complex<float> wl(cosf(ang), sinf(ang));
      for (int i = 0; i < n; i += len) {
        std::complex<float> wv(1, 0);
        for (int j = 0; j < len / 2; ++j) {
          std::complex<float> u = a[i + j], v = a[i + j + len / 2] * wv;
          a[i + j] = u + v;
          a[i + j + len / 2] = u - v;
          wv *= wl;
        }
      }
    }
  }
};
