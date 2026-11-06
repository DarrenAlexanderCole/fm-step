// MIXTAPE (0.9.22): records what you hear to a WAV on the card, and plays a
// take back (LISTEN). The audio thread only copies samples into a ring; a
// writer thread does all the card work, so a slow card can't cause a dropout
// (the ring holds ~21 s; if the card stalls longer, the gap is counted).
//
//   MTREC <path>          start a take (16-bit stereo 48 kHz WAV)   -> MTSTART <path>
//   MTPAUSE <0|1>         pause / resume the take (nothing is written while paused)
//   MTSTOP                finish it                                 -> MTDONE <frames> <path>
//                         (MTFULL first when it stopped at 20 minutes; MTERR <why> <path> on failure)
//   MTPLAY <sec> <path>   play a take from <sec>                    -> MTP <frame> <total> ... MTPEND
//   MTPLAYSTOP · MTPPAUSE <0|1>   stop · pause / resume (the position is kept)
//   MTLOOP <a> <b> <loop>  (0.9.24) for the next MTPLAY: play only [a, b] seconds, looping when loop=1
//   MTGAIN <dB> · MTCOMP <0..3>   (0.9.24) the take's record level and compressor (OFF, GLUE, PUNCH, LIMIT)
//   LEVELS <0|1>          per-track level reports for the mixer     -> LV <9 bytes hex>
//
// While recording: MTR <frames> <peakL> <peakR> <dropped> about 10 times a second.
// The WAV header is rewritten every second, so even a crash leaves a playable
// take. An overview (one level byte per 0.1 s) goes to <folder>/.peaks/<name>.pk.

#include <sys/stat.h>

static const long kMtMax = 20L * 60 * 48000;  // 20 minutes
static const uint32_t kMtRing = 1u << 20;     // frames (~21.8 s)
static int16_t g_mt_ring[kMtRing * 2];
static std::atomic<uint32_t> g_mt_w{0}, g_mt_r{0};
static std::atomic<int> g_mt_on{0};    // the audio thread copies while 1 (2 = paused)
static std::atomic<int> g_mt_busy{0};  // the writer thread is running
static std::atomic<uint32_t> g_mt_drop{0};
static std::atomic<long> g_mt_frames{0};
static std::atomic<float> g_mt_pk[2];
static uint32_t g_mt_rng = 0x2545F491;
static std::atomic<float> g_mt_gain{1.0f};
static std::atomic<int> g_mt_comp{0};
static float g_mt_env = 0;  // compressor envelope (audio thread)
static char g_mt_path[512];

static inline float MtRand() {  // -0.5..0.5, audio thread only
  g_mt_rng ^= g_mt_rng << 13; g_mt_rng ^= g_mt_rng >> 17; g_mt_rng ^= g_mt_rng << 5;
  return (g_mt_rng >> 8) * (1.0f / 16777216.0f) - 0.5f;
}
static inline int16_t MtQ(float x) {  // to 16 bits with TPDF dither
  float v = x * 32767.0f + MtRand() + MtRand();
  v = v < -32768.0f ? -32768.0f : v > 32767.0f ? 32767.0f : v;
  return (int16_t)lrintf(v);
}

// audio thread: one frame of the mix (before MAIN VOLUME)
static inline void MixtapeWrite(float l, float r) {
  if (g_mt_on.load(std::memory_order_relaxed) != 1) return;
  // record level and compressor (stereo linked), then a soft ceiling just under full scale
  float gn = g_mt_gain.load(std::memory_order_relaxed);
  l *= gn; r *= gn;
  int cm = g_mt_comp.load(std::memory_order_relaxed);
  if (cm > 0) {
    static const float thr[4] = {1, 0.126f, 0.2f, 0.708f};    // -18, -14, -3 dB
    static const float ratio[4] = {1, 2.0f, 4.0f, 20.0f};
    static const float atk[4] = {1, 0.0021f, 0.00083f, 0.02f};  // ~10 ms, ~25 ms, ~1 ms
    static const float rel[4] = {1, 0.0001f, 0.00017f, 0.00026f};
    static const float mk[4] = {1, 1.41f, 1.78f, 1.0f};         // makeup +3, +5, 0 dB
    float pk = std::max(fabsf(l), fabsf(r));
    g_mt_env += (pk - g_mt_env) * (pk > g_mt_env ? atk[cm] : rel[cm]);
    float g = 1;
    if (g_mt_env > thr[cm]) g = powf(g_mt_env / thr[cm], 1.0f / ratio[cm] - 1.0f);
    l *= g * mk[cm]; r *= g * mk[cm];
  }
  auto ceil = [](float x) { float a = fabsf(x); return a < 0.9f ? x : copysignf(0.9f + 0.1f * tanhf((a - 0.9f) * 10.0f), x); };
  l = ceil(l); r = ceil(r);
  uint32_t w = g_mt_w.load(std::memory_order_relaxed);
  if (w - g_mt_r.load(std::memory_order_acquire) >= kMtRing) { g_mt_drop.fetch_add(1, std::memory_order_relaxed); return; }
  uint32_t i = (w & (kMtRing - 1)) * 2;
  g_mt_ring[i] = MtQ(l);
  g_mt_ring[i + 1] = MtQ(r);
  g_mt_w.store(w + 1, std::memory_order_release);
  float al = fabsf(l), ar = fabsf(r);
  if (al > g_mt_pk[0].load(std::memory_order_relaxed)) g_mt_pk[0].store(al, std::memory_order_relaxed);
  if (ar > g_mt_pk[1].load(std::memory_order_relaxed)) g_mt_pk[1].store(ar, std::memory_order_relaxed);
}

static void MtHeader(FILE* f, uint32_t frames) {
  unsigned char hd[44] = {0};
  unsigned data = frames * 4;
  auto put32 = [&](int o, unsigned x) { for (int i = 0; i < 4; ++i) hd[o + i] = (x >> (8 * i)) & 255; };
  memcpy(hd, "RIFF", 4); put32(4, 36 + data); memcpy(hd + 8, "WAVEfmt ", 8);
  put32(16, 16); hd[20] = 1; hd[22] = 2; put32(24, 48000); put32(28, 48000 * 4); hd[32] = 4; hd[34] = 16;
  memcpy(hd + 36, "data", 4); put32(40, data);
  fseek(f, 0, SEEK_SET);
  fwrite(hd, 1, 44, f);
}

static std::string MtPeaksPath(const char* wav) {
  std::string p(wav);
  size_t sl = p.rfind('/');
  std::string dir = sl == std::string::npos ? "." : p.substr(0, sl);
  std::string name = sl == std::string::npos ? p : p.substr(sl + 1);
  if (name.size() > 4 && !strcasecmp(name.c_str() + name.size() - 4, ".wav")) name.resize(name.size() - 4);
  mkdir((dir + "/.peaks").c_str(), 0755);
  return dir + "/.peaks/" + name + ".pk";
}

static void* MtWriter(void*) {
  std::string path(g_mt_path);
  FILE* f = fopen(path.c_str(), "wb");
  if (!f) {
    g_mt_on.store(0);
    printf("MTERR open %s\n", path.c_str());
    fflush(stdout);
    g_mt_busy.store(0);
    return nullptr;
  }
  setvbuf(f, nullptr, _IOFBF, 1 << 16);
  MtHeader(f, 0);
  static unsigned char ov[kMtMax / 4800 + 2];
  long ov_n = 0, ov_acc = 0;
  float ov_pk = 0;
  long frames = 0, last_hdr = 0, last_sync = 0;
  bool err = false, full = false;
  for (;;) {
    uint32_t w = g_mt_w.load(std::memory_order_acquire), r = g_mt_r.load(std::memory_order_relaxed);
    long n = (long)(w - r);
    if (n == 0) {
      if (g_mt_on.load() == 0) break;
      usleep(20000);
      continue;
    }
    n = std::min(n, 8192L);
    n = std::min(n, kMtMax - frames);
    uint32_t i0 = r & (kMtRing - 1);
    long a = std::min<long>(n, kMtRing - i0);
    if (fwrite(g_mt_ring + 2 * i0, 4, a, f) != (size_t)a || (n > a && fwrite(g_mt_ring, 4, n - a, f) != (size_t)(n - a))) {
      err = true;
      g_mt_on.store(0);
      break;
    }
    for (long k = 0; k < n; ++k) {  // the overview: one level per 0.1 s
      uint32_t j = ((r + k) & (kMtRing - 1)) * 2;
      float m = (abs(g_mt_ring[j]) + abs(g_mt_ring[j + 1])) * (0.5f / 32768.0f);
      if (m > ov_pk) ov_pk = m;
      if (++ov_acc == 4800) { ov[ov_n++] = (unsigned char)LevelByte(ov_pk); ov_acc = 0; ov_pk = 0; }
    }
    g_mt_r.store(r + (uint32_t)n, std::memory_order_release);
    frames += n;
    g_mt_frames.store(frames);
    if (frames >= kMtMax) { full = true; g_mt_on.store(0); break; }
    if (frames - last_hdr >= 48000) {  // keep the header true: a crash still leaves a good take
      fflush(f);
      MtHeader(f, (uint32_t)frames);
      fseek(f, 0, SEEK_END);
      fflush(f);
      last_hdr = frames;
    }
    if (frames - last_sync >= 48000L * 10) { fsync(fileno(f)); last_sync = frames; }
  }
  if (ov_acc > 0) ov[ov_n++] = (unsigned char)LevelByte(ov_pk);
  fflush(f);
  MtHeader(f, (uint32_t)frames);
  fflush(f);
  fsync(fileno(f));
  if (fclose(f) != 0) err = true;
  if (FILE* p = fopen(MtPeaksPath(path.c_str()).c_str(), "wb")) { fwrite(ov, 1, ov_n, p); fclose(p); }
  g_mt_r.store(g_mt_w.load());
  if (full) printf("MTFULL\n");
  if (err) printf("MTERR write %s\n", path.c_str());
  printf("MTDONE %ld %s\n", frames, path.c_str());
  fflush(stdout);
  g_mt_busy.store(0);
  return nullptr;
}

// ---- LISTEN: a take streamed from the card

static const uint32_t kMpRing = 1u << 17;  // ~2.7 s
static int16_t g_mp_ring[kMpRing * 2];
static std::atomic<uint32_t> g_mp_w{0}, g_mp_r{0};
static std::atomic<int> g_mp_on{0}, g_mp_busy{0}, g_mp_eof{0}, g_mp_ended{0}, g_mp_pause{0};
static std::atomic<long> g_mp_pos{0}, g_mp_total{0};
static char g_mp_path[512];
static double g_mp_from = 0;
static double g_mp_la = 0, g_mp_lb = 0;  // MTLOOP: the part to play (seconds; lb <= la = all)
static int g_mp_loop = 0;
static float g_mp_g = 0;

static void* MpReader(void*) {
  FILE* f = fopen(g_mp_path, "rb");
  long data_at = -1, data_sz = 0;
  int ch = 0, bits = 0;
  unsigned rate = 0;
  if (f) {
    unsigned char h[12];
    if (fread(h, 1, 12, f) == 12 && !memcmp(h, "RIFF", 4) && !memcmp(h + 8, "WAVE", 4)) {
      for (;;) {
        unsigned char c8[8];
        if (fread(c8, 1, 8, f) != 8) break;
        unsigned sz = c8[4] | c8[5] << 8 | c8[6] << 16 | (unsigned)c8[7] << 24;
        if (!memcmp(c8, "fmt ", 4)) {
          unsigned char b[16];
          if (sz < 16 || fread(b, 1, 16, f) != 16) break;
          ch = b[2] | b[3] << 8; rate = b[4] | b[5] << 8 | b[6] << 16 | (unsigned)b[7] << 24; bits = b[14] | b[15] << 8;
          fseek(f, sz - 16 + (sz & 1), SEEK_CUR);
        } else if (!memcmp(c8, "data", 4)) {
          data_at = ftell(f);
          fseek(f, 0, SEEK_END);
          data_sz = std::min<long>(sz ? sz : 0x7fffffff, ftell(f) - data_at);  // a crashed take: trust the file size
          break;
        } else fseek(f, sz + (sz & 1), SEEK_CUR);
      }
    }
  }
  if (!f || data_at < 0 || bits != 16 || ch < 1 || ch > 2 || rate != 48000) {
    if (f) fclose(f);
    g_mp_on.store(0);
    printf("MTERR play %s\n", g_mp_path);
    fflush(stdout);
    g_mp_busy.store(0);
    return nullptr;
  }
  long total = data_sz / (2 * ch);
  long start = std::max(0L, std::min(total, (long)(g_mp_from * 48000)));
  g_mp_total.store(total);
  g_mp_pos.store(start);
  fseek(f, data_at + start * 2 * ch, SEEK_SET);
  long la = 0, lb = total;
  if (g_mp_lb > g_mp_la) {
    la = std::max(0L, std::min(total, (long)(g_mp_la * 48000)));
    lb = std::max(la + 1, std::min(total, (long)(g_mp_lb * 48000)));
  }
  long cur = start;
  if (cur >= lb) cur = la;
  if (cur != start) { start = cur; g_mp_pos.store(start); fseek(f, data_at + start * 2 * ch, SEEK_SET); }
  long left = lb - start;
  static int16_t buf[8192 * 2];
  while (g_mp_on.load() && (left > 0 || g_mp_loop)) {
    if (left <= 0) {  // the loop: back to its start
      fseek(f, data_at + la * 2 * ch, SEEK_SET);
      left = lb - la;
    }
    uint32_t w = g_mp_w.load(std::memory_order_relaxed);
    long space = (long)kMpRing - (long)(w - g_mp_r.load(std::memory_order_acquire));
    if (space < 4096) { usleep(10000); continue; }
    long n = std::min({space, 8192L, left});
    long got = (long)fread(buf, 2 * ch, n, f);
    if (got <= 0) break;
    for (long k = 0; k < got; ++k) {
      uint32_t j = ((w + k) & (kMpRing - 1)) * 2;
      g_mp_ring[j] = buf[k * ch];
      g_mp_ring[j + 1] = buf[k * ch + (ch - 1)];
    }
    g_mp_w.store(w + (uint32_t)got, std::memory_order_release);
    left -= got;
  }
  fclose(f);
  g_mp_eof.store(1);
  g_mp_busy.store(0);
  return nullptr;
}

// audio thread: add the take being listened to (fades in and out over ~10 ms)
static inline void MixtapePlay(float* l, float* r) {
  int on = g_mp_on.load(std::memory_order_relaxed);
  if (!on && g_mp_g <= 0) return;
  bool hold = g_mp_pause.load(std::memory_order_relaxed) != 0;
  if (hold && g_mp_g <= 0) return;  // paused: keep the place
  uint32_t rd = g_mp_r.load(std::memory_order_relaxed);
  if (rd == g_mp_w.load(std::memory_order_acquire)) {
    if (on && g_mp_eof.load()) { g_mp_on.store(0); g_mp_ended.store(1); }
    g_mp_g = 0;
    return;
  }
  g_mp_g += ((on && !hold ? 1.0f : 0.0f) - g_mp_g) * 0.01f;
  if ((!on || hold) && g_mp_g < 0.001f) g_mp_g = 0;
  uint32_t j = (rd & (kMpRing - 1)) * 2;
  *l += g_mp_ring[j] * (g_mp_g / 32768.0f);
  *r += g_mp_ring[j + 1] * (g_mp_g / 32768.0f);
  g_mp_r.store(rd + 1, std::memory_order_release);
  g_mp_pos.fetch_add(1, std::memory_order_relaxed);
}

static void MpStop() {
  g_mp_on.store(0);
  for (int k = 0; k < 200 && g_mp_busy.load(); ++k) usleep(2000);
  usleep(15000);  // let the fade finish
}

// ---- mixer levels: block peaks per track (and the mix), for the dots

static std::atomic<int> g_lv_on{0};
static std::atomic<float> g_lv[9];
static inline void LevelPeak(int i, float v) {
  if (v > g_lv[i].load(std::memory_order_relaxed)) g_lv[i].store(v, std::memory_order_relaxed);
}

// out thread: reports. tick = 4 ms
static void MixtapeReport(int tick) {
  if (tick % 25 == 0) {
    if (g_mt_busy.load())
      printf("MTR %ld %.3f %.3f %u\n", g_mt_frames.load(), g_mt_pk[0].exchange(0), g_mt_pk[1].exchange(0), g_mt_drop.load());
    if (g_mp_on.load()) printf("MTP %ld %ld\n", g_mp_pos.load(), g_mp_total.load());
  }
  if (g_mp_ended.exchange(0)) printf("MTPEND\n");
  if (g_lv_on.load() && tick % 8 == 0) {
    char hex[19];
    for (int i = 0; i < 9; ++i) {
      float p = g_lv[i].exchange(0);
      int v = p <= 1e-6f ? 0 : (int)lrintf((20.0f * log10f(p) + 60.0f) * (255.0f / 60.0f));
      v = v < 0 ? 0 : v > 255 ? 255 : v;
      snprintf(hex + 2 * i, 3, "%02x", v);
    }
    printf("LV %s\n", hex);
  }
}

static std::string RestOfLine(const char* a) {
  while (*a == ' ') ++a;
  std::string s(a);
  while (!s.empty() && (s.back() == '\n' || s.back() == '\r' || s.back() == ' ')) s.pop_back();
  return s;
}

// command thread. Returns 0 (handled) or -1 (not a MIXTAPE command).
static int MixtapeParse(const char* op, const char* a) {
  if (!strcmp(op, "MTREC")) {
    std::string path = RestOfLine(a);
    if (g_mt_busy.load() || path.empty() || path.size() >= sizeof(g_mt_path)) {
      printf("MTERR busy %s\n", path.c_str());
      fflush(stdout);
      return 0;
    }
    if (g_mp_on.load() || g_mp_busy.load()) MpStop();
    snprintf(g_mt_path, sizeof(g_mt_path), "%s", path.c_str());
    g_mt_w.store(0); g_mt_r.store(0); g_mt_drop.store(0); g_mt_frames.store(0);
    g_mt_pk[0].store(0); g_mt_pk[1].store(0);
    g_mt_busy.store(1);
    g_mt_on.store(1);
    pthread_t th;
    pthread_attr_t at;
    pthread_attr_init(&at);
    pthread_attr_setdetachstate(&at, PTHREAD_CREATE_DETACHED);
    if (pthread_create(&th, &at, MtWriter, nullptr) != 0) {
      g_mt_on.store(0); g_mt_busy.store(0);
      printf("MTERR thread %s\n", path.c_str());
    } else printf("MTSTART %s\n", path.c_str());
    pthread_attr_destroy(&at);
    fflush(stdout);
    return 0;
  }
  if (!strcmp(op, "MTSTOP")) { g_mt_on.store(0); return 0; }
  if (!strcmp(op, "MTPAUSE")) {
    int pz = 0;
    sscanf(a, "%d", &pz);
    int cur = g_mt_on.load();
    if (cur != 0) g_mt_on.compare_exchange_strong(cur, pz ? 2 : 1);
    return 0;
  }
  if (!strcmp(op, "MTPPAUSE")) { int pz = 0; sscanf(a, "%d", &pz); g_mp_pause.store(pz != 0); return 0; }
  if (!strcmp(op, "MTPLAY")) {
    float from = 0;
    char* end = nullptr;
    from = strtof(a, &end);
    std::string path = RestOfLine(end ? end : a);
    if (g_mt_busy.load() || path.empty() || path.size() >= sizeof(g_mp_path)) {
      printf("MTERR busy %s\n", path.c_str());
      fflush(stdout);
      return 0;
    }
    if (g_mp_on.load() || g_mp_busy.load()) MpStop();
    snprintf(g_mp_path, sizeof(g_mp_path), "%s", path.c_str());
    g_mp_from = std::max(0.0f, from);
    g_mp_w.store(0); g_mp_r.store(0); g_mp_eof.store(0); g_mp_ended.store(0); g_mp_pause.store(0);
    g_mp_busy.store(1);
    g_mp_on.store(1);
    pthread_t th;
    pthread_attr_t at;
    pthread_attr_init(&at);
    pthread_attr_setdetachstate(&at, PTHREAD_CREATE_DETACHED);
    if (pthread_create(&th, &at, MpReader, nullptr) != 0) { g_mp_on.store(0); g_mp_busy.store(0); }
    pthread_attr_destroy(&at);
    return 0;
  }
  if (!strcmp(op, "MTPLAYSTOP")) { g_mp_on.store(0); return 0; }
  if (!strcmp(op, "MTLOOP")) {
    double la = 0, lb = 0; int lp = 0;
    sscanf(a, "%lf %lf %d", &la, &lb, &lp);
    g_mp_la = la; g_mp_lb = lb; g_mp_loop = lp != 0 && lb > la;
    return 0;
  }
  if (!strcmp(op, "MTGAIN")) { float db = 0; sscanf(a, "%f", &db); g_mt_gain.store(powf(10.0f, std::max(-60.0f, std::min(24.0f, db)) / 20.0f)); return 0; }
  if (!strcmp(op, "MTCOMP")) { int m = 0; sscanf(a, "%d", &m); g_mt_comp.store(std::max(0, std::min(3, m))); return 0; }
  if (!strcmp(op, "LEVELS")) { int on = 0; sscanf(a, "%d", &on); g_lv_on.store(on != 0); return 0; }
  return -1;
}
