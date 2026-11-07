// PADS (0.9.27): every pad its own sound. Original FM-STEP code (GPL-3.0-or-later).
// Included by fmstep_dsp.cc after mixtape.h.
//
// 128 slots = 8 banks x 16 pads. Each slot holds its own sound (int16
// stereo, 48 kHz, up to 20 s), loaded from a WAV on the card by a loader
// thread (never the audio thread), or recorded from the mix (SAMPLE IN).
// 16 pad voices play them with the pad's own trim, ADSR, mode, tune, level,
// pan and reverse.
//
// Commands:
//   PLOAD <slot> <start_s> <len_s> <path>   load part of a WAV (len 0 = to the end, max 20 s)
//   PCLR <slot>                             empty the slot
//   PSET <slot> <in> <out> <a_ms> <d_ms> <s> <r_ms> <mode> <pitch> <fine> <level> <pan> <rev>
//                                           in / out 0..1 of the sound; mode 0 one-shot, 1 gate, 2 loop
//   PN <slot> <vel> · PO <slot>             play / let go
//   PTRK <t|-1>                             track t's notes play slots (note n = slot n)
//   PREC <slot> · PRECSTOP <path>           record the mix onto a slot (SAMPLE IN); the stop writes it to path
//   PX                                      all pad voices off
// Replies:
//   PLOADED <slot> <frames> · PLOADERR <slot> <why> <path> · PRECDONE <slot> <frames> <path> · PRECLEN <frames>

#include <condition_variable>
#include <deque>
#include <thread>

static const int kSlots = 128, kPadVoices = 16;
static const long kPadMaxFrames = 20L * 48000;

struct PadSet {
  float in = 0, out = 1, a = 0.0015f, d = 0, s = 1, r = 0.005f;
  int mode = 0;
  float pitch = 0, fine = 0, level = 1, pan = 0.5f;
  int rev = 0;
};
static PadSet g_pset[kSlots];  // written by the command thread, read by audio (floats: a torn read is harmless)

struct PadBuf { int16_t* d = nullptr; long n = 0; };
static PadBuf g_pbuf[kSlots];                       // audio thread's view
static std::atomic<int16_t*> g_ppend_d[kSlots];     // loader -> audio
static std::atomic<long> g_ppend_n[kSlots];
static std::atomic<int> g_ppend[kSlots];            // 1 = a new buffer is waiting
static std::atomic<int16_t*> g_pgarbage[kSlots];    // (unused: see g_gring)
static std::atomic<int16_t*> g_gring[256];          // audio -> out thread: old buffers to free

struct PadVoice {
  bool on = false, held = false;
  int slot = -1, mode = 0;
  double pos = 0;
  float env = 0, rel = 0, vel = 1;
  long t = 0;
  unsigned age = 0;
};
static PadVoice g_pv[kPadVoices];
static unsigned g_pv_age = 0;

// SAMPLE IN: record the mix onto a slot
static int16_t* g_prec = nullptr;                   // [kPadMaxFrames * 2]
static std::atomic<long> g_prec_n{0};
static std::atomic<int> g_prec_on{0};
static int g_prec_slot = -1;

enum { OP_PN = 200, OP_PO, OP_PX };

// ---- loader thread
struct PadJob { int slot; double start, len; std::string path; };
// never destroyed: the loader thread waits on them until the process ends
static std::mutex& g_pjob_mu = *new std::mutex;
static std::condition_variable& g_pjob_cv = *new std::condition_variable;
static std::deque<PadJob>& g_pjobs = *new std::deque<PadJob>;

static void PadInstall(int slot, int16_t* d, long n) {  // any thread but audio
  g_ppend_n[slot].store(n);
  int16_t* old = g_ppend_d[slot].exchange(d);
  if (g_ppend[slot].exchange(1, std::memory_order_release) == 1 && old) free(old);  // a waiting one that never got used
}

static void PadLoaderMain() {
  for (;;) {
    PadJob j;
    {
      std::unique_lock<std::mutex> lk(g_pjob_mu);
      g_pjob_cv.wait(lk, [] { return !g_pjobs.empty(); });
      j = g_pjobs.front();
      g_pjobs.pop_front();
    }
    std::vector<float> lr;
    char err[96] = "";
    long want = j.len > 0 ? std::min(kPadMaxFrames, (long)(j.len * kSR)) : kPadMaxFrames;
    if (!ReadWav(j.path.c_str(), &lr, err, sizeof(err), j.start, want)) {
      printf("PLOADERR %d %s %s\n", j.slot, err, j.path.c_str());
      fflush(stdout);
      continue;
    }
    long n = (long)lr.size() / 2;
    int16_t* d = (int16_t*)malloc(n * 2 * sizeof(int16_t));
    if (!d) { printf("PLOADERR %d memory %s\n", j.slot, j.path.c_str()); fflush(stdout); continue; }
    for (long i = 0; i < n * 2; ++i) d[i] = (int16_t)lrintf(std::max(-1.0f, std::min(1.0f, lr[i])) * 32767.0f);
    PadInstall(j.slot, d, n);
    printf("PLOADED %d %ld\n", j.slot, n);
    fflush(stdout);
  }
}

static void PadInit() {
  for (int i = 0; i < kSlots; ++i) { g_ppend_d[i].store(nullptr); g_ppend_n[i].store(0); g_ppend[i].store(0); g_pgarbage[i].store(nullptr); }
  for (auto& g : g_gring) g.store(nullptr);
  g_prec = (int16_t*)calloc(kPadMaxFrames * 2, sizeof(int16_t));
  std::thread(PadLoaderMain).detach();
}

// ---- audio thread
static void PadSwapIn() {  // block start: take new buffers
  for (int i = 0; i < kSlots; ++i) {
    if (g_ppend[i].load(std::memory_order_acquire) != 1) continue;
    int16_t* d = g_ppend_d[i].exchange(nullptr);
    long n = g_ppend_n[i].load();
    g_ppend[i].store(0);
    for (auto& v : g_pv) if (v.on && v.slot == i) v.on = false;
    int16_t* old = g_pbuf[i].d;
    g_pbuf[i].d = d; g_pbuf[i].n = d ? n : 0;
    if (old) {  // never free on the audio thread: the out thread sweeps the ring
      for (auto& g : g_gring) { int16_t* e = nullptr; if (g.compare_exchange_strong(e, old)) { old = nullptr; break; } }
    }
  }
}

static inline void PadRelease(PadVoice& v, float sec) {
  if (v.rel > 0) return;
  v.rel = std::max(1e-6f, std::max(v.env, 0.001f) / (std::max(0.003f, sec) * kSR));
}

static void PadOn(int slot, float vel) {
  if (slot < 0 || slot >= kSlots || !g_pbuf[slot].d || g_pbuf[slot].n < 64) return;
  for (auto& v : g_pv) if (v.on && v.slot == slot) PadRelease(v, 0.005f);  // choke
  PadVoice* best = nullptr;
  for (auto& v : g_pv) if (!v.on) { best = &v; break; }
  if (!best) for (auto& v : g_pv) if (v.rel > 0 && (!best || v.env < best->env)) best = &v;
  if (!best) { best = &g_pv[0]; for (auto& v : g_pv) if (v.age < best->age) best = &v; }
  PadVoice& v = *best;
  v = PadVoice();
  v.on = v.held = true;
  v.slot = slot;
  v.mode = std::max(0, std::min(2, g_pset[slot].mode));
  v.vel = std::max(0.05f, std::min(1.0f, vel));
  v.age = ++g_pv_age;
}

static void PadOff(int slot) {
  for (auto& v : g_pv) if (v.on && v.slot == slot && v.held) {
    v.held = false;
    if (v.mode >= 1) PadRelease(v, g_pset[slot].r);
  }
}

static const float kPadGain = 0.5f;

static void PadRender(float (*tl)[kBlock], float (*tr)[kBlock], const bool* used);

// 0.9.28: each pad voice plays through its sample track (8 + bank): the
// track's level (and the fader FX volume), pan and the two sends.
static void PadRenderTracks(float* pl, float* pr, float* dly, float* sbl, float* sbr, float vol2) {
  PadSwapIn();
  float tl[kTracks - kSynthTracks][kBlock], tr[kTracks - kSynthTracks][kBlock];
  bool used[kTracks - kSynthTracks] = {false};
  for (auto& v : g_pv) {
    if (!v.on) continue;
    int bt = v.slot / 16;
    if (!used[bt]) { memset(tl[bt], 0, sizeof(tl[bt])); memset(tr[bt], 0, sizeof(tr[bt])); used[bt] = true; }
  }
  PadRender(tl, tr, used);
  for (int b = 0; b < kTracks - kSynthTracks; ++b) {
    if (!used[b]) continue;
    int t = kSynthTracks + b;
    float level = TP(t, T_LEVEL) / 0.8f * vol2 * (kPadGain / kBusGain);
    float pan = std::max(0.0f, std::min(1.0f, TP(t, T_PAN)));
    float gl = 1.4142136f * cosf(pan * 1.5707963f), gr = 1.4142136f * sinf(pan * 1.5707963f);
    float ds = TP(t, T_DELAY_SEND), rs = TP(t, T_REVERB_SEND);
    float pk = 0;
    for (size_t n = 0; n < kBlock; ++n) {
      float l = tl[b][n] * level * gl, r = tr[b][n] * level * gr;
      pl[n] += l; pr[n] += r;
      dly[n] += 0.5f * (l + r) * ds;
      sbl[n] += l * rs; sbr[n] += r * rs;
      pk = std::max(pk, std::max(fabsf(l), fabsf(r)));
    }
    LevelPeak(t, pk * kBusGain);
  }
}

static void PadRender(float (*tl)[kBlock], float (*tr)[kBlock], const bool* used) {
  for (auto& v : g_pv) {
    if (!v.on) continue;
    const PadBuf& b = g_pbuf[v.slot];
    const PadSet& p = g_pset[v.slot];
    if (!b.d) { v.on = false; continue; }
    float* l = tl[v.slot / 16];
    float* r = tr[v.slot / 16];
    (void)used;
    double s0 = std::max(0.0, std::min(1.0, (double)p.in)) * (b.n - 2);
    double s1 = std::max(s0 + 32, std::min(1.0, (double)p.out) * (b.n - 1));
    double span = s1 - s0;
    double rate = pow(2.0, (p.pitch + p.fine * 0.01) / 12.0);
    float pl = cosf(p.pan * 1.5707963f) * 1.4142136f, pr = sinf(p.pan * 1.5707963f) * 1.4142136f;
    float lv = p.level * v.vel;
    float A = std::max(0.0005f, p.a) * kSR, D = p.d * kSR;
    for (size_t n = 0; n < kBlock; ++n) {
      if (v.pos >= span - 1) {
        if (v.mode == 2 && v.held && v.rel == 0) v.pos = fmod(v.pos, std::max(1.0, span - 1));
        else { v.on = false; break; }
      }
      if (v.rel == 0 && v.mode == 0 && p.r > 0.006f && (span - v.pos) / rate <= p.r * kSR) PadRelease(v, p.r);
      if (v.rel > 0) { v.env -= v.rel; if (v.env <= 0) { v.on = false; break; } }
      else {
        float tt = (float)v.t;
        v.env = tt < A ? tt / A : (tt < A + D ? 1.0f - (1.0f - p.s) * (tt - A) / std::max(1.0f, D) : p.s);
      }
      ++v.t;
      double x = p.rev ? s1 - 1 - v.pos : s0 + v.pos;
      long i0 = std::max(0L, std::min(b.n - 2, (long)x));
      float f = (float)(x - i0);
      const int16_t* q = b.d + i0 * 2;
      float sl = (q[0] + (q[2] - q[0]) * f) * (1.0f / 32768.0f);
      float sr = (q[1] + (q[3] - q[1]) * f) * (1.0f / 32768.0f);
      float tail = (float)std::min(1.0, (span - v.pos) / (0.003 * kSR));
      float g = v.env * lv * (v.mode == 2 ? 1.0f : tail);
      l[n] += sl * g * pl;
      r[n] += sr * g * pr;
      v.pos += rate;
    }
  }
}

// SAMPLE IN: the mix, frame by frame (audio thread)
static inline void PadRecWrite(float l, float r) {
  if (!g_prec_on.load(std::memory_order_relaxed)) return;
  long n = g_prec_n.load(std::memory_order_relaxed);
  if (n >= kPadMaxFrames) return;
  g_prec[2 * n] = (int16_t)lrintf(std::max(-1.0f, std::min(1.0f, l)) * 32767.0f);
  g_prec[2 * n + 1] = (int16_t)lrintf(std::max(-1.0f, std::min(1.0f, r)) * 32767.0f);
  g_prec_n.store(n + 1, std::memory_order_release);
}

static bool PadHandle(const Cmd& c) {
  switch (c.op) {
    case OP_PN: PadOn(c.i[0], c.f[0]); return true;
    case OP_PO: PadOff(c.i[0]); return true;
    case OP_PX: for (auto& v : g_pv) v.on = false; return true;
  }
  return false;
}

// command thread. -1: not ours · 0: done here · 1: queued for the audio thread
static int PadParse(const char* op, const char* a, Cmd* c) {
  if (!strcmp(op, "PN")) { c->op = OP_PN; return sscanf(a, "%d %f", &c->i[0], &c->f[0]) == 2; }
  if (!strcmp(op, "PO")) { c->op = OP_PO; return sscanf(a, "%d", &c->i[0]) == 1; }
  if (!strcmp(op, "PX")) { c->op = OP_PX; return 1; }
  if (!strcmp(op, "PMUTE")) { int s2 = -1, m = 0; if (sscanf(a, "%d %d", &s2, &m) == 2 && s2 >= 0 && s2 < kSlots) g_pmute[s2].store(m != 0); return 0; }
  if (!strcmp(op, "PSET")) {
    int s, mode, rev;
    float in, out, at, dt, su, rt, pi, fi, lv, pn;
    if (sscanf(a, "%d %f %f %f %f %f %f %d %f %f %f %f %d", &s, &in, &out, &at, &dt, &su, &rt, &mode, &pi, &fi, &lv, &pn, &rev) == 13 && s >= 0 && s < kSlots) {
      PadSet& p = g_pset[s];
      p.in = std::max(0.0f, std::min(0.999f, in)); p.out = std::max(p.in + 0.001f, std::min(1.0f, out));
      p.a = std::max(0.0005f, std::min(4.0f, at * 0.001f)); p.d = std::max(0.0f, std::min(8.0f, dt * 0.001f));
      p.s = std::max(0.0f, std::min(1.0f, su)); p.r = std::max(0.003f, std::min(8.0f, rt * 0.001f));
      p.mode = std::max(0, std::min(2, mode)); p.pitch = std::max(-24.0f, std::min(24.0f, pi)); p.fine = std::max(-100.0f, std::min(100.0f, fi));
      p.level = std::max(0.0f, std::min(2.0f, lv)); p.pan = std::max(0.0f, std::min(1.0f, pn)); p.rev = rev != 0;
    }
    return 0;
  }
  if (!strcmp(op, "PLOAD")) {
    PadJob j;
    char path[512];
    if (sscanf(a, "%d %lf %lf %511[^\n]", &j.slot, &j.start, &j.len, path) == 4 && j.slot >= 0 && j.slot < kSlots) {
      j.path = path;
      { std::lock_guard<std::mutex> lk(g_pjob_mu); g_pjobs.push_back(j); }
      g_pjob_cv.notify_one();
    }
    return 0;
  }
  if (!strcmp(op, "PCLR")) { int s; if (sscanf(a, "%d", &s) == 1 && s >= 0 && s < kSlots) PadInstall(s, nullptr, 0); return 0; }
  if (!strcmp(op, "PTRK")) { int t = -1; sscanf(a, "%d", &t); g_pad_track.store(t >= 0 && t < kTracks ? t : -1); return 0; }
  if (!strcmp(op, "PREC")) {
    int s = -1;
    if (sscanf(a, "%d", &s) == 1 && s >= 0 && s < kSlots) { g_prec_slot = s; g_prec_n.store(0); g_prec_on.store(1); }
    return 0;
  }
  if (!strcmp(op, "PRECSTOP")) {
    char path[512] = "";
    sscanf(a, " %511[^\n]", path);
    g_prec_on.store(0);
    usleep(12000);  // the audio thread's last frames
    long n = g_prec_n.load(std::memory_order_acquire);
    if (g_prec_slot < 0 || n < 64) { printf("PRECDONE %d 0 -\n", g_prec_slot); fflush(stdout); return 0; }
    int16_t* d = (int16_t*)malloc(n * 2 * sizeof(int16_t));
    if (d) {
      memcpy(d, g_prec, n * 2 * sizeof(int16_t));
      PadInstall(g_prec_slot, d, n);
    }
    if (path[0]) {  // and to the card, so the kit can find it again
      if (FILE* f = fopen(path, "wb")) {
        MtHeader(f, (uint32_t)n);
        fwrite(g_prec, 2, n * 2, f);
        MtHeader(f, (uint32_t)n);
        fclose(f);
      }
    }
    printf("PRECDONE %d %ld %s\n", g_prec_slot, n, path[0] ? path : "-");
    fflush(stdout);
    return 0;
  }
  return -1;
}

// out thread: how far SAMPLE IN has got
static void PadReport(int tick) {
  for (auto& g : g_gring) if (int16_t* p = g.exchange(nullptr)) free(p);
  if (tick % 25 == 0 && g_prec_on.load()) { printf("PRECLEN %ld\n", g_prec_n.load()); }
}
