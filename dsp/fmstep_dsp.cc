// fmstep-dsp: FM-STEP's sound engine and sequencer process (0.7).
//
//   8 tracks, each with its own sound:  pooled Plaits voices (Mutable
//   Instruments, MIT) -> per-track sub oscillator, drive ladder and wobble
//   -> shared BBD chorus, dub tape delay, Clouds reverb (MIT), space echo,
//   stutter, tape/master chain, DJ filters -> ALSA hw (raw ioctls).
//   A 16-pattern x 8-track x 32-step sequencer runs inside the audio thread,
//   so timing never depends on the UI.
//
// Commands on stdin (one per line; see Parse() for the full list):
//   P name v               global param, or a track param of the UI track
//   T track name v         track param          U track    select UI track
//   N note vel [track]     note on              O note [track]   note off
//   S pat trk step on note vel gate micro       set a step
//   L pat trk step name v  parameter lock       LX pat trk step  clear locks
//   LEN pat len | CLR pat [trk] | PAT p | CHAIN p.. | PLAY | STOP | M trk 0/1
//   R note vel | r note    live-record a note (UI track) | TL name v  record knob
//   A all notes off | C clear echoes | Q quit
// stdout: READY, STATS, POS pat step, REC/RECG/RECL (recorded data), SPEC (visualizer), ERR.
// The club layer (tracks, sub, filter, wobble, delays, tape, sequencer) is
// original FM-STEP code.

#include <fcntl.h>
#include <pthread.h>
#include <sched.h>
#include <sound/asound.h>
#include <poll.h>
#include <sys/ioctl.h>
#include <unistd.h>

#include <algorithm>
#include <atomic>
#include <cerrno>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <ctime>

#include "plaits/dsp/voice.h"
#include "clouds/dsp/frame.h"
#include "reverb_f32.h"
#include "usb_resampler.h"
#include "scope.h"  // Clouds reverb with float storage (0.7.2)

using clouds::FloatFrame;

static const float kSR = 48000.0f;
static const int kMaxVoices = 12;
static const int kTracks = 8;
static const int kPatterns = 16;
static const int kSteps = 64;
static const int kLocks = 4;
static const size_t kBlock = plaits::kBlockSize;  // 12 frames

// ------------------------------------------------------------------ params

enum TParam {  // per track, lockable per step
  T_ENGINE, T_HARMONICS, T_TIMBRE, T_MORPH, T_DECAY, T_LPG_COLOUR,
  T_ATTACK, T_DECAY2, T_SUSTAIN, T_RELEASE,
  T_SUB_LEVEL, T_SUB_OCT, T_CUTOFF, T_RESONANCE, T_DRIVE,
  T_WOBBLE_DIV, T_WOBBLE_DEPTH, T_DELAY_SEND, T_REVERB_SEND,
  T_LEVEL, T_VOICES, T_PAN,
  T_EQ_LOW, T_EQ_MID, T_EQ_HIGH, T_EQ_MID_FREQ,
  T_EQ_LOW_FREQ, T_EQ_HIGH_FREQ,  // 0.9.13: SHIFT+K1 / SHIFT+K3 on the EQ page
  T_COUNT
};
static const char* kTNames[T_COUNT] = {
  "engine", "harmonics", "timbre", "morph", "decay", "lpg_colour",
  "attack", "decay2", "sustain", "release",
  "sub_level", "sub_oct", "cutoff", "resonance", "drive",
  "wobble_div", "wobble_depth", "delay_send", "reverb_send",
  "level", "voices", "pan",
  "eq_low", "eq_mid", "eq_high", "eq_mid_freq",
  "eq_low_freq", "eq_high_freq",
};
static const float kTDefault[T_COUNT] = {
  8, 0.3f, 0.5f, 0.4f, 0.5f, 0.5f,
  0.1f, 0.4f, 0.8f, 0.35f,
  0.0f, 1, 0.75f, 0.2f, 0.2f,
  4, 0.0f, 0.0f, 0.15f,
  0.8f, 4, 0.5f,
  0.5f, 0.5f, 0.5f, 0.5f,
  0.5740f, 0.5283f,  // 150 Hz and 6 kHz, as before
};

enum GParam {  // global
  G_DELAY_DIV, G_DELAY_FEEDBACK, G_DELAY_TONE, G_REVERB_SIZE, G_REVERB_TONE,
  G_TEMPO, G_MASTER, G_THROW, G_FADER_CUTOFF,
  G_DRIFT, G_CHORUS, G_SOFTNESS, G_TAPE_DRIVE, G_TAPE_AGE, G_HISS, G_CRACKLE, G_GLUE,
  G_FX_LP, G_FX_HP, G_FX_PAN, G_FX_VOL, G_FX_STUTTER, G_FX_SPACE,
  G_SWING, G_REC_QUANT, G_FX_SLICER, G_FX_WASH, G_USB_LEVEL,
  G_SEND_A_FX, G_SEND_B_FX, G_SPACE_AMT,  // RACK: what send A / B feed (0.9.26: index/5: dub delay, reverb, space echo, chorus, phaser, crush)
  G_RCH_RATE, G_RCH_DEPTH, G_RPH_RATE, G_RPH_FB, G_RCR_BITS, G_RCR_RATE,  // 0.9.26 rack chorus / phaser / crush
  G_MC_ON, G_MC_THRESH, G_MC_RATIO, G_MC_ATTACK, G_MC_RELEASE, G_MC_MAKEUP, G_LIM_ON, G_LIM_CEIL, G_LIM_RELEASE,  // 0.9.27 master
  G_COUNT
};
static const char* kGNames[G_COUNT] = {
  "delay_div", "delay_feedback", "delay_tone", "reverb_size", "reverb_tone",
  "tempo", "master", "throw", "fader_cutoff",
  "drift", "chorus", "softness", "tape_drive", "tape_age", "hiss", "crackle", "glue",
  "fx_lp", "fx_hp", "fx_pan", "fx_vol", "fx_stutter", "fx_space",
  "swing", "rec_quant", "fx_slicer", "fx_wash", "usb_level",
  "rack_a", "rack_b", "space_amt",
  "rack_chorus_rate", "rack_chorus_depth", "rack_phaser_rate", "rack_phaser_fb", "rack_crush_bits", "rack_crush_rate",
  "mc_on", "mc_thresh", "mc_ratio", "mc_attack", "mc_release", "mc_makeup", "lim_on", "lim_ceiling", "lim_release",
};
static const float kGDefault[G_COUNT] = {
  5, 0.45f, 0.5f, 0.5f, 0.5f,
  120, 0.0f, 0, 1.0f,
  0.35f, 0.3f, 0.35f, 0.4f, 0.25f, 0.15f, 0.0f, 0.5f,
  1.0f, 0.0f, 0.5f, 1.0f, 0.0f, 0.0f,
  0.0f, 1.0f, 0.0f, 0.0f, 0.625f,
  0, 0.2f, 0.55f,  // A = dub delay, B = reverb (as before 0.9.25)
  0.35f, 0.6f, 0.3f, 0.5f, 0.4f, 0.4f,
  0, 0.6f, 0.12f, 0.3f, 0.2f, 0.2f, 0, 0.95f, 0.15f,
};

static std::atomic<float> g_gp[G_COUNT];
static std::atomic<float> g_tp[kTracks][T_COUNT];
static std::atomic<int> g_ui_track{0};

static void DefaultParams() {
  for (int i = 0; i < G_COUNT; ++i) g_gp[i].store(kGDefault[i]);
  for (int t = 0; t < kTracks; ++t)
    for (int i = 0; i < T_COUNT; ++i) g_tp[t][i].store(kTDefault[i]);
}

static inline float G(GParam p) { return g_gp[p].load(std::memory_order_relaxed); }
static int FindT(const char* n) { for (int i = 0; i < T_COUNT; ++i) if (!strcmp(n, kTNames[i])) return i; return -1; }
static int FindG(const char* n) { for (int i = 0; i < G_COUNT; ++i) if (!strcmp(n, kGNames[i])) return i; return -1; }

// Musical divisions in beats, shortest first.
static const float kDivBeats[] = {0.125f, 1.0f / 6, 0.25f, 1.0f / 3, 0.5f, 0.75f, 2.0f / 3, 1.0f, 1.5f, 2.0f, 4.0f};
static const int kNumDivs = sizeof(kDivBeats) / sizeof(kDivBeats[0]);
static float Bpm() { return std::max(40.0f, std::min(240.0f, G(G_TEMPO))); }
static float DivSeconds(float idx) {
  int i = std::max(0, std::min(kNumDivs - 1, (int)lrintf(idx)));
  return kDivBeats[i] * 60.0f / Bpm();
}

// ------------------------------------------------------------------ queues

enum Op {
  OP_NOTE_ON, OP_NOTE_OFF, OP_ALL_OFF, OP_CLEAR_FX, OP_STEP, OP_LOCK, OP_LOCK_CLEAR,
  OP_LEN, OP_CLR, OP_PAT, OP_CHAIN, OP_PLAY, OP_STOP, OP_MUTE,
  OP_REC_ON, OP_REC_OFF, OP_REC_LOCK, OP_KILL,
};
struct Cmd { int op; int i[10]; float f[4]; };

template <typename T, unsigned kSize>
class Ring {  // single producer, single consumer
 public:
  bool Push(const T& c) {
    unsigned h = head_.load(std::memory_order_relaxed);
    unsigned n = (h + 1) % kSize;
    if (n == tail_.load(std::memory_order_acquire)) return false;
    buf_[h] = c;
    head_.store(n, std::memory_order_release);
    return true;
  }
  bool Pop(T* c) {
    unsigned t = tail_.load(std::memory_order_relaxed);
    if (t == head_.load(std::memory_order_acquire)) return false;
    *c = buf_[t];
    tail_.store((t + 1) % kSize, std::memory_order_release);
    return true;
  }
 private:
  T buf_[kSize];
  std::atomic<unsigned> head_{0}, tail_{0};
};
static Ring<Cmd, 1024> g_cmds;

// Replies from the audio thread (recorded data) to the stdout thread.
struct Rep { char kind; int pat, trk, step, a; float x, y; };
static Ring<Rep, 256> g_reps;
static std::atomic<int> g_pos{-1};  // pat*64+step of the playing step, -1 stopped
static Scope g_scope;               // visualizer tap (mix before MAIN VOLUME)
static std::atomic<bool> g_spec_on{false};  // "SPEC 1": stream band levels
static std::atomic<int> g_spec_track{-1};   // "SPEC 1 t": analyse track t (post EQ, with its sub); -1 = the mix

// ------------------------------------------------------------------ voices

static bool Percussive(int engine) { return engine >= 19; }  // string, modal, drums

struct Env {
  enum { IDLE, ATTACK, DECAY, SUSTAIN, RELEASE } stage = IDLE;
  float v = 0;
  float Process(float a, float d, float s, float r) {
    switch (stage) {
      case ATTACK: v += 1.0f / (a * kSR / kBlock); if (v >= 1) { v = 1; stage = DECAY; } break;
      case DECAY: v = s + (v - s) * expf(-6.9f / (d * kSR / kBlock)); if (v - s < 1e-4f) { v = s; stage = SUSTAIN; } break;
      case SUSTAIN: v = s; break;
      case RELEASE: v *= expf(-6.9f / (r * kSR / kBlock)); if (v < 1e-5f) { v = 0; stage = IDLE; } break;
      default: break;
    }
    return v;
  }
};

// Gain staging (tuned by measurement with FMSTEP_METER; FMSTEP_TUNE overrides offline).
// 0.7.1: 8 tracks summed overdrove every stage of the 0.6 single-sound
// staging (tape input peaked at 6x, glue -3 dB constant). Now each stage has
// headroom: drums hit the track filter below 1, the mix bus peaks ~0.4, tape
// input ~0.8; tape + glue cost < 1 dB of crest factor (demo 5 measurements).
static float kTrackGain = 0.5f, kPercGain = 1.1f, kBusGain = 0.3f, kTapeK = 2.0f;
static float kGlueThr = -15.0f, kGlueAtk = 0.0007f, kGlueRel = 0.00007f, kGlueBoost = 0.0f, kOutGain = 1.5f;

// Final safety limiter: linear up to 0.7, then a smooth knee towards 1.0.
static inline float SafeClip(float x) {
  float a = fabsf(x);
  if (a <= 0.7f) return x;
  float o = a - 0.7f;
  return copysignf(0.7f + o / (1.0f + o * 3.333f), x);
}

// Per-second meters for offline analysis (FMSTEP_METER).
struct SecMeter { float dly, rvin, tape, out, hf; };
static SecMeter g_sec[120];
static long g_frames = 0;
static inline SecMeter& Sec() { return g_sec[std::min(119L, g_frames / 48000)]; }

static uint32_t g_rng = 0x9E3779B9u;
static inline float Rand01() {  // xorshift32
  g_rng ^= g_rng << 13; g_rng ^= g_rng >> 17; g_rng ^= g_rng << 5;
  return (g_rng >> 8) * (1.0f / 16777216.0f);
}

struct SynthVoice {
  plaits::Voice voice;
  char ram[32768];
  int note = -1;
  int track = 0;
  float vel = 0;
  bool gate = false;
  unsigned age = 0;    // allocation order; also the identity stamp for note-offs
  float drift = 0;     // slow random pitch wander (analog oscillator feel)
  float detune = 0;    // per-note fixed offset, cents
  float lp = 0;        // velocity-tracking tone filter state
  int perc_hold = 0;   // blocks of trigger high for percussive engines
  int low = 0;         // blocks of trigger low before a retrigger (clean edge)
  int tail = 0;        // blocks left before a percussive voice counts as free
  int quiet = 0;       // consecutive near-silent blocks (frees finished drum hits)
  Env env;
  plaits::Voice::Frame frames[plaits::kMaxBlockSize];

  void Init() {
    stmlib::BufferAllocator alloc(ram, sizeof(ram));
    voice.Init(&alloc);
  }
  bool Free() const { return note < 0 || (env.stage == Env::IDLE && tail <= 0 && !gate); }
};

static SynthVoice g_voices[kMaxVoices];
static unsigned g_age = 0;

static float TimeParam(float n, float lo, float hi) { return lo * powf(hi / lo, std::max(0.0f, std::min(1.0f, n))); }
static float NoteHz(float n) { return 440.0f * powf(2.0f, (n - 69.0f) / 12.0f); }

// ------------------------------------------------------------------ filters

struct Ladder {  // 4-pole TPT ladder, tanh-saturated feedback; coefficient set per block
  float s[4] = {0, 0, 0, 0};
  float y_prev = 0, G = 0.5f;
  void SetCutoff(float hz) { float g = tanf(3.14159265f * std::min(hz, 0.42f * kSR) / kSR); G = g / (1 + g); }
  float Process(float x, float res, float drive) {
    float k = res * 3.8f;
    float u = tanhf(x * (1 + drive * 4) - k * tanhf(y_prev));
    float y = u;
    for (int i = 0; i < 4; ++i) {
      float v = (y - s[i]) * G;
      float lp = v + s[i];
      s[i] = lp + v;
      y = lp;
    }
    y_prev = y;
    return y * (1 + k * 0.3f) / (1 + drive);  // resonance/drive gain compensation
  }
};

// BBD-style stereo chorus (two modulated short delays in quadrature).
// 0.9.26 RACK send effects: what a send can feed besides delay / reverb /
// space echo. Wet only (the send is the dry).
struct SendFx {
  // chorus: three modulated taps per side
  static const int kLen = 4096;
  float bl[kLen] = {0}, br[kLen] = {0};
  int w = 0;
  float ph = 0, pph = 0;
  // phaser: 6 first-order allpasses per side
  float apl[6] = {0}, apr[6] = {0}, fbl = 0, fbr = 0;
  // crush: sample-and-hold
  float hl = 0, hr = 0, hold = 0, lpl = 0, lpr = 0;
  static float Tap(const float* buf, int w, float d) {
    float pos = w - d;
    while (pos < 0) pos += kLen;
    int i = (int)pos;
    float f = pos - i;
    return buf[i] + (buf[(i + 1) % kLen] - buf[i]) * f;
  }
  static float Allpass(float x, float a, float& z) {  // y = -a x + z; z = x + a y
    float y = -a * x + z;
    z = x + a * y;
    return y;
  }
  void Chorus(float xl, float xr, float rate, float depth, float* l, float* r) {
    bl[w] = xl; br[w] = xr;
    ph += (0.08f + 2.4f * rate * rate) / kSR;
    if (ph >= 1) ph -= 1;
    float base = 0.012f * kSR, dep = (0.0008f + 0.0055f * depth) * kSR;
    float ol = 0, orr = 0;
    for (int k = 0; k < 3; ++k) {
      float p = 6.2831853f * (ph + k / 3.0f);
      ol += Tap(bl, w, base + dep * sinf(p));
      orr += Tap(br, w, base + dep * cosf(p + 0.4f));
    }
    w = (w + 1) % kLen;
    *l = ol * 0.45f; *r = orr * 0.45f;
  }
  void Phaser(float xl, float xr, float rate, float fb, float* l, float* r) {
    pph += (0.04f + 1.6f * rate * rate) / kSR;
    if (pph >= 1) pph -= 1;
    float m1 = 0.5f + 0.5f * sinf(6.2831853f * pph), m2 = 0.5f + 0.5f * cosf(6.2831853f * pph);
    auto coef = [](float m) {  // sweep the notches ~200 Hz .. 3.2 kHz
      float f = 200.0f * powf(16.0f, m);
      float t = tanf(3.14159265f * f / kSR);
      return (1 - t) / (1 + t);
    };
    float a1 = coef(m1), a2 = coef(m2), g = 0.85f * fb;
    float yl = xl + fbl * g, yr = xr + fbr * g;
    for (int k = 0; k < 6; ++k) { yl = Allpass(yl, a1, apl[k]); yr = Allpass(yr, a2, apr[k]); }
    fbl = std::max(-2.0f, std::min(2.0f, yl)); fbr = std::max(-2.0f, std::min(2.0f, yr));
    *l = (xl + yl) * 0.5f; *r = (xr + yr) * 0.5f;
  }
  void Crush(float xl, float xr, float bits, float rate, float* l, float* r) {
    float hz = 22000.0f * powf(1.0f / 22.0f, rate);  // 22 kHz .. 1 kHz
    hold += hz / kSR;
    if (hold >= 1) {
      hold -= 1;
      float q = powf(2.0f, 12.0f - 9.0f * bits);  // 12 .. 3 bits
      hl = roundf(xl * q) / q; hr = roundf(xr * q) / q;
    }
    lpl += (hl - lpl) * 0.6f; lpr += (hr - lpr) * 0.6f;  // take the edge off the aliasing
    *l = lpl; *r = lpr;
  }
  void Process(int fx, float xl, float xr, const float* p, float* l, float* r) {
    if (fx == 3) Chorus(xl, xr, p[0], p[1], l, r);
    else if (fx == 4) Phaser(xl, xr, p[2], p[3], l, r);
    else Crush(xl, xr, p[4], p[5], l, r);
  }
};

struct Chorus {
  static const int kLen = 2048;
  float bl[kLen] = {0}, br[kLen] = {0};
  int w = 0;
  float ph = 0;
  static float Tap(const float* buf, int w, float d) {
    float pos = w - d;
    while (pos < 0) pos += kLen;
    int i = (int)pos;
    float f = pos - i;
    return buf[i] + (buf[(i + 1) % kLen] - buf[i]) * f;
  }
  void Process(float xl, float xr, float amt, float* l, float* r) {
    bl[w] = xl; br[w] = xr;
    ph += 0.55f / kSR;
    if (ph >= 1) ph -= 1;
    float m1 = sinf(6.2831853f * ph), m2 = cosf(6.2831853f * ph);
    float base = 0.0065f * kSR, dep = 0.0022f * kSR;
    float a = Tap(bl, w, base + dep * m1), b = Tap(br, w, base + dep * m2);
    w = (w + 1) % kLen;
    float wet = amt * 0.7f;
    *l = xl * (1 - wet * 0.5f) + a * wet;
    *r = xr * (1 - wet * 0.5f) + b * wet;
  }
};


// ------------------------------------------------------------------ tracks

// RBJ biquad (transposed direct form II), for the per-track 3-band EQ.
struct Biquad {
  float b0 = 1, b1 = 0, b2 = 0, a1 = 0, a2 = 0, z1 = 0, z2 = 0;
  float Run(float x) { float y = b0 * x + z1; z1 = b1 * x - a1 * y + z2; z2 = b2 * x - a2 * y; return y; }
  void Set(int type, float hz, float db, float q) {  // 0 low shelf, 1 peak, 2 high shelf
    float A = powf(10.0f, db / 40.0f), w = 6.2831853f * hz / kSR, c = cosf(w), sn = sinf(w);
    float al = sn / (2 * q), sq = 2 * sqrtf(A) * al, a0;
    if (type == 1) {
      b0 = 1 + al * A; b1 = -2 * c; b2 = 1 - al * A; a0 = 1 + al / A; a1 = -2 * c; a2 = 1 - al / A;
    } else if (type == 0) {
      b0 = A * ((A + 1) - (A - 1) * c + sq); b1 = 2 * A * ((A - 1) - (A + 1) * c); b2 = A * ((A + 1) - (A - 1) * c - sq);
      a0 = (A + 1) + (A - 1) * c + sq; a1 = -2 * ((A - 1) + (A + 1) * c); a2 = (A + 1) + (A - 1) * c - sq;
    } else {
      b0 = A * ((A + 1) + (A - 1) * c + sq); b1 = -2 * A * ((A - 1) + (A + 1) * c); b2 = A * ((A + 1) + (A - 1) * c - sq);
      a0 = (A + 1) - (A - 1) * c + sq; a1 = 2 * ((A - 1) - (A + 1) * c); a2 = (A + 1) - (A - 1) * c - sq;
    }
    b0 /= a0; b1 /= a0; b2 /= a0; a1 /= a0; a2 /= a0;
  }
};

// EQ knob -> gain: centre flat, left half cuts down to -36 dB (DJ-style kill),
// right half boosts up to +18 dB. Mirrored in the UI's dbShow().
static float EqDb(float v) { return v < 0.5f ? (v - 0.5f) * 72.0f : (v - 0.5f) * 36.0f; }

struct Track {
  Biquad eq[3];
  float eq_set[6] = {-1, -1, -1, -1, -1, -1};  // values the EQ coefficients were computed for
  bool eq_on = false;
  Ladder ladder;
  float lfo_phase = 0;
  bool retrig = false;
  float cut_smooth = 0.75f;
  float sub_phase = 0, sub_freq = 0, sub_amp = 0, sub_eq = 1;  // sub_eq: low EQ applied to the sub
  int held_low = -1;
  bool muted = false;
  float lock[T_COUNT];
  bool lock_on[T_COUNT];
  float buf[kBlock];
  long skip_k = -1;  // absolute step whose note was already played live while recording
};
static Track g_trk[kTracks];

// Effective track parameter: the step's lock if one is active, else the knob value.
static inline float TP(int t, int p) {
  return g_trk[t].lock_on[p] ? g_trk[t].lock[p] : g_tp[t][p].load(std::memory_order_relaxed);
}

static void UpdateHeldLow(int t) {
  int lo = -1;
  for (auto& v : g_voices)
    if (v.track == t && v.gate && v.note >= 0 && (lo < 0 || v.note < lo)) lo = v.note;
  g_trk[t].held_low = lo;
}

// Returns the voice index used (the voice's age is its note-off stamp).
// FLUX part 2: one track can play slices instead of its synth (sampler.h)
static std::atomic<int> g_chop_track{-1};
static std::atomic<int> g_chop_motion{0};
static std::atomic<float> g_chop_drift{0.0f};
static void SliceOn(int i, float vel, float lag_ms, int mode);
static void SliceOff(int i);
// PADS (0.9.27, padsampler.h): one track's notes play pad slots (note n = slot n)
static std::atomic<int> g_pad_track{-1};
static void PadOn(int slot, float vel);
static void PadOff(int slot);

static int NoteOn(int t, int note, float vel) {
  if (t == g_pad_track.load(std::memory_order_relaxed)) { PadOn(note, vel); return -1000 - note; }
  if (t == g_chop_track.load(std::memory_order_relaxed)) {
    SliceOn(note - 36, vel, g_chop_drift.load(std::memory_order_relaxed) * 28.0f * (0.55f + 0.45f * Rand01()),
            g_chop_motion.load(std::memory_order_relaxed));
    return -100 - (note - 36);
  }
  int limit = std::max(1, std::min(kMaxVoices, (int)lrintf(TP(t, T_VOICES))));
  int pick = -1, used = 0;
  for (int i = 0; i < kMaxVoices; ++i) {
    SynthVoice& v = g_voices[i];
    if (v.Free() || v.track != t) continue;
    ++used;
    if (v.note == note) pick = i;  // same note: retrigger that voice
  }
  if (pick < 0 && used >= limit) {  // track at its polyphony: steal its oldest (released first)
    for (int pass = 0; pass < 2 && pick < 0; ++pass)
      for (int i = 0; i < kMaxVoices; ++i) {
        SynthVoice& v = g_voices[i];
        if (v.Free() || v.track != t || (pass == 0 && v.gate)) continue;
        if (pick < 0 || v.age < g_voices[pick].age) pick = i;
      }
  }
  if (pick < 0) for (int i = 0; i < kMaxVoices; ++i) if (g_voices[i].Free()) { pick = i; break; }
  if (pick < 0)
    for (int pass = 0; pass < 2 && pick < 0; ++pass)
      for (int i = 0; i < kMaxVoices; ++i) {
        if (pass == 0 && g_voices[i].gate) continue;
        if (pick < 0 || g_voices[i].age < g_voices[pick].age) pick = i;
      }
  SynthVoice& v = g_voices[pick];
  bool was_busy = !v.Free();
  int old_track = v.track;
  v.low = was_busy ? 1 : 0;
  if (v.track != t) v.lp = 0;
  v.note = note;
  v.track = t;
  v.vel = vel;
  v.gate = true;
  v.age = ++g_age;
  if (!was_busy) v.env.v = 0;
  v.env.stage = Env::ATTACK;
  v.perc_hold = 4;
  v.tail = (int)(4.0f * kSR / kBlock);
  v.quiet = 0;
  v.detune = (Rand01() - 0.5f) * 2.0f;  // scaled by the drift amount at render
  g_trk[t].retrig = true;
  UpdateHeldLow(t);
  if (old_track != t) UpdateHeldLow(old_track);
  return pick;
}

static void NoteOff(int t, int note) {
  if (t == g_pad_track.load(std::memory_order_relaxed)) { PadOff(note); return; }
  if (t == g_chop_track.load(std::memory_order_relaxed)) { SliceOff(note - 36); return; }
  for (auto& v : g_voices)
    if (v.track == t && v.note == note && v.gate) { v.gate = false; v.env.stage = Env::RELEASE; }
  UpdateHeldLow(t);
}

static void VoiceOff(int idx, unsigned stamp) {
  if (idx <= -1000) { PadOff(-1000 - idx); return; }  // a sequenced pad ends
  if (idx <= -100) { SliceOff(-100 - idx); return; }  // a sequenced slice ends
  if (idx < 0 || idx >= kMaxVoices) return;
  SynthVoice& v = g_voices[idx];
  if (v.age != stamp || !v.gate) return;
  v.gate = false;
  v.env.stage = Env::RELEASE;
  UpdateHeldLow(v.track);
}

static void AllOff() {
  for (auto& v : g_voices) if (v.gate) { v.gate = false; v.env.stage = Env::RELEASE; }
  for (int t = 0; t < kTracks; ++t) g_trk[t].held_low = -1;
}

// ------------------------------------------------------------------ sequencer

struct Step {
  bool on = false;
  int note = 60;
  float vel = 0.8f, gate = 0.5f, micro = 0;  // gate in steps, micro in steps (-0.45..0.45)
  int lp[kLocks] = {-1, -1, -1, -1};
  float lv[kLocks] = {0, 0, 0, 0};
};
struct Pattern { int len = 16; Step s[kTracks][kSteps]; };
static Pattern* g_pat;  // [kPatterns]

enum { EV_POS, EV_STEP, EV_OFF };
struct Ev { bool used; double t; int kind, track, pat, step, voice; long k; unsigned stamp; };
static const int kMaxEv = 512;
static Ev g_ev[kMaxEv];

struct Seq {
  bool playing = false;
  double pos = 0;   // in 1/16 steps since PLAY
  long next_k = 0;  // next absolute step to schedule
  int enq_pat = 0, enq_step = 0;
  int cur_pat = 0, cur_step = 0;
  long cur_k = 0;
  int sel_pat = 0, queued = -1;
  int chain[16], chain_len = 0, chain_idx = 0;
  struct Held { bool on; int pat, step; double start; } rec[128];
};
static Seq g_seq;

static void PushEv(const Ev& e) {
  for (auto& x : g_ev) if (!x.used) { x = e; x.used = true; return; }
}

static float SwingOffset(int step) {
  return (step & 1) ? std::max(0.0f, std::min(1.0f, G(G_SWING))) * 0.42f : 0.0f;  // up to ~71% swing
}

static void Enqueue(long k) {
  int p = g_seq.enq_pat, s = g_seq.enq_step;
  PushEv({true, (double)k, EV_POS, 0, p, s, 0, k, 0});
  for (int t = 0; t < kTracks; ++t) {
    double when = k + SwingOffset(s) + g_pat[p].s[t][s].micro;
    PushEv({true, std::max(when, g_seq.pos), EV_STEP, t, p, s, 0, k, 0});
  }
  if (++g_seq.enq_step >= g_pat[p].len) {
    g_seq.enq_step = 0;
    if (g_seq.queued >= 0) { g_seq.enq_pat = g_seq.queued; g_seq.queued = -1; }
    else if (g_seq.chain_len > 0) {
      g_seq.chain_idx = (g_seq.chain_idx + 1) % g_seq.chain_len;
      g_seq.enq_pat = g_seq.chain[g_seq.chain_idx];
    }
  }
}

static void ClearLocks(int t) { memset(g_trk[t].lock_on, 0, sizeof(g_trk[t].lock_on)); }

static bool g_trace = false;  // offline: print every trigger (timing tests)

static void FireStep(const Ev& e) {
  Track& tr = g_trk[e.track];
  const Step& st = g_pat[e.pat].s[e.track][e.step];
  ClearLocks(e.track);
  for (int i = 0; i < kLocks; ++i)
    if (st.lp[i] >= 0 && st.lp[i] < T_COUNT) { tr.lock[st.lp[i]] = st.lv[i]; tr.lock_on[st.lp[i]] = true; }
  if (tr.skip_k == e.k) { tr.skip_k = -1; return; }
  if (!st.on || tr.muted) return;
  int v = NoteOn(e.track, st.note, st.vel);
  if (g_trace) printf("TRIG t=%.4fs track=%d pat=%d step=%d note=%d\n", g_seq.pos * 15.0 / Bpm(), e.track, e.pat, e.step, st.note);
  if (v <= -100) {  // a slice (chop track): its off is a slice release
    PushEv({true, e.t + std::max(0.05f, st.gate), EV_OFF, e.track, 0, 0, v, 0, 0});
    return;
  }
  PushEv({true, e.t + std::max(0.05f, st.gate), EV_OFF, e.track, 0, 0, v, 0, g_voices[v].age});
}

static void SeqStop() {
  if (!g_seq.playing) return;
  g_seq.playing = false;
  for (auto& e : g_ev) {
    if (e.used && e.kind == EV_OFF) VoiceOff(e.voice, e.stamp);
    e.used = false;
  }
  for (int t = 0; t < kTracks; ++t) { ClearLocks(t); g_trk[t].skip_k = -1; }
  for (auto& h : g_seq.rec) h.on = false;
  g_pos.store(-1);
}

static void SeqPlay() {
  SeqStop();
  g_seq.playing = true;
  g_seq.pos = 0;
  g_seq.next_k = 0;
  g_seq.chain_idx = 0;
  g_seq.enq_pat = g_seq.chain_len > 0 ? g_seq.chain[0] : g_seq.sel_pat;
  g_seq.enq_step = 0;
  g_seq.queued = -1;
  g_seq.cur_pat = g_seq.enq_pat;
  g_seq.cur_step = 0;
  g_seq.cur_k = 0;
}

// Advance by one audio block, firing everything due before its end.
static void SeqBlock() {
  if (!g_seq.playing) return;
  double steps_per_sample = Bpm() * 4.0 / 60.0 / kSR;
  double end = g_seq.pos + steps_per_sample * kBlock;
  while (g_seq.next_k <= end + 1.0) Enqueue(g_seq.next_k++);  // schedule one step ahead
  for (int kind = EV_OFF; kind >= EV_POS; --kind)  // offs, then steps, then playhead
    for (auto& e : g_ev) {
      if (!e.used || e.kind != kind || e.t >= end) continue;
      e.used = false;
      if (kind == EV_OFF) VoiceOff(e.voice, e.stamp);
      else if (kind == EV_STEP) FireStep(e);
      else { g_seq.cur_pat = e.pat; g_seq.cur_step = e.step; g_seq.cur_k = e.k; g_pos.store(e.pat * 64 + e.step); }
    }
  g_seq.pos = end;
}

static void SetLock(Step& st, int p, float v, int* slot_out) {
  int slot = -1;
  for (int i = 0; i < kLocks; ++i) if (st.lp[i] == p) slot = i;
  if (slot < 0) for (int i = 0; i < kLocks; ++i) if (st.lp[i] < 0) { slot = i; break; }
  if (slot < 0) slot = kLocks - 1;
  st.lp[slot] = p;
  st.lv[slot] = v;
  if (slot_out) *slot_out = slot;
}

// Live recording: put the note on the nearest step (or keep its timing when
// quantise is off). The note itself is played live by the caller.
static void RecNoteOn(int t, int note, float vel) {
  if (!g_seq.playing || note < 0 || note > 127) return;
  int p = g_seq.cur_pat, s = g_seq.cur_step;
  int len = g_pat[p].len;
  double t0 = g_seq.cur_k + SwingOffset(s);
  int s1 = (s + 1) % len;
  double t1 = g_seq.cur_k + 1 + SwingOffset(s1);
  bool next = (g_seq.pos - t0) > (t1 - g_seq.pos);
  int step = next ? s1 : s;
  double micro = g_seq.pos - (next ? t1 : t0);
  if (G(G_REC_QUANT) > 0.5f) micro = 0;
  micro = std::max(-0.45, std::min(0.45, micro));
  Step& st = g_pat[p].s[t][step];
  st.on = true;
  st.note = note;
  st.vel = vel;
  st.gate = 0.5f;
  st.micro = (float)micro;
  if (next) g_trk[t].skip_k = g_seq.cur_k + 1;  // already scheduled: don't play it twice
  g_seq.rec[note] = {true, p, step, g_seq.pos};
  g_reps.Push({'R', p, t, step, note, vel, (float)micro});
}

static void RecNoteOff(int t, int note) {
  if (note < 0 || note > 127) return;
  Seq::Held& h = g_seq.rec[note];
  if (!h.on) return;
  h.on = false;
  float gate = std::max(0.1f, std::min(16.0f, (float)(g_seq.pos - h.start)));
  g_pat[h.pat].s[t][h.step].gate = gate;
  g_reps.Push({'G', h.pat, t, h.step, note, gate, 0});
}

static void RecLock(int t, int p, float v) {
  if (!g_seq.playing) { g_tp[t][p].store(v); return; }
  int slot = 0;
  SetLock(g_pat[g_seq.cur_pat].s[t][g_seq.cur_step], p, v, &slot);
  g_trk[t].lock[p] = v;
  g_trk[t].lock_on[p] = true;
  g_reps.Push({'L', g_seq.cur_pat, t, g_seq.cur_step, p, v, (float)slot});
}

// ------------------------------------------------------------------ fx
struct OnePole {
  float z = 0;
  float Lp(float x, float hz) { float a = 1 - expf(-2 * 3.14159265f * hz / kSR); z += a * (x - z); return z; }
  float Hp(float x, float hz) { return x - Lp(x, hz); }
};

struct DubDelay {  // stereo ping-pong tape delay, feedback can exceed 100%
  static const int kLen = 96000;  // 2 s
  float l[kLen], r[kLen];
  int w = 0;
  float time_smooth = 0.25f;
  float wow_phase = 0;
  OnePole hp_l, hp_r, lp_l, lp_r;
  void Clear() { memset(l, 0, sizeof(l)); memset(r, 0, sizeof(r)); }
  float Read(const float* b, float delay) {
    float pos = w - delay;
    while (pos < 0) pos += kLen;
    int i = (int)pos;
    float f = pos - i;
    return b[i] + (b[(i + 1) % kLen] - b[i]) * f;
  }
  void Process(float in, float target_s, float fb, float tone, float* out_l, float* out_r) {
    time_smooth += (target_s - time_smooth) * 0.00005f;  // slow glide = tape-speed bend
    wow_phase += 0.45f / kSR;
    if (wow_phase > 1) wow_phase -= 1;
    float d = time_smooth * kSR * (1 + 0.0015f * sinf(6.2831853f * wow_phase));
    d = std::max(16.0f, std::min((float)kLen - 4, d));
    float yl = Read(l, d), yr = Read(r, d);
    float lp_hz = 600 + tone * tone * 9000;
    float fl = lp_l.Lp(hp_l.Hp(yl, 120), lp_hz);
    float fr = lp_r.Lp(hp_r.Hp(yr, 120), lp_hz);
    // ping-pong: input to left, left feeds right, right feeds left
    Sec().dly = std::max(Sec().dly, fabsf(in + fb * fr));
    l[w] = tanhf(in + fb * fr);
    r[w] = tanhf(fb * fl);
    w = (w + 1) % kLen;
    *out_l = yl;
    *out_r = yr;
  }
};

// Antiderivative-antialiased tanh: smooth saturation without digital fizz.
struct AdaaTanh {
  float x1 = 0;
  static float F(float x) { float a = fabsf(x); return a > 15 ? a - 0.6931472f : logf(coshf(x)); }
  float Process(float x) {
    float d = x - x1;
    float y = fabsf(d) < 1e-4f ? tanhf(0.5f * (x + x1)) : (F(x) - F(x1)) / d;
    x1 = x;
    return y;
  }
};

// Tape + mastering character: glue compressor -> pre-emphasis -> asymmetric
// saturation (ADAA) -> de-emphasis -> head bump -> HF loss -> wow & flutter
// -> hiss and crackle. Everything at "0" is (nearly) transparent.
struct Tape {
  float drive = 0.4f, age = 0.3f, hiss = 0.15f, crackle = 0, glue = 0.5f;
  AdaaTanh sat[2];
  OnePole pre[2], de[2], dc[2], hf1[2], hf2[2], hiss_bp[2];
  float bump_lp[2] = {0, 0}, bump_bp[2] = {0, 0};
  float env = 0;
  // wow & flutter delay
  static const int kLen = 1024;
  float wbuf[2][kLen] = {{0}};
  int w = 0;
  float wow_ph = 0, flut_ph = 0, wander = 0;
  float pink[2][3] = {{0}};
  float crk = 0, crk_lp[2] = {0, 0};
  float m_gr = 0, m_gr_sum = 0, m_sat = 0; long m_n = 0;  // meters (offline analysis)
  void Reset() {  // forget the audio inside the tape (hard stop)
    memset(wbuf, 0, sizeof(wbuf));
    for (int ch = 0; ch < 2; ++ch) {
      sat[ch] = AdaaTanh();
      pre[ch].z = de[ch].z = dc[ch].z = hf1[ch].z = hf2[ch].z = hiss_bp[ch].z = 0;
      bump_lp[ch] = bump_bp[ch] = 0;
    }
    env = 0;
  }
  void Set(float d, float a, float h, float c, float g) { drive = d; age = a; hiss = h; crackle = c; glue = g; }
  float Pink(int ch) {  // Paul Kellet's economy pink filter
    float wn = Rand01() * 2 - 1;
    float* b = pink[ch];
    b[0] = 0.99765f * b[0] + wn * 0.0990460f;
    b[1] = 0.96300f * b[1] + wn * 0.2965164f;
    b[2] = 0.57000f * b[2] + wn * 1.0526913f;
    return (b[0] + b[1] + b[2] + wn * 0.1848f) * 0.2f;
  }
  void Process(float* l, float* r) {
    // 1. glue: stereo-linked RMS compressor, ~2:1 above -16 dBFS
    float lvl = 0.5f * (*l * *l + *r * *r);
    float coef = lvl > env ? kGlueAtk : kGlueRel;
    env += coef * (lvl - env);
    float db = 10.0f * log10f(env + 1e-9f);
    float over = db - kGlueThr;
    float gr_db = over > 0 ? -over * 0.5f * glue : 0;
    float gc = powf(10.0f, (gr_db + kGlueBoost * glue) / 20.0f);
    m_gr = std::min(m_gr, gr_db); m_gr_sum += gr_db; ++m_n;
    float io[2] = {*l * gc, *r * gc};
    // wow (slow) + flutter (fast) + random wander, deeper with age
    wow_ph += 0.55f / kSR; if (wow_ph >= 1) wow_ph -= 1;
    flut_ph += 6.8f / kSR; if (flut_ph >= 1) flut_ph -= 1;
    wander = wander * 0.9998f + (Rand01() - 0.5f) * 0.004f;
    float mod_s = age * (0.0009f * sinf(6.2831853f * wow_ph) + 0.00003f * sinf(6.2831853f * flut_ph) + 0.004f * wander);
    float delay = (0.003f + mod_s) * kSR;
    float hf_hz = 20000.0f - age * 13000.0f;
    float k = 1.0f + drive * kTapeK;  // input gain into the tape
    for (int ch = 0; ch < 2; ++ch) {
      float x = io[ch];
      // pre-emphasis (+highs), saturate, de-emphasis: highs compress gently
      float pe = x + 0.25f * pre[ch].Hp(x, 3500);  // gentle pre-emphasis (0.7.2: was 0.6 @ 2.5k, crunched hats)
      float bias = 0.08f * drive;  // asymmetry -> warm even harmonics
      m_sat = std::max(m_sat, fabsf(pe * k + bias));
      Sec().tape = std::max(Sec().tape, fabsf(pe * k + bias));
      float y = sat[ch].Process(pe * k + bias) / k * (1.0f + 0.15f * drive);
      y = dc[ch].Hp(y, 12);
      float lpd = de[ch].Lp(y, 3500);
      y = lpd + (y - lpd) / 1.25f;
      // head bump: +2 dB around 70 Hz (resonant low shelf feel)
      float f = 2.0f * sinf(3.14159265f * 70.0f / kSR);
      bump_lp[ch] += f * bump_bp[ch];
      float hp = y - bump_lp[ch] - 0.7f * bump_bp[ch];
      bump_bp[ch] += f * hp;
      y += 0.26f * bump_bp[ch];
      // HF loss with age
      y = hf2[ch].Lp(hf1[ch].Lp(y, hf_hz), hf_hz * 1.3f);
      // wow & flutter
      wbuf[ch][w] = y;
      float pos = w - delay;
      while (pos < 0) pos += kLen;
      int i = (int)pos;
      float fr = pos - i;
      y = wbuf[ch][i] + (wbuf[ch][(i + 1) % kLen] - wbuf[ch][i]) * fr;
      // hiss: shaped pink noise around 1-8 kHz, about -66 dBFS at 0.5
      float hn = hiss_bp[ch].Hp(Pink(ch), 400) * hiss * hiss * 0.004f;
      y += hn;
      io[ch] = y;
    }
    w = (w + 1) % kLen;
    // crackle: sparse dust clicks, random stereo position
    if (crackle > 0 && Rand01() < crackle * crackle * 6.0f / kSR) crk = (Rand01() * 0.5f + 0.5f) * 0.035f * (Rand01() < 0.5f ? -1 : 1);
    float pan = Rand01();
    crk_lp[0] += 0.5f * (crk * pan - crk_lp[0]);
    crk_lp[1] += 0.5f * (crk * (1 - pan) - crk_lp[1]);
    crk *= 0.6f;
    float makeup = (1.0f + 0.7f * glue + 0.4f * drive) * kOutGain;  // keep loudness equal so warmth is a fair A/B
    *l = io[0] * makeup + crk_lp[0];
    *r = io[1] * makeup + crk_lp[1];
  }
};

// ---- performance fader effects (original FM-STEP code) --------------------

// Zero-delay-feedback state-variable filter (Simper), per channel.
struct Svf {
  float ic1 = 0, ic2 = 0;
  static float G(float hz) { return tanf(3.14159265f * std::min(hz, 0.45f * kSR) / kSR); }
  void Run(float x, float g, float q, float* lp, float* hp) {
    float k = 1.0f / q;
    float a1 = 1 / (1 + g * (g + k)), a2 = g * a1, a3 = g * a2;
    float v3 = x - ic2;
    float v1 = a1 * ic1 + a2 * v3;
    float v2 = ic2 + a2 * ic1 + a3 * v3;
    ic1 = 2 * v1 - ic1;
    ic2 = 2 * v2 - ic2;
    *lp = v2;
    *hp = x - k * v1 - v2;
  }
};

// Space echo: three tape heads (1:2:3 like a classic spring/tape echo unit),
// saturating feedback through a band-passed, wobbling tape loop, plus a
// small spring-style diffuser. The fader sets send and feedback together:
// all the way up runs away into self-oscillation.
struct SpaceEcho {
  static const int kLen = 144000;  // 3 s
  float buf[kLen];
  int w = 0;
  float wob = 0, wander = 0;
  OnePole hp, lp;
  float ap[4][512];
  int apw[4] = {0, 0, 0, 0};
  float fb_state = 0;
  void Clear() { memset(buf, 0, sizeof(buf)); memset(ap, 0, sizeof(ap)); }
  float Head(float d) {
    float pos = w - d;
    while (pos < 0) pos += kLen;
    int i = (int)pos;
    float f = pos - i;
    return buf[i] + (buf[(i + 1) % kLen] - buf[i]) * f;
  }
  float Diffuse(float x) {
    static const int len[4] = {142, 211, 331, 467};
    for (int k = 0; k < 4; ++k) {
      int r = (apw[k] - len[k] + 512) % 512;
      float d = ap[k][r];
      float v = x + 0.6f * d;
      ap[k][apw[k]] = v;
      apw[k] = (apw[k] + 1) % 512;
      x = d - 0.6f * v;
    }
    return x;
  }
  void Process(float in, float amount, float beat_s, float* l, float* r) {
    if (amount < 0.002f && fb_state < 1e-5f) { buf[w] = 0; w = (w + 1) % kLen; *l = *r = 0; return; }
    wob += 0.7f / kSR; if (wob >= 1) wob -= 1;
    wander = wander * 0.9997f + (Rand01() - 0.5f) * 0.003f;
    float base = 0.375f * beat_s * kSR * (1 + 0.002f * sinf(6.2831853f * wob) + 0.01f * wander);  // dotted 1/16
    base = std::min(base, kLen / 3.2f);
    float h1 = Head(base), h2 = Head(2 * base), h3 = Head(3 * base);
    float send = powf(amount, 0.7f);
    float fb = amount * 1.08f;
    float loop = lp.Lp(hp.Hp(0.45f * h2 + 0.55f * h3, 180), 3200);
    fb_state = fabsf(loop);
    buf[w] = tanhf(in * send * 1.4f + fb * loop);
    w = (w + 1) % kLen;
    float spring = Diffuse(h1 + h2 + h3) * 0.35f;
    *l = 0.8f * h1 + 0.5f * h2 + 0.2f * h3 + spring;
    *r = 0.2f * h1 + 0.5f * h2 + 0.8f * h3 + spring;
  }
};

// Stutter / beat repeat: capture the slice just played and loop it.
// Fader zones pick the slice: 1/4, 1/8, 1/16, 1/32 (higher = faster).
// Stutter / beat repeat: capture the slice just played and loop it.
// Fader zones pick the slice: 1/4, 1/8, 1/16, 1/32 (higher = faster).
// 0.7.3: two loop slots; a new slice fades in while the old one fades out
// (3 ms), and switching off crossfades back to the dry signal, so moving
// the fader through the zones no longer pops.
struct Stutter {
  static const int kHist = 96000;  // 2 s of history
  float hist[2][kHist];
  float loop[2][2][kHist];  // [slot][channel]
  int w = 0, len[2] = {0, 0}, pos[2] = {0, 0}, cur = 0, zone = -1;
  float g[2] = {0, 0};
  float mix = 0;
  void Clear() { memset(hist, 0, sizeof(hist)); }
  void Capture(int slot, int z, float beat_s) {
    float beats = 1.0f / (1 << z);  // 1/4 .. 1/32 note
    int n = std::max(64, std::min(kHist - 1, (int)(beats * beat_s * kSR)));
    for (int i = 0; i < n; ++i) {
      int src = (w - n + i + kHist) % kHist;
      loop[slot][0][i] = hist[0][src];
      loop[slot][1][i] = hist[1][src];
    }
    len[slot] = n;
    pos[slot] = 0;
  }
  void Process(float* l, float* r, float amount, float beat_s) {
    hist[0][w] = *l; hist[1][w] = *r;
    w = (w + 1) % kHist;
    int z = amount < 0.06f ? -1 : amount < 0.3f ? 0 : amount < 0.55f ? 1 : amount < 0.8f ? 2 : 3;
    if (z != zone) {
      if (z >= 0) { cur ^= 1; Capture(cur, z, beat_s); }
      zone = z;
    }
    const float step = 1.0f / 144.0f;  // 3 ms slot crossfade
    for (int k = 0; k < 2; ++k) {
      float t = k == cur ? 1.0f : 0.0f;
      g[k] += std::max(-step, std::min(step, t - g[k]));
    }
    float target = zone >= 0 ? 1.0f : 0.0f;
    mix += (target - mix) * 0.004f;
    if (mix < 1e-4f && zone < 0) return;
    float sl = 0, sr = 0;
    for (int k = 0; k < 2; ++k) {
      if (g[k] <= 0 || len[k] <= 0) continue;
      int n = len[k], p = pos[k];
      int fade = std::min(240, n / 4);  // short fades at the loop seam
      float e = g[k];
      if (p < fade) e *= p / (float)fade;
      else if (p > n - fade) e *= (n - p) / (float)fade;
      sl += loop[k][0][p] * e;
      sr += loop[k][1][p] * e;
      pos[k] = (p + 1) % n;
    }
    *l = *l * (1 - mix) + sl * mix;
    *r = *r * (1 - mix) + sr * mix;
  }
};

// Slicer (trance gate) locked to the sequencer grid: the fader sets depth
// first, then speed (1/8, 1/16, 1/32). The gate is smoothed (~1 ms) so it
// chops without clicks.
struct Slicer {
  float g = 1, amt = 0;
  float Gain(double steps, float amount) {
    amt += (amount - amt) * 0.002f;
    if (amt < 0.02f && g > 0.999f) { g = 1; return 1; }
    float rate = amt < 0.4f ? 2.0f : amt < 0.75f ? 1.0f : 0.5f;  // steps per slice
    double ph = steps / rate;
    ph -= floor(ph);
    float depth = std::min(1.0f, amt * 3.0f);
    float target = ph < 0.5 ? 1.0f : 1.0f - depth;
    g += (target - g) * 0.03f;
    return g;
  }
};

static Svf g_djf[2], g_djh[2];
static Slicer g_slicer;
static double g_clock = 0;  // 1/16 steps, follows the sequencer while it plays
static float g_wash = 0;
static SpaceEcho* g_space;
static Stutter* g_stutter;
static float g_fx_lp = 1, g_fx_hp = 0, g_fx_pan = 0.5f, g_fx_vol = 1, g_fx_space = 0;

static Chorus* g_chorus;
static SendFx* g_sfx[2];
static Tape* g_tapep = nullptr;
#define g_tape (*g_tapep)
static DubDelay* g_delay;
static clouds::ReverbF32 g_reverb;
static float g_reverb_buf[16384];
static float g_master_smooth = 0;
static float g_throw_smooth = 0;
static float g_pan_s[kTracks];

static float m_lad[kTracks], m_bus = 0, m_out = 0; static long m_hot = 0, m_tot = 0;

static float* g_usb_dst = nullptr;  // where RenderBlock puts the USB copy (audio thread)
static float g_usb_smooth = 0.5f;
static int g_kill = 0;  // 0 run, 1 fading out, 2 silent: clear, 3 fading in
static float g_kill_g = 1;
static void KillAll();

#include "sampler.h"  // FLUX part 2: tape, GRAB, slices, scrub
#include "mixtape.h"  // 0.9.22: MIXTAPE recorder, LISTEN, mixer levels
#include "padsampler.h"  // 0.9.27: every pad its own sound

// ---- RACK MASTER (0.9.27): a bus compressor and a limiter at the very end
struct MasterParams { bool mc, lim; float thr, ratio, ca, cr, mk, ceil, lr; };
static float g_mc_env = 0, g_lim_g = 1;
static std::atomic<float> g_mc_gr{1.0f}, g_lim_gr{1.0f};
static MasterParams MasterPrep() {
  MasterParams m;
  m.mc = G(G_MC_ON) > 0.5f;
  m.lim = G(G_LIM_ON) > 0.5f;
  m.thr = -30.0f + 30.0f * G(G_MC_THRESH);                    // dB
  m.ratio = 1.5f + 8.5f * G(G_MC_RATIO);
  m.ca = expf(-1.0f / ((1.0f + 99.0f * G(G_MC_ATTACK)) * 0.001f * kSR));
  m.cr = expf(-1.0f / ((30.0f + 970.0f * G(G_MC_RELEASE)) * 0.001f * kSR));
  m.mk = powf(10.0f, 12.0f * G(G_MC_MAKEUP) / 20.0f);
  m.ceil = powf(10.0f, (-6.0f + 6.0f * G(G_LIM_CEIL)) / 20.0f);
  m.lr = 1.0f / ((10.0f + 490.0f * G(G_LIM_RELEASE)) * 0.001f * kSR);
  return m;
}
static inline void MasterDyn(float* l, float* r, const MasterParams& m) {
  if (m.mc) {
    float pk = std::max(fabsf(*l), fabsf(*r));
    g_mc_env = pk > g_mc_env ? m.ca * g_mc_env + (1 - m.ca) * pk : m.cr * g_mc_env + (1 - m.cr) * pk;
    float db = 20.0f * log10f(std::max(1e-6f, g_mc_env)) - m.thr;
    float red = 0;  // 6 dB soft knee
    if (db > 3) red = db * (1 - 1 / m.ratio);
    else if (db > -3) red = (db + 3) * (db + 3) / 12.0f * (1 - 1 / m.ratio);
    float g = powf(10.0f, -red / 20.0f);
    if (g < g_mc_gr.load(std::memory_order_relaxed)) g_mc_gr.store(g, std::memory_order_relaxed);
    *l *= g * m.mk; *r *= g * m.mk;
  } else g_mc_env = 0;
  if (m.lim) {
    float pk = std::max(fabsf(*l), fabsf(*r));
    float want = pk > m.ceil ? m.ceil / pk : 1.0f;
    if (want < g_lim_g) g_lim_g = want; else g_lim_g = std::min(1.0f, g_lim_g + m.lr);
    if (g_lim_g < g_lim_gr.load(std::memory_order_relaxed)) g_lim_gr.store(g_lim_g, std::memory_order_relaxed);
    *l = std::max(-m.ceil, std::min(m.ceil, *l * g_lim_g));
    *r = std::max(-m.ceil, std::min(m.ceil, *r * g_lim_g));
  } else g_lim_g = 1;
}
static void MasterReport(int tick) {
  if (tick % 25 != 0 || (G(G_MC_ON) < 0.5f && G(G_LIM_ON) < 0.5f)) return;
  printf("MGR %.3f %.3f\n", g_mc_gr.exchange(1.0f), g_lim_gr.exchange(1.0f));
}

struct TrackBlock { plaits::Patch patch; float a, d, s, r; bool perc; };

// Render one Plaits block (12 frames) of the full chain into out (stereo).
static void RenderBlock(float* out) {
  float sc_l = 0, sc_r = 0;  // visualizer peaks this block
  const int spec_t = g_spec_track.load(std::memory_order_relaxed);
  float g_scope_tr[kBlock] = {0};  // the analysed track, this block
  SeqBlock();
  TrackBlock tb[kTracks];
  for (int t = 0; t < kTracks; ++t) {
    int engine = std::max(0, std::min(23, (int)lrintf(TP(t, T_ENGINE))));
    tb[t].perc = Percussive(engine);
    plaits::Patch& p = tb[t].patch;
    p = plaits::Patch();
    p.harmonics = TP(t, T_HARMONICS);
    p.timbre = TP(t, T_TIMBRE);
    p.morph = TP(t, T_MORPH);
    p.engine = engine;
    p.decay = TP(t, T_DECAY);
    p.lpg_colour = TP(t, T_LPG_COLOUR);
    tb[t].a = TimeParam(TP(t, T_ATTACK), 0.001f, 4.0f);
    tb[t].d = TimeParam(TP(t, T_DECAY2), 0.01f, 6.0f);
    tb[t].s = TP(t, T_SUSTAIN);
    tb[t].r = TimeParam(TP(t, T_RELEASE), 0.01f, 6.0f);
    memset(g_trk[t].buf, 0, sizeof(g_trk[t].buf));
  }

  float drift_amt = G(G_DRIFT);
  float softness = G(G_SOFTNESS);
  for (auto& v : g_voices) {
    if (v.note < 0) continue;
    TrackBlock& b = tb[v.track];
    if (b.perc) { if (v.tail > 0) --v.tail; } else v.tail = 0;
    float lvl = v.env.Process(b.a, b.d, b.s, b.r);
    if (v.Free()) continue;
    plaits::Modulations m = {};
    m.trigger_patched = true;
    bool high = b.perc ? v.perc_hold > 0 : v.gate;
    if (v.low > 0) { high = false; --v.low; }
    else if (v.perc_hold > 0) --v.perc_hold;
    m.trigger = high ? 1.0f : 0.0f;
    m.level_patched = !b.perc;
    m.level = lvl;
    v.drift = v.drift * 0.9995f + (Rand01() - 0.5f) * 0.02f;
    float cents = drift_amt * (v.detune * 6.0f + v.drift * 25.0f);
    plaits::Patch patch = b.patch;
    patch.note = (float)v.note + cents * 0.01f;
    v.voice.Render(patch, m, v.frames, kBlock);
    float g = (b.perc ? kPercGain : 1.0f) * (0.35f + 0.65f * v.vel) / 32768.0f;
    // softness: harder hits are brighter, soft hits darker (like a real instrument)
    float fc = 20000.0f * powf(0.12f + 0.88f * v.vel * v.vel, softness * 1.6f);
    float ka = 1.0f - expf(-6.2831853f * fc / kSR);
    float* buf = g_trk[v.track].buf;
    int peak = 0;
    for (size_t n = 0; n < kBlock; ++n) {
      v.lp += ka * (v.frames[n].out * g - v.lp);
      buf[n] += v.lp;
      peak = std::max(peak, abs((int)v.frames[n].out));
    }
    // a finished drum hit stops rendering (saves CPU, frees the voice)
    if (b.perc && !v.gate && v.perc_hold == 0) {
      v.quiet = peak < 4 ? v.quiet + 1 : 0;
      if (v.quiet > 200) v.tail = 0;
    }
  }

  // shared busses for this block
  float chl[kBlock] = {0}, chr[kBlock] = {0};  // melodic tracks -> chorus
  float pl[kBlock] = {0}, pr[kBlock] = {0};    // drum tracks (no chorus)
  float sub_bus[kBlock] = {0}, dly_in[kBlock] = {0}, sp_in[kBlock] = {0};
  float sbl[kBlock] = {0}, sbr[kBlock] = {0};  // 0.9.25: send B (A stays in dly_in, mono, as before)
  FloatFrame rv[kBlock];
  for (size_t n = 0; n < kBlock; ++n) rv[n].l = rv[n].r = 0;

  float cut_scale = G(G_FADER_CUTOFF);
  float fx_vol_t = G(G_FX_VOL);
  float vol_start = g_fx_vol;
  g_fx_vol += (fx_vol_t - g_fx_vol) * 0.035f;
  float vol2 = g_fx_vol * g_fx_vol;
  (void)vol_start;
  g_throw_smooth += ((G(G_THROW) > 0.5f ? 1.0f : 0.0f) - g_throw_smooth) * 0.035f;
  int ui = g_ui_track.load(std::memory_order_relaxed);

  for (int t = 0; t < kTracks; ++t) {
    Track& tr = g_trk[t];
    // wobble LFO per track (retriggered by each new note), cutoff once per block
    if (tr.retrig) { tr.lfo_phase = 0.25f; tr.retrig = false; }  // start at the top (open)
    float lfo_hz = 1.0f / DivSeconds(TP(t, T_WOBBLE_DIV));
    tr.lfo_phase += lfo_hz * kBlock / kSR;
    if (tr.lfo_phase >= 1) tr.lfo_phase -= 1;
    float lfo01 = 0.5f + 0.5f * sinf(6.2831853f * tr.lfo_phase);
    float depth = TP(t, T_WOBBLE_DEPTH);
    tr.cut_smooth += (TP(t, T_CUTOFF) * cut_scale - tr.cut_smooth) * 0.024f;
    tr.ladder.SetCutoff(25.0f * powf(720.0f, tr.cut_smooth) * powf(2.0f, -depth * 6.0f * (1.0f - lfo01)));
    float res = TP(t, T_RESONANCE), drive = TP(t, T_DRIVE);
    float level = TP(t, T_LEVEL) * kTrackGain * vol2;
    float dsend = TP(t, T_DELAY_SEND);
    if (t == ui) dsend = std::max(dsend, g_throw_smooth);
    float rsend = TP(t, T_REVERB_SEND);
    // 3-band EQ: low shelf 40-400 Hz, mid peak 200 Hz-5 kHz, high shelf 2-16 kHz
    float ev[6] = {TP(t, T_EQ_LOW), TP(t, T_EQ_MID), TP(t, T_EQ_HIGH), TP(t, T_EQ_MID_FREQ),
                   TP(t, T_EQ_LOW_FREQ), TP(t, T_EQ_HIGH_FREQ)};
    if (memcmp(ev, tr.eq_set, sizeof(ev)) != 0) {
      tr.eq[0].Set(0, 40.0f * powf(10.0f, ev[4]), EqDb(ev[0]), 0.707f);
      tr.eq[1].Set(1, 200.0f * powf(25.0f, ev[3]), EqDb(ev[1]), 0.9f);
      tr.eq[2].Set(2, 2000.0f * powf(8.0f, ev[5]), EqDb(ev[2]), 0.707f);
      bool on = fabsf(ev[0] - 0.5f) > 0.004f || fabsf(ev[1] - 0.5f) > 0.004f || fabsf(ev[2] - 0.5f) > 0.004f;
      if (on && !tr.eq_on) for (auto& q : tr.eq) q.z1 = q.z2 = 0;
      tr.eq_on = on;
      memcpy(tr.eq_set, ev, sizeof(ev));
    }
    g_pan_s[t] += (TP(t, T_PAN) - g_pan_s[t]) * 0.05f;
    float gl = 1.4142136f * cosf(g_pan_s[t] * 1.5707963f), gr = 1.4142136f * sinf(g_pan_s[t] * 1.5707963f);

    // sub oscillator follows the track's lowest held note
    float sub_level = TP(t, T_SUB_LEVEL);
    const float sub_lvl = TP(t, T_LEVEL) / 0.8f;  // the default LEVEL (0.8) keeps the old balance
    bool sub = sub_level > 0.001f || tr.sub_amp > 1e-5f;
    int sub_oct = TP(t, T_SUB_OCT) > 1.5f ? 2 : 1;
    float sub_target = tr.held_low >= 0 ? NoteHz((float)tr.held_low - 12.0f * sub_oct) : tr.sub_freq;
    float sub_amp_target = tr.held_low >= 0 ? sub_level : 0.0f;
    // the low EQ band acts on the sub too, so a low kill really kills
    float sub_eq_t = tr.eq_on ? powf(10.0f, EqDb(ev[0]) / 20.0f) : 1.0f;
    const bool scoped = t == spec_t;

    bool silent = true;
    for (size_t n = 0; n < kBlock; ++n) if (tr.buf[n] != 0) { silent = false; break; }
    if (silent && !sub && fabsf(tr.ladder.y_prev) < 1e-6f) continue;
    float* bl = tb[t].perc ? pl : chl;
    float* br = tb[t].perc ? pr : chr;
    float lpk = 0;  // this track's peak this block (mixer dots)
    for (size_t n = 0; n < kBlock; ++n) {
      m_lad[t] = std::max(m_lad[t], fabsf(tr.buf[n] * level * (1 + drive * 4)));
      float y = tr.ladder.Process(tr.buf[n] * level, res, drive);
      if (tr.eq_on) y = tr.eq[2].Run(tr.eq[1].Run(tr.eq[0].Run(y)));
      lpk = std::max(lpk, fabsf(y) * std::max(gl, gr));
      bl[n] += y * gl;
      br[n] += y * gr;
      dly_in[n] += y * dsend;
      sbl[n] += y * rsend * gl;
      sbr[n] += y * rsend * gr;
      sp_in[n] += y;
      if (sub) {
        tr.sub_freq += (sub_target - tr.sub_freq) * 0.002f;
        tr.sub_amp += (sub_amp_target - tr.sub_amp) * (sub_amp_target > tr.sub_amp ? 0.01f : 0.0008f);
        tr.sub_phase += tr.sub_freq / kSR;
        if (tr.sub_phase >= 1) tr.sub_phase -= 1;
        tr.sub_eq += (sub_eq_t - tr.sub_eq) * 0.003f;
        // 0.9.24: the sub follows the track's LEVEL too (it used to ignore the mixer fader)
        float sv = tanhf(1.6f * sinf(6.2831853f * tr.sub_phase)) * 0.8f * tr.sub_amp * vol2 * tr.sub_eq * sub_lvl;
        sub_bus[n] += sv;
        lpk = std::max(lpk, fabsf(sv));
        if (scoped) g_scope_tr[n] += sv;
      }
      if (scoped) g_scope_tr[n] += y;
    }
    LevelPeak(t, lpk * kBusGain);
  }

  float dly_s = DivSeconds(G(G_DELAY_DIV));
  float fb = G(G_DELAY_FEEDBACK) * 1.15f;
  float tone = G(G_DELAY_TONE);
  float master_target = G(G_MASTER);
  float chorus_amt = G(G_CHORUS);
  float beat_s = 60.0f / Bpm();
  float fx_lp_t = G(G_FX_LP), fx_hp_t = G(G_FX_HP), fx_pan_t = G(G_FX_PAN);
  float fx_space_t = G(G_FX_SPACE), fx_stutter = G(G_FX_STUTTER), fx_slicer = G(G_FX_SLICER), fx_wash = G(G_FX_WASH);
  double sps = Bpm() * 4.0 / 60.0 / kSR;  // steps per sample
  if (g_seq.playing) g_clock = g_seq.pos - sps * kBlock;
  g_fx_pan += (fx_pan_t - g_fx_pan) * 0.035f;
  float pan_l = 1.4142136f * cosf(g_fx_pan * 1.5707963f);
  float pan_r = 1.4142136f * sinf(g_fx_pan * 1.5707963f);

  // RACK routing (0.9.25): send A (the old dub send) and send B (the old
  // reverb send) each feed the effect picked for them.
  const int fxA = std::max(0, std::min(5, (int)lrintf(G(G_SEND_A_FX) * 5))), fxB = std::max(0, std::min(5, (int)lrintf(G(G_SEND_B_FX) * 5)));  // index / 5
  const float rp[6] = {G(G_RCH_RATE), G(G_RCH_DEPTH), G(G_RPH_RATE), G(G_RPH_FB), G(G_RCR_BITS), G(G_RCR_RATE)};
  float xfl[kBlock] = {0}, xfr[kBlock] = {0};  // chorus / phaser / crush returns
  static float s_throw[kBlock] = {0};          // MIXTAPE COMP + FX: last block's dry mix, into send A
  const float senda = g_mt_senda.load(std::memory_order_relaxed);
  if (senda > 0) for (size_t n = 0; n < kBlock; ++n) dly_in[n] += s_throw[n] * senda * 1.4142f;
  const float space_amt = G(G_SPACE_AMT);
  const bool space_sent = fxA == 2 || fxB == 2;
  for (size_t n = 0; n < kBlock; ++n) {
    float al = dly_in[n] * 0.7071f, ar = al;  // send A as stereo
    float din = 0, spn = 0;
    if (fxA == 0) din += dly_in[n]; else if (fxA == 1) { rv[n].l += al; rv[n].r += ar; } else if (fxA == 2) spn += dly_in[n];
    else { float a, b; g_sfx[0]->Process(fxA, al, ar, rp, &a, &b); xfl[n] += a; xfr[n] += b; }
    if (fxB == 0) din += 0.5f * (sbl[n] + sbr[n]); else if (fxB == 1) { rv[n].l += sbl[n]; rv[n].r += sbr[n]; } else if (fxB == 2) spn += 0.5f * (sbl[n] + sbr[n]);
    else { float a, b; g_sfx[1]->Process(fxB, sbl[n], sbr[n], rp, &a, &b); xfl[n] += a; xfr[n] += b; }
    dly_in[n] = din;
    sbl[n] = spn;  // reused: what goes to the space echo by send
  }
  for (size_t n = 0; n < kBlock; ++n) {
    float cl, cr;
    g_chorus->Process(chl[n], chr[n], chorus_amt, &cl, &cr);  // stereo width, BBD-style
    // performance pan (constant power, unity at centre); sub stays centred
    cl = cl + pl[n] + sub_bus[n];
    cr = cr + pr[n] + sub_bus[n];
    s_throw[n] = 0.5f * (cl + cr);
    float dl, dr;
    g_delay->Process(dly_in[n], dly_s, fb, tone, &dl, &dr);
    rv[n].l += dl * 0.25f;
    rv[n].r += dr * 0.25f;
    g_wash += (fx_wash - g_wash) * 0.0005f;  // reverb wash: the whole mix into a long reverb
    if (g_wash > 1e-4f) { rv[n].l += cl * g_wash * 0.6f; rv[n].r += cr * g_wash * 0.6f; }
    g_fx_space += (fx_space_t - g_fx_space) * 0.003f;
    float sl, sr;
    {  // the fader throws the whole mix in; a RACK send throws only what is sent
      float amt = space_sent ? std::max(g_fx_space, space_amt) : g_fx_space;
      float in = sp_in[n] * (amt > 1e-4f ? std::min(1.0f, g_fx_space / amt) : 0.0f);
      if (space_sent) in += sbl[n] * 2.0f;
      g_space->Process(in, amt, beat_s, &sl, &sr);
    }
    out[2 * n] = cl + dl + sl + xfl[n];
    out[2 * n + 1] = cr + dr + sr + xfr[n];
  }
  g_reverb.set_amount(1.0f);
  g_reverb.set_input_gain(0.3f);
  float rtime = 0.5f + 0.48f * G(G_REVERB_SIZE);
  g_reverb.set_time(rtime + (0.985f - rtime) * g_wash);
  g_reverb.set_diffusion(0.7f);
  g_reverb.set_lp(0.3f + 0.65f * G(G_REVERB_TONE));
  for (size_t n = 0; n < kBlock; ++n) Sec().rvin = std::max(Sec().rvin, std::max(fabsf(rv[n].l), fabsf(rv[n].r)));
  g_reverb.Process(rv, kBlock);
  g_tape.Set(G(G_TAPE_DRIVE), G(G_TAPE_AGE), G(G_HISS), G(G_CRACKLE), G(G_GLUE));
  // DJ filter coefficients once per block (smoothing per block is inaudible)
  g_fx_lp += (fx_lp_t - g_fx_lp) * 0.035f;
  g_fx_hp += (fx_hp_t - g_fx_hp) * 0.035f;
  // 0.7.3: both filters always run (switching them in and out popped);
  // fully open they sit at 20 kHz / 12 Hz.
  float usb_gain = powf(10.0f, -24.0f * (1.0f - G(G_USB_LEVEL)) / 20.0f);  // -24..0 dB
  float g_lp = Svf::G(40.0f * powf(500.0f, g_fx_lp));
  float g_hp = Svf::G(12.0f * powf(250.0f, g_fx_hp));
  MasterParams mcp = MasterPrep();
  float smp_l[kBlock] = {0}, smp_r[kBlock] = {0};
  SamplerRender(smp_l, smp_r);
  PadRender(smp_l, smp_r);
  {
    int ct = g_pad_track.load(std::memory_order_relaxed);
    if (ct < 0) ct = g_chop_track.load(std::memory_order_relaxed);
    float sp = 0;
    for (size_t n = 0; n < kBlock; ++n) sp = std::max(sp, std::max(fabsf(smp_l[n]), fabsf(smp_r[n])));
    if (ct >= 0 && ct < kTracks) LevelPeak(ct, sp);
  }
  for (size_t n = 0; n < kBlock; ++n) {
    float l = out[2 * n] + rv[n].l;
    float rr = out[2 * n + 1] + rv[n].r;
    l *= kBusGain;
    rr *= kBusGain;
    l += smp_l[n];  // slices and scrub join the mix here (so the fader fx and tape colour them)
    rr += smp_r[n];
    g_stutter->Process(&l, &rr, fx_stutter, beat_s);
    float sg = g_slicer.Gain(g_clock, fx_slicer);
    g_clock += sps;
    TapeBarCheck(g_clock, g_seq.playing);
    l *= sg;
    rr *= sg;
    m_bus = std::max(m_bus, std::max(fabsf(l), fabsf(rr)));
    g_tape.Process(&l, &rr);  // glue, tape saturation, head bump, age, noise
    {
      float a, b;
      g_djf[0].Run(l, g_lp, 1.1f, &a, &b); l = a;
      g_djf[1].Run(rr, g_lp, 1.1f, &a, &b); rr = a;
      g_djh[0].Run(l, g_hp, 0.9f, &a, &b); l = b;
      g_djh[1].Run(rr, g_hp, 0.9f, &a, &b); rr = b;
    }
    // performance PAN (fader): balance of the whole mix, effects included
    l *= pan_l;
    rr *= pan_r;
    MasterDyn(&l, &rr, mcp);  // 0.9.27 RACK MASTER: comp, then limiter
    // hard stop (KILL): fade everything out in 30 ms, clear, fade back in
    if (g_kill == 1) { g_kill_g -= 1.0f / 1440.0f; if (g_kill_g <= 0) { g_kill_g = 0; g_kill = 2; } }
    else if (g_kill == 3) { g_kill_g += 1.0f / 480.0f; if (g_kill_g >= 1) { g_kill_g = 1; g_kill = 0; } }
    l *= g_kill_g;
    rr *= g_kill_g;
    TapeWrite(l, rr);  // the rolling tape: what you hear, before MAIN VOLUME
    PadRecWrite(l, rr);  // SAMPLE IN: the mix onto a pad
    MixtapeWrite(&l, &rr);  // MIXTAPE: the same, to the card (when recording); MONITOR: hear it processed
    LevelPeak(8, std::max(fabsf(l), fabsf(rr)));
    MixtapePlay(&l, &rr);  // LISTEN: a take from the card (not recorded)
    if (g_usb_dst) {  // USB copy: before MAIN VOLUME, at its own level
      g_usb_smooth += (usb_gain - g_usb_smooth) * 0.002f;
      g_usb_dst[2 * n] = SafeClip(l * g_usb_smooth);
      g_usb_dst[2 * n + 1] = SafeClip(rr * g_usb_smooth);
    }
    if (spec_t >= 0) g_scope.Push(g_scope_tr[n], g_scope_tr[n]);
    else g_scope.Push(l, rr);
    sc_l = std::max(sc_l, fabsf(l));
    sc_r = std::max(sc_r, fabsf(rr));
    g_master_smooth += (master_target - g_master_smooth) * 0.002f;
    float g = g_master_smooth * g_master_smooth;
    l *= g;
    rr *= g;
    m_out = std::max(m_out, std::max(fabsf(l), fabsf(rr)));
    Sec().out = std::max(Sec().out, std::max(fabsf(l), fabsf(rr)));
    ++g_frames; ++m_tot; if (fabsf(l) > 0.7f) ++m_hot;
    out[2 * n] = SafeClip(l);
    out[2 * n + 1] = SafeClip(rr);
  }
  g_scope.Peaks(sc_l, sc_r);
  if (g_kill == 2) KillAll();
}


// ------------------------------------------------------------------ commands

// Everything silent: voices, echoes, reverb, stutter, filters (called while
// the output is faded to zero, so clearing cannot click).
static void KillAll() {
  for (auto& v : g_voices) { v.gate = false; v.note = -1; v.env.stage = Env::IDLE; v.env.v = 0; v.tail = 0; v.lp = 0; }
  for (auto& t : g_trk) {
    t.held_low = -1; t.sub_amp = 0;
    t.ladder = Ladder();
    for (auto& q : t.eq) q.z1 = q.z2 = 0;
  }
  g_delay->Clear();
  g_space->Clear();
  g_stutter->Clear();
  g_stutter->zone = -1; g_stutter->mix = 0; g_stutter->g[0] = g_stutter->g[1] = 0;
  g_reverb.Clear();
  for (auto& v : g_sv) v.on = false;
  g_scrub_gain = 0;
  g_tape.Reset();
  for (auto& f : g_djf) f = Svf();
  for (auto& f : g_djh) f = Svf();
  memset(g_chorus->bl, 0, sizeof(g_chorus->bl)); memset(g_chorus->br, 0, sizeof(g_chorus->br));
  for (auto* f : g_sfx) memset((void*)f, 0, sizeof(SendFx));  // all-zero is its start state
  g_kill = 3;
}

static bool ValidStep(int p, int t, int s) { return p >= 0 && p < kPatterns && t >= 0 && t < kTracks && s >= 0 && s < kSteps; }

static void Handle(const Cmd& c) {
  if (PadHandle(c)) return;
  if (SamplerHandle(c)) return;
  switch (c.op) {
    case OP_NOTE_ON: if (c.i[1] >= 0 && c.i[1] < kTracks) NoteOn(c.i[1], c.i[0], c.f[0]); break;
    case OP_NOTE_OFF: if (c.i[1] >= 0 && c.i[1] < kTracks) NoteOff(c.i[1], c.i[0]); break;
    case OP_ALL_OFF: AllOff(); break;
    case OP_CLEAR_FX: g_delay->Clear(); g_space->Clear(); break;
    case OP_STEP:
      if (ValidStep(c.i[0], c.i[1], c.i[2])) {
        Step& st = g_pat[c.i[0]].s[c.i[1]][c.i[2]];
        st.on = c.i[3] != 0;
        st.note = std::max(0, std::min(127, c.i[4]));
        st.vel = std::max(0.0f, std::min(1.0f, c.f[0]));
        st.gate = std::max(0.05f, std::min(16.0f, c.f[1]));
        st.micro = std::max(-0.45f, std::min(0.45f, c.f[2]));
      }
      break;
    case OP_LOCK:
      if (ValidStep(c.i[0], c.i[1], c.i[2]) && c.i[3] >= 0) SetLock(g_pat[c.i[0]].s[c.i[1]][c.i[2]], c.i[3], c.f[0], nullptr);
      break;
    case OP_LOCK_CLEAR:
      if (ValidStep(c.i[0], c.i[1], c.i[2])) {
        Step& st = g_pat[c.i[0]].s[c.i[1]][c.i[2]];
        for (int i = 0; i < kLocks; ++i) st.lp[i] = -1;
      }
      break;
    case OP_LEN:
      if (c.i[0] >= 0 && c.i[0] < kPatterns) {
        int len = std::max(1, std::min(kSteps, c.i[1]));
        g_pat[c.i[0]].len = len;
        // live length change: stay on the bar grid (counted from PLAY), so
        // going 32 -> 4 -> 32 lands back where the full groove would be
        if (g_seq.playing && c.i[0] == g_seq.enq_pat) g_seq.enq_step = (int)(g_seq.next_k % len);
      }
      break;
    case OP_CLR:
      if (c.i[0] >= 0 && c.i[0] < kPatterns)
        for (int t = 0; t < kTracks; ++t)
          if (c.i[1] < 0 || c.i[1] == t)
            for (int s = 0; s < kSteps; ++s) g_pat[c.i[0]].s[t][s] = Step();
      break;
    case OP_PAT:
      if (c.i[0] >= 0 && c.i[0] < kPatterns) {
        g_seq.sel_pat = c.i[0];
        if (g_seq.playing) g_seq.queued = c.i[0];
      }
      break;
    case OP_CHAIN:
      g_seq.chain_len = 0;
      for (int i = 0; i < c.i[9] && i < 8; ++i)
        if (c.i[i] >= 0 && c.i[i] < kPatterns) g_seq.chain[g_seq.chain_len++] = c.i[i];
      break;
    case OP_PLAY: SeqPlay(); break;
    case OP_STOP: SeqStop(); break;
    case OP_KILL: if (g_kill == 0) g_kill = 1; break;
    case OP_MUTE: if (c.i[0] >= 0 && c.i[0] < kTracks) g_trk[c.i[0]].muted = c.i[1] != 0; break;
    case OP_REC_ON:
      NoteOn(c.i[1], c.i[0], c.f[0]);
      RecNoteOn(c.i[1], c.i[0], c.f[0]);
      break;
    case OP_REC_OFF:
      NoteOff(c.i[1], c.i[0]);
      RecNoteOff(c.i[1], c.i[0]);
      break;
    case OP_REC_LOCK: RecLock(c.i[1], c.i[0], c.f[0]); break;
  }
}

// Parse one command line. Parameter writes are applied directly (atomics);
// everything else is returned in *c for the audio thread.
// Returns 0 = nothing to queue, 1 = queue *c, 2 = quit.
static int Parse(const char* line, Cmd* c) {
  char op[16] = {0}, name[64];
  int n = 0;
  if (sscanf(line, "%15s%n", op, &n) != 1) return 0;
  const char* a = line + n;
  memset(c, 0, sizeof(*c));
  int ui = g_ui_track.load();
  float v;
  int t;
  if (!strcmp(op, "Q")) return 2;
  {
    int pr = PadParse(op, a, c);
    if (pr >= 0) return pr;
    int sr = SamplerParse(op, a, c);
    if (sr >= 0) return sr;
    if (MixtapeParse(op, a) == 0) return 0;
  }
  if (!strcmp(op, "P")) {
    if (sscanf(a, "%63s %f", name, &v) != 2) return 0;
    int gi = FindG(name), ti = FindT(name);
    if (gi >= 0) g_gp[gi].store(v);
    else if (ti >= 0) g_tp[ui][ti].store(v);
    return 0;
  }
  if (!strcmp(op, "T")) {
    if (sscanf(a, "%d %63s %f", &t, name, &v) != 3 || t < 0 || t >= kTracks) return 0;
    int ti = FindT(name);
    if (ti >= 0) g_tp[t][ti].store(v);
    return 0;
  }
  if (!strcmp(op, "U")) {
    if (sscanf(a, "%d", &t) == 1 && t >= 0 && t < kTracks) g_ui_track.store(t);
    return 0;
  }
  c->i[1] = ui;
  if (!strcmp(op, "N")) { c->op = OP_NOTE_ON; return sscanf(a, "%d %f %d", &c->i[0], &c->f[0], &c->i[1]) >= 2; }
  if (!strcmp(op, "O")) { c->op = OP_NOTE_OFF; return sscanf(a, "%d %d", &c->i[0], &c->i[1]) >= 1; }
  if (!strcmp(op, "R")) { c->op = OP_REC_ON; return sscanf(a, "%d %f", &c->i[0], &c->f[0]) == 2; }
  if (!strcmp(op, "r")) { c->op = OP_REC_OFF; return sscanf(a, "%d", &c->i[0]) == 1; }
  if (!strcmp(op, "TL")) {
    if (sscanf(a, "%63s %f", name, &c->f[0]) != 2) return 0;
    c->op = OP_REC_LOCK;
    c->i[0] = FindT(name);
    return c->i[0] >= 0;
  }
  if (!strcmp(op, "A")) { c->op = OP_ALL_OFF; return 1; }
  if (!strcmp(op, "C")) { c->op = OP_CLEAR_FX; return 1; }
  if (!strcmp(op, "S")) {
    c->op = OP_STEP;
    return sscanf(a, "%d %d %d %d %d %f %f %f", &c->i[0], &c->i[1], &c->i[2], &c->i[3], &c->i[4], &c->f[0], &c->f[1], &c->f[2]) == 8;
  }
  if (!strcmp(op, "L")) {
    c->op = OP_LOCK;
    if (sscanf(a, "%d %d %d %63s %f", &c->i[0], &c->i[1], &c->i[2], name, &c->f[0]) != 5) return 0;
    c->i[3] = FindT(name);
    return c->i[3] >= 0;
  }
  if (!strcmp(op, "LX")) { c->op = OP_LOCK_CLEAR; return sscanf(a, "%d %d %d", &c->i[0], &c->i[1], &c->i[2]) == 3; }
  if (!strcmp(op, "LEN")) { c->op = OP_LEN; return sscanf(a, "%d %d", &c->i[0], &c->i[1]) == 2; }
  if (!strcmp(op, "CLR")) { c->op = OP_CLR; c->i[1] = -1; return sscanf(a, "%d %d", &c->i[0], &c->i[1]) >= 1; }
  if (!strcmp(op, "PAT")) { c->op = OP_PAT; return sscanf(a, "%d", &c->i[0]) == 1; }
  if (!strcmp(op, "CHAIN")) {  // up to 8 patterns (i[9] = count); no args = loop one
    c->op = OP_CHAIN;
    int k = sscanf(a, "%d %d %d %d %d %d %d %d", &c->i[0], &c->i[1], &c->i[2], &c->i[3], &c->i[4], &c->i[5], &c->i[6], &c->i[7]);
    c->i[9] = k > 0 ? k : 0;
    return 1;
  }
  if (!strcmp(op, "PLAY")) { c->op = OP_PLAY; return 1; }
  if (!strcmp(op, "STOP")) { c->op = OP_STOP; return 1; }
  if (!strcmp(op, "KILL")) { c->op = OP_KILL; return 1; }
  if (!strcmp(op, "SPEC")) {  // reader-side only: SPEC on [track]
    int on = 0, t = -1;
    sscanf(a, "%d %d", &on, &t);
    g_spec_on.store(on != 0);
    g_spec_track.store(t >= 0 && t < kTracks ? t : -1);
    return 3;
  }
  if (!strcmp(op, "M")) { c->op = OP_MUTE; return sscanf(a, "%d %d", &c->i[0], &c->i[1]) == 2; }
  return 0;
}

static void PrintReps() {
  Rep r;
  while (g_reps.Pop(&r)) {
    if (r.kind == 'R') printf("REC %d %d %d %d %.3f %.3f\n", r.pat, r.trk, r.step, r.a, r.x, r.y);
    else if (r.kind == 'G') printf("RECG %d %d %d %.3f\n", r.pat, r.trk, r.step, r.x);
    else if (r.kind == 'L') printf("RECL %d %d %d %s %.4f\n", r.pat, r.trk, r.step, kTNames[r.a], r.x);
  }
}

// ------------------------------------------------------------------ ALSA

// ------------------------------------------------------------------ ALSA

struct Pcm { int fd = -1; unsigned period = 0, buffer = 0; long xruns = 0; };

static snd_mask* Mask(snd_pcm_hw_params* p, int n) { return &p->masks[n - SNDRV_PCM_HW_PARAM_FIRST_MASK]; }
static snd_interval* Ival(snd_pcm_hw_params* p, int n) { return &p->intervals[n - SNDRV_PCM_HW_PARAM_FIRST_INTERVAL]; }
static void Any(snd_pcm_hw_params* p) {
  memset(p, 0, sizeof(*p));
  for (int n = SNDRV_PCM_HW_PARAM_FIRST_MASK; n <= SNDRV_PCM_HW_PARAM_LAST_MASK; ++n) memset(Mask(p, n), 0xff, sizeof(snd_mask));
  for (int n = SNDRV_PCM_HW_PARAM_FIRST_INTERVAL; n <= SNDRV_PCM_HW_PARAM_LAST_INTERVAL; ++n) { Ival(p, n)->min = 0; Ival(p, n)->max = ~0u; }
  p->rmask = ~0u; p->info = ~0u;
}
static void SetMask(snd_pcm_hw_params* p, int n, unsigned bit) { snd_mask* m = Mask(p, n); memset(m, 0, sizeof(*m)); m->bits[bit >> 5] = 1u << (bit & 31); }
static void SetInt(snd_pcm_hw_params* p, int n, unsigned v) { snd_interval* i = Ival(p, n); i->min = i->max = v; i->integer = 1; }

static bool PcmOpen(const char* dev, Pcm* pcm) {
  pcm->fd = open(dev, O_RDWR);
  if (pcm->fd < 0) { printf("ERR open %s: %s\n", dev, strerror(errno)); return false; }
  snd_pcm_hw_params hw;
  Any(&hw);
  SetMask(&hw, SNDRV_PCM_HW_PARAM_ACCESS, SNDRV_PCM_ACCESS_RW_INTERLEAVED);
  SetMask(&hw, SNDRV_PCM_HW_PARAM_FORMAT, SNDRV_PCM_FORMAT_S32_LE);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_CHANNELS, 2);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_RATE, 48000);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_PERIOD_SIZE, 240);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_PERIODS, 4);
  if (ioctl(pcm->fd, SNDRV_PCM_IOCTL_HW_PARAMS, &hw) < 0) { printf("ERR HW_PARAMS: %s\n", strerror(errno)); return false; }
  pcm->period = Ival(&hw, SNDRV_PCM_HW_PARAM_PERIOD_SIZE)->min;
  pcm->buffer = Ival(&hw, SNDRV_PCM_HW_PARAM_BUFFER_SIZE)->min;
  snd_pcm_sw_params sw;
  memset(&sw, 0, sizeof(sw));
  sw.avail_min = pcm->period;
  sw.xfer_align = 1;
  sw.start_threshold = pcm->period * 2;
  sw.stop_threshold = pcm->buffer;
  sw.proto = SNDRV_PCM_VERSION;
  if (ioctl(pcm->fd, SNDRV_PCM_IOCTL_SW_PARAMS, &sw) < 0) { printf("ERR SW_PARAMS: %s\n", strerror(errno)); return false; }
  if (ioctl(pcm->fd, SNDRV_PCM_IOCTL_PREPARE) < 0) { printf("ERR PREPARE: %s\n", strerror(errno)); return false; }
  return true;
}

static void PcmWrite(Pcm* pcm, const int32_t* buf, unsigned frames) {
  unsigned off = 0;
  while (off < frames) {
    snd_xferi x;
    x.result = 0;
    x.buf = (void*)(buf + off * 2);
    x.frames = frames - off;
    if (ioctl(pcm->fd, SNDRV_PCM_IOCTL_WRITEI_FRAMES, &x) < 0) {
      if (errno == EPIPE) { pcm->xruns++; ioctl(pcm->fd, SNDRV_PCM_IOCTL_PREPARE); continue; }
      if (errno == EINTR) continue;
      return;
    }
    off += x.result;
  }
}

// ------------------------------------------------------------------ threads

static std::atomic<long> g_render_max_us{0};
static std::atomic<int> g_active{0};
static Pcm g_pcm;

static long NowUs() { timespec t; clock_gettime(CLOCK_MONOTONIC, &t); return t.tv_sec * 1000000L + t.tv_nsec / 1000; }

static int CountActive() {
  int act = 0;
  for (auto& v : g_voices) if (!v.Free()) ++act;
  return act;
}

// ------------------------------------------------------------------ USB audio
// The MPC presents a USB audio interface (UAC2 gadget) to a connected
// computer. A separate thread copies FM-STEP's output to it so the computer
// can record. The audio thread only drops frames into a ring buffer; if the
// gadget is missing or the computer is not recording, nothing else happens.
// The two clocks (codec and USB host) drift a little, so the ring is kept
// about two periods deep and re-centred when it drifts (rare tiny glitch in
// the USB copy only, never in the main output).
static const unsigned kUsbRing = 1u << 15;  // frames (0.68 s)
static float g_usb_ring[kUsbRing * 2];
static std::atomic<unsigned> g_usb_w{0}, g_usb_r{0};
static std::atomic<bool> g_usb_on{false};
static std::atomic<long> g_usb_frames{0}, g_usb_skips{0}, g_usb_xruns{0};
static char g_usb_info[200] = "starting";

static void UsbPush(const float* f, unsigned n) {  // audio thread
  if (!g_usb_on.load(std::memory_order_relaxed)) return;
  unsigned w = g_usb_w.load(std::memory_order_relaxed), r = g_usb_r.load(std::memory_order_acquire);
  if (kUsbRing - (w - r) < n) return;  // computer not taking audio: drop
  for (unsigned i = 0; i < n; ++i) {
    unsigned k = (w + i) & (kUsbRing - 1);
    g_usb_ring[2 * k] = f[2 * i];
    g_usb_ring[2 * k + 1] = f[2 * i + 1];
  }
  g_usb_w.store(w + n, std::memory_order_release);
}

static int FindUac2Card() {
  FILE* f = fopen("/proc/asound/cards", "r");
  if (!f) return -1;
  char line[256];
  int card = -1;
  while (fgets(line, sizeof(line), f))
    if (strstr(line, "UAC2") && sscanf(line, " %d", &card) == 1) break;
  fclose(f);
  return card;
}

static std::atomic<bool> g_quit{false};

static void* UsbThread(void*) {
  int card = FindUac2Card();
  if (card < 0) { snprintf(g_usb_info, sizeof(g_usb_info), "off(no-uac2-card)"); return nullptr; }
  char dev[64];
  snprintf(dev, sizeof(dev), "/dev/snd/pcmC%dD0p", card);
  int fd = open(dev, O_RDWR | O_NONBLOCK);
  if (fd < 0) { snprintf(g_usb_info, sizeof(g_usb_info), "off(open-%s:%s)", dev, strerror(errno)); return nullptr; }
  snd_pcm_hw_params hw;
  Any(&hw);
  if (ioctl(fd, SNDRV_PCM_IOCTL_HW_REFINE, &hw) < 0) { snprintf(g_usb_info, sizeof(g_usb_info), "off(refine:%s)", strerror(errno)); close(fd); return nullptr; }
  const int fmts[] = {SNDRV_PCM_FORMAT_S32_LE, SNDRV_PCM_FORMAT_S24_LE, SNDRV_PCM_FORMAT_S24_3LE, SNDRV_PCM_FORMAT_S16_LE};
  const int bps_of[] = {4, 4, 3, 2};
  int fi = -1;
  snd_mask* fm = Mask(&hw, SNDRV_PCM_HW_PARAM_FORMAT);
  for (int i = 0; i < 4 && fi < 0; ++i) if (fm->bits[fmts[i] >> 5] & (1u << (fmts[i] & 31))) fi = i;
  snd_interval* ci = Ival(&hw, SNDRV_PCM_HW_PARAM_CHANNELS);
  snd_interval* ri = Ival(&hw, SNDRV_PCM_HW_PARAM_RATE);
  unsigned chans = std::max(2u, ci->min);
  if (ci->max < 2) chans = ci->min;
  // the MPC's gadget runs at 44.1 kHz: we resample 48k -> its rate below
  unsigned rate = (ri->min <= 48000 && ri->max >= 48000) ? 48000 : ri->min;
  if (fi < 0 || rate < 8000 || rate > 192000) {
    snprintf(g_usb_info, sizeof(g_usb_info), "off(unsupported fmt=%d ch=%u-%u rate=%u-%u)", fi, ci->min, ci->max, ri->min, ri->max);
    close(fd); return nullptr;
  }
  Any(&hw);
  SetMask(&hw, SNDRV_PCM_HW_PARAM_ACCESS, SNDRV_PCM_ACCESS_RW_INTERLEAVED);
  SetMask(&hw, SNDRV_PCM_HW_PARAM_FORMAT, fmts[fi]);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_CHANNELS, chans);
  SetInt(&hw, SNDRV_PCM_HW_PARAM_RATE, rate);
  if (ioctl(fd, SNDRV_PCM_IOCTL_HW_PARAMS, &hw) < 0) { snprintf(g_usb_info, sizeof(g_usb_info), "off(hw_params:%s)", strerror(errno)); close(fd); return nullptr; }
  unsigned period = Ival(&hw, SNDRV_PCM_HW_PARAM_PERIOD_SIZE)->min;
  unsigned buffer = Ival(&hw, SNDRV_PCM_HW_PARAM_BUFFER_SIZE)->min;
  period = std::max(48u, std::min(period, 2048u));
  snd_pcm_sw_params sw;
  memset(&sw, 0, sizeof(sw));
  sw.avail_min = period;
  sw.xfer_align = 1;
  sw.start_threshold = std::min(buffer, period * 2);
  sw.stop_threshold = buffer;
  sw.proto = SNDRV_PCM_VERSION;
  ioctl(fd, SNDRV_PCM_IOCTL_SW_PARAMS, &sw);
  ioctl(fd, SNDRV_PCM_IOCTL_PREPARE);
  int bps = bps_of[fi];
  snprintf(g_usb_info, sizeof(g_usb_info), "on(card%d,fmt=%d,ch=%u,rate=%u,period=%u,buffer=%u)", card, fmts[fi], chans, rate, period, buffer);
  unsigned char* out = new unsigned char[period * chans * 4];
  int fails = 0;
  UsbResampler rs;
  rs.Init(48000.0 / rate, period);
  float* lr = new float[period * 2];
  g_usb_on.store(true);
  while (!g_quit.load()) {
    pollfd p = {fd, POLLOUT, 0};
    if (poll(&p, 1, 100) <= 0) {  // computer not recording: keep the ring fresh
      g_usb_r.store(g_usb_w.load(std::memory_order_acquire), std::memory_order_release);
      rs.started = false;
      continue;
    }
    // Resample from the ring (48 kHz, codec clock) to the gadget rate (USB
    // host clock) with cubic interpolation. The step is nudged by how full
    // the ring is, so the two clocks never drift apart.
    unsigned w = g_usb_w.load(std::memory_order_acquire);
    int got = rs.Pull(g_usb_ring, kUsbRing, w, g_usb_r.load(std::memory_order_relaxed), period, lr);
    if (got < 0) { usleep(1500); continue; }
    if (got == 2) g_usb_skips++;
    memset(out, 0, period * chans * bps);
    for (unsigned i = 0; i < period; ++i) {
      for (int ch = 0; ch < 2 && ch < (int)chans; ++ch) {
        float x = std::max(-1.0f, std::min(1.0f, lr[2 * i + ch]));
        unsigned char* d = out + (i * chans + ch) * bps;
        if (bps == 2) { int16_t v = (int16_t)lrintf(x * 32767.0f); memcpy(d, &v, 2); }
        else if (bps == 3) { int32_t v = (int32_t)lrintf(x * 8388607.0f); d[0] = v & 255; d[1] = (v >> 8) & 255; d[2] = (v >> 16) & 255; }
        else if (fmts[fi] == SNDRV_PCM_FORMAT_S24_LE) { int32_t v = (int32_t)lrintf(x * 8388607.0f); memcpy(d, &v, 4); }
        else { int32_t v = (int32_t)(x * 2147483000.0f); memcpy(d, &v, 4); }
      }
    }
    g_usb_r.store(rs.Consumed(), std::memory_order_release);

    unsigned done = 0;
    while (done < period && !g_quit.load()) {
      snd_xferi x;
      x.result = 0;
      x.buf = out + done * chans * bps;
      x.frames = period - done;
      if (ioctl(fd, SNDRV_PCM_IOCTL_WRITEI_FRAMES, &x) < 0) {
        if (errno == EPIPE) {
          g_usb_xruns++;
          ioctl(fd, SNDRV_PCM_IOCTL_PREPARE);
          if (++fails > 200) {  // the gadget keeps failing: give up quietly
            snprintf(g_usb_info, sizeof(g_usb_info), "off(too-many-xruns)");
            g_usb_on.store(false);
            done = period;
            break;
          }
          usleep(2000);
          continue;
        }
        if (errno == EAGAIN || errno == EINTR) { pollfd q = {fd, POLLOUT, 0}; poll(&q, 1, 20); continue; }
        snprintf(g_usb_info, sizeof(g_usb_info), "off(write:%s)", strerror(errno));
        g_usb_on.store(false);
        done = period;
        break;
      }
      done += x.result;
      g_usb_frames += x.result;
      fails = 0;
    }
    if (!g_usb_on.load()) break;
  }
  g_usb_on.store(false);
  ioctl(fd, SNDRV_PCM_IOCTL_DROP);
  close(fd);
  delete[] out;
  delete[] lr;
  return nullptr;
}

static void* AudioThread(void*) {
  sched_param sp; sp.sched_priority = 60;
  pthread_setschedparam(pthread_self(), SCHED_FIFO, &sp);
  const unsigned frames = g_pcm.period;
  float* f = new float[frames * 2];
  float* ub = new float[frames * 2];
  int32_t* pcm = new int32_t[frames * 2];
  while (!g_quit.load()) {
    long t0 = NowUs();
    Cmd c;
    while (g_cmds.Pop(&c)) Handle(c);
    while (g_grab_swaps.Pop(&c)) Handle(c);
    for (unsigned off = 0; off + kBlock <= frames; off += kBlock) {
      g_usb_dst = ub + off * 2;
      RenderBlock(f + off * 2);
    }
    g_usb_dst = nullptr;
    for (unsigned i = 0; i < frames * 2; ++i) {
      float x = std::max(-1.0f, std::min(1.0f, f[i]));
      pcm[i] = (int32_t)(x * 2147483000.0f);
    }
    long us = NowUs() - t0;
    if (us > g_render_max_us.load()) g_render_max_us.store(us);
    g_active.store(CountActive());
    PcmWrite(&g_pcm, pcm, frames);
    UsbPush(ub, frames);
  }
  // fade out over ~100 ms, then drop the stream
  g_gp[G_MASTER].store(0);
  for (int k = 0; k < 20; ++k) {
    for (unsigned off = 0; off + kBlock <= frames; off += kBlock) RenderBlock(f + off * 2);
    for (unsigned i = 0; i < frames * 2; ++i) pcm[i] = (int32_t)(std::max(-1.0f, std::min(1.0f, f[i])) * 2147483000.0f);
    PcmWrite(&g_pcm, pcm, frames);
  }
  ioctl(g_pcm.fd, SNDRV_PCM_IOCTL_DROP);
  close(g_pcm.fd);
  return nullptr;
}

// stdout: playhead (when it moves), recorded data, and stats every 10 s.
static void* OutThread(void*) {
  int last_pos = -2, spec_tick = 0, mt_tick = 0;
  long last_stats = NowUs();
  while (!g_quit.load()) {
    usleep(4000);
    int p = g_pos.load();
    if (p != last_pos) {
      if (p < 0) printf("POS -1 -1\n");
      else printf("POS %d %d\n", p / 64, p % 64);
      last_pos = p;
    }
    PrintReps();
    MixtapeReport(++mt_tick);
    PadReport(mt_tick);
    MasterReport(mt_tick);
    if (spec_tick % 10 == 5) SamplerTapeReport();
    { Cmd gc; if (SamplerPoll(&gc)) while (!g_grab_swaps.Push(gc)) usleep(1000); }
    if (++spec_tick >= 10 && g_spec_on.load()) {  // ~25 per second
      spec_tick = 0;
      float bands[kScopeBands];
      g_scope.Spectrum(bands, 48000.0f);
      char hex[2 * kScopeBands + 1];
      for (int b = 0; b < kScopeBands; ++b) {
        int v = (int)lrintf((bands[b] + 90.0f) * (255.0f / 90.0f));
        v = v < 0 ? 0 : v > 255 ? 255 : v;
        snprintf(hex + 2 * b, 3, "%02x", v);
      }
      printf("SPEC %.3f %.3f %s\n", g_scope.peak_l.exchange(0), g_scope.peak_r.exchange(0), hex);
    }
    if (NowUs() - last_stats > 10000000L) {
      last_stats = NowUs();
      printf("STATS render_max_us=%ld budget_us=%u xruns=%ld voices=%d usb=%s usb_frames=%ld usb_skips=%ld usb_xruns=%ld\n",
             g_render_max_us.exchange(0), g_pcm.period * 1000000u / 48000u, g_pcm.xruns, g_active.load(),
             g_usb_info, g_usb_frames.load(), g_usb_skips.load(), g_usb_xruns.load());
    }
    fflush(stdout);
  }
  return nullptr;
}

// Offline mode for auditioning and benchmarking without the MPC:
//   fmstep-dsp --render out.wav script.txt
// Script lines are the normal commands, plus "W <seconds>" to render time.
static int RenderOffline(const char* wav, const char* script) {
  FILE* in = fopen(script, "r");
  FILE* out = fopen(wav, "wb");
  if (!in || !out) { perror("open"); return 1; }
  unsigned char hdr[44] = {0};
  fwrite(hdr, 1, 44, out);
  unsigned long frames_total = 0;
  long worst = 0;
  double total_us = 0;
  long blocks = 0;
  int max_voices = 0;
  char line[256];
  float f[kBlock * 2];
  while (fgets(line, sizeof(line), in)) {
    if (line[0] == 'W' && line[1] == ' ') {
      float v;
      if (sscanf(line + 1, "%f", &v) != 1) continue;
      long n = (long)(v * kSR / kBlock);
      for (long b = 0; b < n; ++b) {
        { Cmd gc; if (SamplerPoll(&gc)) Handle(gc); }
        // offline runs faster than real time: let the MIXTAPE threads keep up
        while (g_mt_on.load() && g_mt_w.load() - g_mt_r.load() > kMtRing / 2) usleep(1000);
        while (g_mp_on.load() && !g_mp_eof.load() && g_mp_w.load() - g_mp_r.load() < 4096) usleep(500);
        if (b % 400 == 0) MixtapeReport(0);
        long t0 = NowUs();
        RenderBlock(f);
        long us = NowUs() - t0;
        total_us += us; ++blocks;
        if (us > worst) worst = us;
        max_voices = std::max(max_voices, CountActive());
        int16_t s16[kBlock * 2];
        for (size_t i = 0; i < kBlock * 2; ++i) s16[i] = (int16_t)lrintf(std::max(-1.0f, std::min(1.0f, f[i])) * 32767.0f);
        fwrite(s16, 2, kBlock * 2, out);
        frames_total += kBlock;
      }
      PrintReps();
      continue;
    }
    Cmd c;
    int r = Parse(line, &c);
    if (r == 1) Handle(c);
    if (r == 2) break;
  }
  g_mt_on.store(0);
  while (g_mt_busy.load()) usleep(1000);
  unsigned data = frames_total * 4;
  auto put32 = [&](int o, unsigned x) { for (int i = 0; i < 4; ++i) hdr[o + i] = (x >> (8 * i)) & 255; };
  auto put16 = [&](int o, unsigned x) { hdr[o] = x & 255; hdr[o + 1] = (x >> 8) & 255; };
  memcpy(hdr, "RIFF", 4); put32(4, 36 + data); memcpy(hdr + 8, "WAVEfmt ", 8);
  put32(16, 16); put16(20, 1); put16(22, 2); put32(24, 48000); put32(28, 48000 * 4); put16(32, 4); put16(34, 16);
  memcpy(hdr + 36, "data", 4); put32(40, data);
  fseek(out, 0, SEEK_SET); fwrite(hdr, 1, 44, out); fclose(out);
  if (getenv("FMSTEP_METER")) {
    printf("METER ladder-in peak per track (>1 = tanh saturating):");
    for (int t = 0; t < kTracks; ++t) printf(" %.2f", m_lad[t]);
    printf("\nMETER mix bus peak %.2f | glue GR max %.1f dB avg %.1f dB | tape input peak %.2f | pre-clip peak %.2f, %.1f%% samples in limiter\n",
           m_bus, g_tape.m_gr, g_tape.m_gr_sum / std::max(1L, g_tape.m_n), g_tape.m_sat, m_out, 100.0 * m_hot / std::max(1L, m_tot));
  }
  if (getenv("FMSTEP_METER"))
    for (long i = 0; i <= std::min(119L, g_frames / 48000); ++i)
      printf("SEC %3ld  delay-in %.2f  reverb-in %.3f  tape-in %.2f  out %.2f\n", i, g_sec[i].dly, g_sec[i].rvin, g_sec[i].tape, g_sec[i].out);
  printf("RENDERED %.1f s; per 12-frame block: avg %.1f us, worst %ld us (real time = 250 us); max voices %d\n",
         frames_total / kSR, total_us / std::max(1L, blocks), worst, max_voices);
  return 0;
}

static void InitEngine() {
  DefaultParams();
  for (auto& v : g_voices) v.Init();
  for (auto& t : g_trk) { memset(t.lock_on, 0, sizeof(t.lock_on)); }
  for (int t = 0; t < kTracks; ++t) g_pan_s[t] = 0.5f;
  g_pat = new Pattern[kPatterns];
  g_chorus = new Chorus();
  g_sfx[0] = new SendFx(); g_sfx[1] = new SendFx();
  g_delay = new DubDelay();
  g_delay->Clear();
  g_tapep = new Tape();
  g_space = new SpaceEcho(); g_space->Clear();
  g_stutter = new Stutter(); g_stutter->Clear();
  g_reverb.Init(g_reverb_buf);
  SamplerInit();
  PadInit();
  MtFxInit();
}

int main(int argc, char** argv) {
  setvbuf(stdout, nullptr, _IOLBF, 0);
  InitEngine();
  if (argc > 3 && !strcmp(argv[1], "--render")) {
    g_gp[G_MASTER].store(0.8f);
    g_trace = getenv("FMSTEP_TRACE") != nullptr;
    if (const char* tune = getenv("FMSTEP_TUNE")) {  // e.g. track=0.6,bus=0.5
      struct { const char* n; float* p; } kv[] = {{"track", &kTrackGain}, {"perc", &kPercGain}, {"bus", &kBusGain},
        {"tapek", &kTapeK}, {"thr", &kGlueThr}, {"atk", &kGlueAtk}, {"rel", &kGlueRel}, {"boost", &kGlueBoost}, {"out", &kOutGain}};
      char buf[256]; snprintf(buf, sizeof(buf), "%s", tune);
      for (char* tok = strtok(buf, ","); tok; tok = strtok(nullptr, ",")) {
        char* eq = strchr(tok, '='); if (!eq) continue; *eq = 0;
        for (auto& x : kv) if (!strcmp(tok, x.n)) *x.p = strtof(eq + 1, nullptr);
      }
    }
    return RenderOffline(argv[2], argv[3]);
  }
  const char* dev = argc > 1 ? argv[1] : "/dev/snd/pcmC2D0p";
  if (!PcmOpen(dev, &g_pcm)) return 2;
  printf("READY period=%u buffer=%u voices=%d engines=24 tracks=%d patterns=%d steps=%d\n",
         g_pcm.period, g_pcm.buffer, kMaxVoices, kTracks, kPatterns, kSteps);
  pthread_t at, ot, ut;
  pthread_create(&at, nullptr, AudioThread, nullptr);
  pthread_create(&ot, nullptr, OutThread, nullptr);
  bool usb = getenv("FMSTEP_NO_USB_AUDIO") == nullptr;
  if (usb) pthread_create(&ut, nullptr, UsbThread, nullptr);

  char line[256];
  while (fgets(line, sizeof(line), stdin)) {
    Cmd c;
    int r = Parse(line, &c);
    if (r == 2) break;
    if (r == 1) while (!g_cmds.Push(c)) usleep(1000);
  }
  // stdin closed or Q: the UI is exiting (or died) -> fade out and stop.
  g_quit.store(true);
  pthread_join(at, nullptr);
  pthread_join(ot, nullptr);
  if (usb) pthread_join(ut, nullptr);
  printf("EXIT xruns=%ld usb=%s usb_frames=%ld\n", g_pcm.xruns, g_usb_info, g_usb_frames.load());
  return 0;
}
