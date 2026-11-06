// FLUX part 2: the rolling tape, GRAB, slices on the pads and scrub.
// Original FM-STEP code (MIT). Included by fmstep_dsp.cc after the
// sequencer and before RenderBlock.
//
//   TAPE   everything you hear (after the effects, before MAIN VOLUME) is
//          written into a 24 s ring, always. While the sequencer plays, the
//          frame where each bar starts is remembered, so a GRAB can be cut
//          exactly on bar lines.
//   GRAB   copies the last 1/2/4 bars out of the ring into one of two grab
//          buffers (on the command thread, never the audio thread), then
//          the audio thread swaps to it.
//   SLICES 32 slice definitions (start, end, pitch, reverse) set by the app;
//          6 slice voices play them from the pads: one-shot, gate (plays
//          while held) or tape (spins up, and spins down on release).
//          Pad pressure DRAGS a playing slice: up to a semitone slower with
//          a tape wobble, like leaning on the reel.
//   SCRUB  the fader moves a scrub head through the grab; the sound follows
//          how fast the head moves (silent when it stops), like rocking
//          tape by hand.
//
// Commands (one per line, like the rest of the engine):
//   GRAB <bars>                 grab the last bars (1..8)
//   SLC <i> <start> <end> <pitch> <rev>   slice i, frames from the grab start
//   SL <i> <vel> <lag_ms> <mode>          play slice i (mode 0 one-shot, 1 gate, 2 tape)
//   SLO <i>                     release slice i (gate / tape modes)
//   SLP <i> <pressure>          pad pressure 0..1 on slice i
//   SLX                         all slice voices off
//   SCRUB <0|1>                 scrub head off / on
//   SCRUBPOS <0..1>             scrub head target in the grab
//   TAPEMON <0|1>               stream the tape's levels ("TW ...") for the TAPE view
//   CHOPTRK <t|-1>              track t's notes play slices (note 36 = slice 1)
//   CHOPCFG <motion> <drift>    motion (0 one-shot, 1 gate, 2 loop) and drift for those
//   LOAD <file.wav>             a WAV from the card becomes the grab (to chop)
//   LOADAT <sec> <file.wav>    the same, starting <sec> into the file (a MIXTAPE take)
//   LOADKIT <folder>            up to 16 WAVs in a folder, one per pad (KIT reply)
//   SAVE <file.wav>             the current grab to the card
// Replies:
//   GRAB <frames> <bars> <bpm> <how>   then GW <hex> (256 x lo,hi levels), GT <n> <frames...> (hits)
//        how: 0 grab (free), 1 grab (on bar lines), 2 file, 3 kit
//   GRABEMPTY · LOADERR <why> <path> · KIT <n> <s e>... · SAVED <path> · SAVEERR <path>
//   TW <hex>                    new tape level points (lo,hi per 1024 frames)

#include <cstdlib>
#include <dirent.h>
#include <mutex>
#include <string>
#include <strings.h>
#include <vector>
#include <algorithm>

static const long kTapeFrames = 24L * 48000;  // 24 s
static const long kGrabFrames = 16L * 48000;  // longest grab: 16 s
static const int kSlices = 32;
static const int kSliceVoices = 6;
static const int kTapeChunk = 1024;           // frames per TW level point

static float* g_rtape = nullptr;               // [kTapeFrames * 2]
static std::atomic<long> g_tape_w{0};         // frames written so far (never wraps in practice)
static float* g_grab[2] = {nullptr, nullptr}; // [kGrabFrames * 2] each
static int g_grab_cur = -1;                   // audio thread: the buffer playing (-1 none)
static long g_grab_len = 0;                   // audio thread: its length in frames
static int g_grab_next = 0;                   // command thread: the buffer to fill next

// bar lines (tape frame where each bar started), written by the audio thread
static const int kBarHist = 16;
static std::atomic<long> g_bar_at[kBarHist];
static std::atomic<int> g_bar_n{0};           // bar lines recorded so far
static long g_last_bar = -1;                  // audio thread: last bar number seen

// tape levels for the TAPE view
static std::atomic<bool> g_tapemon{false};
static const int kTwRing = 512;
static uint16_t g_tw[kTwRing];                // lo << 8 | hi, 0..255 each
static std::atomic<int> g_tw_w{0};
static float g_tw_lp = 0, g_tw_lo = 0, g_tw_hi = 0;
static int g_tw_n = 0;

struct SliceDef { long start = 0, end = 0; float pitch = 0; bool rev = false; };
static SliceDef g_slc[kSlices];

struct SliceVoice {
  bool on = false;
  int slice = -1, mode = 0;
  long start = 0, end = 0;   // frames in the grab
  double pos = 0;            // read head (frames from start, in play direction)
  double rate = 1;
  bool rev = false, held = false;
  float vel = 1, env = 0, rel = 0;  // rel: 0 playing, >0 releasing
  long delay = 0;            // frames to wait (DRIFT lag)
  float spin = 1;            // tape mode speed 0..1
  float press = 0, drag = 0, wob = 0;
  unsigned age = 0;
};
static SliceVoice g_sv[kSliceVoices];
static unsigned g_sv_age = 0;

// GRAB requests: taken on the command thread, done by SamplerPoll (the
// output thread live, the render loop offline) so a grab pressed late in a
// bar can wait for that bar to finish without blocking anything.
static std::atomic<int> g_grab_req{0};      // bars requested (0 = none)
static std::atomic<long> g_grab_len_pub{0}; // length of the newest buffer (for SAVE)
static std::atomic<int> g_grab_wait_n{-1};  // grab once this many bar lines exist (-1 = now)
static Ring<Cmd, 8> g_grab_swaps;           // finished grabs for the audio thread (live)

static bool g_scrub_on = false;
static std::atomic<float> g_scrub_target{0};
static double g_scrub_pos = 0;
static float g_scrub_gain = 0;

enum { OP_GRAB_SWAP = 100, OP_SLC, OP_SL_ON, OP_SL_OFF, OP_SL_PRESS, OP_SL_ALLOFF, OP_SCRUB };

static void SamplerInit() {
  g_rtape = (float*)calloc(kTapeFrames * 2, sizeof(float));
  g_grab[0] = (float*)calloc(kGrabFrames * 2, sizeof(float));
  g_grab[1] = (float*)calloc(kGrabFrames * 2, sizeof(float));
  for (auto& b : g_bar_at) b.store(0);
}

static inline float LevelByte(float x) {  // 0..1 peak -> 0..255 on a -48..0 dB scale
  float db = 20.0f * log10f(x + 1e-6f);
  float v = (db + 48.0f) / 48.0f * 255.0f;
  return v < 0 ? 0 : v > 255 ? 255 : v;
}

// audio thread: one frame of what you hear goes on the tape
static inline void TapeWrite(float l, float r) {
  long w = g_tape_w.load(std::memory_order_relaxed);
  long k = w % kTapeFrames;
  g_rtape[2 * k] = l;
  g_rtape[2 * k + 1] = r;
  g_tape_w.store(w + 1, std::memory_order_release);
  // levels for the TAPE view: lows (below ~250 Hz) and the rest
  float m = 0.5f * (l + r);
  g_tw_lp += (m - g_tw_lp) * 0.032f;
  g_tw_lo = std::max(g_tw_lo, fabsf(g_tw_lp));
  g_tw_hi = std::max(g_tw_hi, fabsf(m - g_tw_lp));
  if (++g_tw_n >= kTapeChunk) {
    int i = g_tw_w.load(std::memory_order_relaxed);
    g_tw[i % kTwRing] = (uint16_t)((int)LevelByte(g_tw_lo * 1.6f) << 8 | (int)LevelByte(g_tw_hi * 1.6f));
    g_tw_w.store(i + 1, std::memory_order_release);
    g_tw_n = 0; g_tw_lo = g_tw_hi = 0;
  }
}

// audio thread: called with the sequencer clock (1/16 steps) for every frame
static inline void TapeBarCheck(double clock16, bool playing) {
  if (!playing) {
    if (g_last_bar != -1) g_bar_n.store(0, std::memory_order_release);  // new PLAY = new bar lines
    g_last_bar = -1;
    return;
  }
  long bar = (long)floor(clock16 / 16.0);
  if (bar != g_last_bar) {
    g_last_bar = bar;
    int n = g_bar_n.load(std::memory_order_relaxed);
    g_bar_at[n % kBarHist].store(g_tape_w.load(std::memory_order_relaxed), std::memory_order_relaxed);
    g_bar_n.store(n + 1, std::memory_order_release);
  }
}

static inline float GrabAt(const float* g, long len, double p, int ch) {
  if (p < 0 || p >= len - 1) return 0;
  long i = (long)p;
  float f = (float)(p - i);
  return g[2 * i + ch] + (g[2 * (i + 1) + ch] - g[2 * i + ch]) * f;
}

// Transients: where the hits are (frames), strongest first then sorted.
// Energy of the high-passed signal in 256-frame hops; a hit is a jump well
// above the recent average, at least 1/32 note after the last one.
static int Onsets(const float* g, long len, long* out, int maxn) {
  const int hop = 256;
  long nh = len / hop;
  if (nh < 4) { out[0] = 0; return 1; }
  std::vector<float> e(nh), od(nh, 0.0f);
  float hp = 0, prev = 0;
  for (long h = 0; h < nh; ++h) {
    float acc = 0;
    for (long i = h * hop; i < (h + 1) * hop; ++i) {
      float m = 0.5f * (g[2 * i] + g[2 * i + 1]);
      hp = 0.97f * (hp + m - prev);  // gentle high-pass: kicks still count
      prev = m;
      acc += hp * hp + 0.25f * m * m;
    }
    e[h] = sqrtf(acc / hop);
  }
  for (long h = 1; h < nh; ++h) {
    float avg = 0;
    int k = 0;
    for (long j = std::max(0L, h - 8); j < h; ++j) { avg += e[j]; ++k; }
    avg /= std::max(1, k);
    od[h] = std::max(0.0f, e[h] - 1.3f * avg - 0.002f);
  }
  long gap = (long)(60.0f / Bpm() / 8.0f * kSR / hop);  // 1/32 note in hops
  gap = std::max(4L, gap);
  // peaks, strongest first
  std::vector<std::pair<float, long>> pk;
  for (long h = 1; h + 1 < nh; ++h)
    if (od[h] > 0 && od[h] >= od[h - 1] && od[h] >= od[h + 1]) pk.push_back({od[h], h});
  std::sort(pk.begin(), pk.end(), [](const std::pair<float, long>& a, const std::pair<float, long>& b) { return a.first > b.first; });
  std::vector<long> pick;
  pick.push_back(0);
  for (auto& q : pk) {
    if ((int)pick.size() >= maxn) break;
    bool ok = true;
    for (long x : pick) if (labs(x - q.second) < gap) { ok = false; break; }
    if (ok) pick.push_back(q.second);
  }
  std::sort(pick.begin(), pick.end());
  int n = 0;
  for (long h : pick) {
    // back up to just before the rise (2 hops), so the attack is kept
    long f = std::max(0L, (h - (h > 0 ? 1 : 0)) * hop);
    out[n++] = f;
  }
  return n;
}

// GRAB / GW / GT lines for a new buffer (and KIT boundaries when given).
static void PublishGrab(const float* g, long len, int bars, float bpm, int how) {
  printf("GRAB %ld %d %.2f %d\n", len, bars, bpm, how);
  char hex[256 * 4 + 1];
  float lp = 0;
  for (int p = 0; p < 256; ++p) {
    long a = len * p / 256, b = len * (p + 1) / 256;
    float lo = 0, hi = 0;
    for (long i = a; i < b; ++i) {
      float m = 0.5f * (g[2 * i] + g[2 * i + 1]);
      lp += (m - lp) * 0.032f;
      lo = std::max(lo, fabsf(lp));
      hi = std::max(hi, fabsf(m - lp));
    }
    snprintf(hex + 4 * p, 5, "%02x%02x", (unsigned char)LevelByte(lo * 1.6f), (unsigned char)LevelByte(hi * 1.6f));
  }
  printf("GW %s\n", hex);
  long on[16];
  int n = Onsets(g, len, on, 16);
  printf("GT %d", n);
  for (int i = 0; i < n; ++i) printf(" %ld", on[i]);
  printf("\n");
  fflush(stdout);
}

static float PeakOf(const float* g, long len) {
  float pk = 0;
  for (long i = 0; i < 2 * len; ++i) pk = std::max(pk, fabsf(g[i]));
  return pk;
}

static void Declick(float* g, long len) {  // short fades at both ends so a loop never clicks
  long fade = std::min(len / 4, 96L);
  for (long i = 0; i < fade; ++i) {
    float a = (float)i / fade;
    g[2 * i] *= a; g[2 * i + 1] *= a;
    g[2 * (len - 1 - i)] *= a; g[2 * (len - 1 - i) + 1] *= a;
  }
}

// GRAB <bars>: copies out of the ring. Returns false (and says GRABEMPTY)
// when there is nothing on the tape, so a silent grab never replaces chops.
static bool SamplerGrab(int bars, int* out_buf, long* out_len) {
  bars = std::max(1, std::min(8, bars));
  long now = g_tape_w.load(std::memory_order_acquire);
  float bpm = Bpm();
  long bar_frames = (long)(4.0f * 60.0f / bpm * kSR);
  long want = (long)bars * bar_frames;
  long start = 0, end = now;
  int aligned = 0;
  int nb = g_bar_n.load(std::memory_order_acquire);
  if (g_seq.playing && nb >= 1) {
    long last = g_bar_at[(nb - 1) % kBarHist].load();
    if (now - last < bar_frames * 2 && last > 0) {
      end = last;
      aligned = 1;
      if (nb >= bars + 1) {  // cut on the bar lines when they span about `bars` bars
        long first = g_bar_at[(nb - 1 - bars) % kBarHist].load();
        if (last > first && labs((last - first) - want) < want / 4) start = first;
        else start = end - want;
      } else start = end - want;
    }
  }
  if (!aligned) start = end - want;
  long len = end - start;
  if (len > kGrabFrames) { start = end - kGrabFrames; len = kGrabFrames; }
  if (start < 0) { start = 0; len = end; }
  if (now - start > kTapeFrames - 48000) { start = now - (kTapeFrames - 48000); len = end - start; }
  float* g = g_grab[g_grab_next];
  for (long i = 0; i < len; ++i) {
    long k = (start + i) % kTapeFrames;
    g[2 * i] = g_rtape[2 * k];
    g[2 * i + 1] = g_rtape[2 * k + 1];
  }
  if (len < 4800 || PeakOf(g, len) < 0.001f) {  // under -60 dB: nothing there
    printf("GRABEMPTY\n");
    fflush(stdout);
    return false;
  }
  Declick(g, len);
  PublishGrab(g, len, bars, bpm, aligned);
  *out_buf = g_grab_next;
  *out_len = len;
  g_grab_next ^= 1;
  return true;
}

// ---- WAV files: LOAD (a loop to chop), LOADKIT (a folder: one sound per pad), SAVE

static bool ReadWav(const char* path, std::vector<float>* lr, char* err, size_t errn, double off_s = 0) {
  FILE* f = fopen(path, "rb");
  if (!f) { snprintf(err, errn, "open"); return false; }
  unsigned char h[12];
  if (fread(h, 1, 12, f) != 12 || memcmp(h, "RIFF", 4) || memcmp(h + 8, "WAVE", 4)) { fclose(f); snprintf(err, errn, "not-wav"); return false; }
  int fmt = 0, ch = 0, bits = 0;
  unsigned rate = 0;
  std::vector<unsigned char> data;
  for (;;) {
    unsigned char c8[8];
    if (fread(c8, 1, 8, f) != 8) break;
    unsigned sz = c8[4] | c8[5] << 8 | c8[6] << 16 | (unsigned)c8[7] << 24;
    if (!memcmp(c8, "fmt ", 4)) {
      unsigned char b[40] = {0};
      size_t n = fread(b, 1, std::min(sz, 40u), f);
      if (sz > 40) fseek(f, sz - 40, SEEK_CUR);
      if (n < 16) break;
      fmt = b[0] | b[1] << 8; ch = b[2] | b[3] << 8;
      rate = b[4] | b[5] << 8 | b[6] << 16 | (unsigned)b[7] << 24;
      bits = b[14] | b[15] << 8;
      if (fmt == 0xFFFE && n >= 26) fmt = b[24] | b[25] << 8;  // extensible: the sub-format
    } else if (!memcmp(c8, "data", 4)) {
      if (off_s > 0 && ch > 0 && bits >= 8) {  // LOADAT: start this far in (a MIXTAPE take)
        long fb = (long)ch * (bits / 8);
        long skip = std::min<long>((long)sz / fb, (long)(off_s * rate)) * fb;
        fseek(f, skip, SEEK_CUR);
        sz -= (unsigned)skip;
      }
      size_t cap = std::min<size_t>(sz, (size_t)kGrabFrames * 8 * 4 * 2);  // enough for 16 s at 192k
      data.resize(cap);
      data.resize(fread(data.data(), 1, cap, f));
      break;
    } else fseek(f, sz + (sz & 1), SEEK_CUR);
  }
  fclose(f);
  if (ch < 1 || ch > 8 || rate < 4000 || rate > 384000 || !(fmt == 1 || fmt == 3) || !(bits == 16 || bits == 24 || bits == 32 || bits == 8)) {
    snprintf(err, errn, "format(fmt=%d ch=%d bits=%d rate=%u)", fmt, ch, bits, rate);
    return false;
  }
  int bps = bits / 8;
  long frames = (long)(data.size() / (bps * ch));
  auto sample = [&](long fr, int c) -> float {
    const unsigned char* q = data.data() + (fr * ch + c) * bps;
    if (fmt == 3 && bits == 32) { float x; memcpy(&x, q, 4); return x; }
    if (bits == 8) return (q[0] - 128) / 128.0f;
    if (bits == 16) return (int16_t)(q[0] | q[1] << 8) / 32768.0f;
    if (bits == 24) { int32_t v = (q[0] << 8 | q[1] << 16 | q[2] << 24); return v / 2147483648.0f; }
    int32_t v = (int32_t)(q[0] | q[1] << 8 | q[2] << 16 | (unsigned)q[3] << 24);
    return v / 2147483648.0f;
  };
  // to stereo at 48 kHz (linear interpolation)
  double step = (double)rate / kSR;
  long out = std::min((long)(frames / step), kGrabFrames);
  lr->resize(out * 2);
  for (long i = 0; i < out; ++i) {
    double p = i * step;
    long a = (long)p;
    long b = std::min(a + 1, frames - 1);
    float t = (float)(p - a);
    for (int c = 0; c < 2; ++c) {
      int cc = std::min(c, ch - 1);
      (*lr)[2 * i + c] = sample(a, cc) + (sample(b, cc) - sample(a, cc)) * t;
    }
  }
  return out > 0;
}

static std::mutex g_load_mu;
static std::string g_load_path;
static std::atomic<int> g_load_kind{0};  // 1 LOAD, 2 LOADKIT, 3 SAVE
static double g_load_off = 0;            // LOADAT: seconds into the file (under g_load_mu)

static bool EndsWithWav(const char* n) {
  size_t l = strlen(n);
  return l > 4 && (!strcasecmp(n + l - 4, ".wav"));
}

// LOAD / LOADKIT into the next grab buffer. KIT replies "KIT n s0 e0 s1 e1 ...".
static bool SamplerLoad(int kind, const std::string& path, int* out_buf, long* out_len) {
  float* g = g_grab[g_grab_next];
  long len = 0;
  char err[96] = "";
  std::vector<std::pair<long, long>> parts;
  if (kind == 1) {
    std::vector<float> lr;
    double off;
    { std::lock_guard<std::mutex> lk(g_load_mu); off = g_load_off; }
    if (!ReadWav(path.c_str(), &lr, err, sizeof(err), off)) { printf("LOADERR %s %s\n", err, path.c_str()); fflush(stdout); return false; }
    len = (long)lr.size() / 2;
    memcpy(g, lr.data(), lr.size() * sizeof(float));
  } else {
    DIR* d = opendir(path.c_str());
    if (!d) { printf("LOADERR dir %s\n", path.c_str()); fflush(stdout); return false; }
    std::vector<std::string> names;
    while (dirent* e = readdir(d)) if (e->d_name[0] != '.' && EndsWithWav(e->d_name)) names.push_back(e->d_name);
    closedir(d);
    std::sort(names.begin(), names.end());
    for (auto& n : names) {
      if ((int)parts.size() >= 16) break;
      std::vector<float> lr;
      if (!ReadWav((path + "/" + n).c_str(), &lr, err, sizeof(err))) continue;
      long fl = std::min((long)lr.size() / 2, kGrabFrames - len - 960);
      if (fl < 64) break;
      memcpy(g + 2 * len, lr.data(), fl * 2 * sizeof(float));
      parts.push_back({len, len + fl});
      len += fl;
      memset(g + 2 * len, 0, 960 * 2 * sizeof(float));  // 20 ms of silence between sounds
      len += 960;
    }
    if (parts.empty()) { printf("LOADERR empty-kit %s\n", path.c_str()); fflush(stdout); return false; }
  }
  if (len < 64) { printf("LOADERR short %s\n", path.c_str()); fflush(stdout); return false; }
  if (kind == 1) Declick(g, len);
  PublishGrab(g, len, 0, Bpm(), kind == 1 ? 2 : 3);
  if (kind == 2) {
    printf("KIT %d", (int)parts.size());
    for (auto& q : parts) printf(" %ld %ld", q.first, q.second);
    printf("\n");
    fflush(stdout);
  }
  *out_buf = g_grab_next;
  *out_len = len;
  g_grab_next ^= 1;
  return true;
}

// SAVE: the current grab as a 16-bit stereo 48 kHz WAV.
static void SamplerSave(const std::string& path) {
  const float* g = g_grab[g_grab_next ^ 1];
  long len = g_grab_len_pub.load();
  FILE* f = len > 0 ? fopen(path.c_str(), "wb") : nullptr;
  if (!f) { printf("SAVEERR %s\n", path.c_str()); fflush(stdout); return; }
  unsigned data = (unsigned)len * 4;
  unsigned char hd[44] = {0};
  auto put32 = [&](int o, unsigned x) { for (int i = 0; i < 4; ++i) hd[o + i] = (x >> (8 * i)) & 255; };
  memcpy(hd, "RIFF", 4); put32(4, 36 + data); memcpy(hd + 8, "WAVEfmt ", 8);
  put32(16, 16); hd[20] = 1; hd[22] = 2; put32(24, 48000); put32(28, 48000 * 4); hd[32] = 4; hd[34] = 16;
  memcpy(hd + 36, "data", 4); put32(40, data);
  fwrite(hd, 1, 44, f);
  std::vector<int16_t> buf(4096 * 2);
  for (long i = 0; i < len; i += 4096) {
    long n = std::min(4096L, len - i);
    for (long k = 0; k < 2 * n; ++k) buf[k] = (int16_t)lrintf(std::max(-1.0f, std::min(1.0f, g[2 * i + k])) * 32767.0f);
    fwrite(buf.data(), 2, 2 * n, f);
  }
  fclose(f);
  sync();
  printf("SAVED %s\n", path.c_str());
  fflush(stdout);
}

// Do a requested GRAB once its bar line has passed. Returns true with the
// swap command in *c when a grab is ready (the caller hands it to the audio
// thread). Gives up waiting after 2 bars (sequencer stopped meanwhile).
static bool SamplerPoll(Cmd* c) {
  int kind = g_load_kind.exchange(0);
  if (kind) {
    std::string path;
    { std::lock_guard<std::mutex> lk(g_load_mu); path = g_load_path; }
    if (kind == 3) { SamplerSave(path); return false; }
    int buf; long len;
    if (!SamplerLoad(kind, path, &buf, &len)) return false;
    memset(c, 0, sizeof(*c));
    c->op = OP_GRAB_SWAP; c->i[0] = buf; c->i[1] = (int)len;
    g_grab_len_pub.store(len);
    return true;
  }
  int bars = g_grab_req.load();
  if (bars <= 0) return false;
  int wait = g_grab_wait_n.load();
  static long wait_since = -1;
  if (wait >= 0) {
    long now = g_tape_w.load();
    if (wait_since < 0) wait_since = now;
    bool late = now - wait_since > (long)(8.0f * 60.0f / Bpm() * kSR) || !g_seq.playing;
    if (g_bar_n.load(std::memory_order_acquire) < wait && !late) return false;
  }
  wait_since = -1;
  g_grab_req.store(0);
  int buf; long len;
  if (!SamplerGrab(bars, &buf, &len)) return false;
  memset(c, 0, sizeof(*c));
  c->op = OP_GRAB_SWAP; c->i[0] = buf; c->i[1] = (int)len;
  g_grab_len_pub.store(len);
  return true;
}

// command thread: new TW points since the last call
static void SamplerTapeReport() {
  static int sent = 0;
  if (!g_tapemon.load()) { sent = g_tw_w.load(); return; }
  int w = g_tw_w.load(std::memory_order_acquire);
  if (w - sent > 64) sent = w - 64;
  if (w == sent) return;
  char hex[64 * 4 + 1];
  int n = 0;
  for (; sent < w && n < 64; ++sent, ++n) snprintf(hex + 4 * n, 5, "%04x", (unsigned)(g_tw[sent % kTwRing] & 0xffff));
  printf("TW %s\n", hex);
}

// ---- audio thread -----------------------------------------------------------

static void SliceOn(int i, float vel, float lag_ms, int mode) {
  if (g_grab_cur < 0 || i < 0 || i >= kSlices) return;
  const SliceDef& d = g_slc[i];
  if (d.end - d.start < 64) return;  // no slice on this pad (short kit, few hits)
  long s = std::max(0L, std::min(g_grab_len - 2, d.start)), e = std::max(s + 64, std::min(g_grab_len - 1, d.end));
  // gate / one-shot retrigger: a slice already playing is released (choke)
  for (auto& v : g_sv) if (v.on && v.slice == i && v.rel == 0) v.rel = 1;
  SliceVoice* best = nullptr;
  for (auto& v : g_sv) if (!v.on) { best = &v; break; }
  if (!best) for (auto& v : g_sv) if (v.rel > 0 && (!best || v.env < best->env)) best = &v;
  if (!best) { best = &g_sv[0]; for (auto& v : g_sv) if (v.age < best->age) best = &v; }
  SliceVoice& v = *best;
  v = SliceVoice();
  v.on = true; v.slice = i; v.mode = mode; v.start = s; v.end = e;
  v.rev = d.rev; v.rate = powf(2.0f, d.pitch / 12.0f);
  v.vel = std::max(0.05f, std::min(1.0f, vel));
  v.held = true;
  v.delay = (long)(std::max(0.0f, lag_ms) * 0.001f * kSR);
  v.spin = 1.0f;
  v.age = ++g_sv_age;
}

static void SliceOff(int i) {
  for (auto& v : g_sv) if (v.on && v.slice == i && v.held) {
    v.held = false;
    if ((v.mode == 1 || v.mode == 2) && v.rel == 0) v.rel = 1;  // gate / loop: let go = stop
  }
}

static void SlicePress(int i, float p) {
  for (auto& v : g_sv) if (v.on && v.slice == i) v.press = std::max(0.0f, std::min(1.0f, p));
}

// Render the slice voices and the scrub head, adding into l/r (kBlock frames).
static const float kSliceGain = 0.46f;  // grab re-enters before the tape stage: level-matched (offline test)

static void SamplerRender(float* l, float* r) {
  if (g_grab_cur < 0) return;
  const float* g = g_grab[g_grab_cur];
  const long len = g_grab_len;
  for (auto& v : g_sv) {
    if (!v.on) continue;
    for (size_t n = 0; n < kBlock; ++n) {
      if (v.delay > 0) { --v.delay; continue; }
      // pressure: drag the tape (slower, with wow), smoothed
      v.drag += (v.press - v.drag) * 0.004f;
      v.wob += 2.0f * 3.14159265f * 5.5f / kSR;
      if (v.wob > 6.2831853f) v.wob -= 6.2831853f;
      double rate = v.rate * v.spin * (1.0 - 0.055 * v.drag) * (1.0 + 0.012 * v.drag * sinf(v.wob));
      double span = (double)(v.end - v.start);
      if (v.pos >= span - 1) {
        if (v.mode == 2 && v.held && v.rel == 0) v.pos = 0;  // LOOP: round again while held
        else { v.on = false; break; }
      }
      double p = v.rev ? (double)v.end - 1 - v.pos : (double)v.start + v.pos;
      // envelope: 1.5 ms attack, 5 ms release, 4 ms fade before the end
      if (v.rel > 0) { v.env -= 1.0f / (0.005f * kSR); if (v.env <= 0) { v.on = false; break; } }
      else v.env = std::min(1.0f, v.env + 1.0f / (0.0015f * kSR));
      float tail = (float)std::min(1.0, (span - v.pos) / (0.004 * kSR));
      float a = v.env * tail * v.vel * kSliceGain;
      l[n] += GrabAt(g, len, p, 0) * a;
      r[n] += GrabAt(g, len, p, 1) * a;
      v.pos += rate;
    }
  }
  if (g_scrub_on || g_scrub_gain > 0.0001f) {
    double target = (double)g_scrub_target.load(std::memory_order_relaxed) * (len - 2);
    for (size_t n = 0; n < kBlock; ++n) {
      double prev = g_scrub_pos;
      g_scrub_pos += (target - g_scrub_pos) * 0.0009;  // ~25 ms follow
      float speed = (float)fabs(g_scrub_pos - prev);
      float want = g_scrub_on ? std::min(1.0f, speed * 1.6f) : 0.0f;
      g_scrub_gain += (want - g_scrub_gain) * 0.004f;
      l[n] += GrabAt(g, len, g_scrub_pos, 0) * g_scrub_gain * kSliceGain;
      r[n] += GrabAt(g, len, g_scrub_pos, 1) * g_scrub_gain * kSliceGain;
    }
  }
}

static bool SamplerHandle(const Cmd& c) {
  switch (c.op) {
    case OP_GRAB_SWAP:
      for (auto& v : g_sv) v.on = false;
      g_grab_cur = c.i[0];
      g_grab_len = (long)c.i[1];
      for (int i = 0; i < kSlices; ++i) {  // default: 16 even slices (the app may redefine them)
        g_slc[i] = SliceDef();
        g_slc[i].start = g_grab_len * (i % 16) / 16;
        g_slc[i].end = g_grab_len * (i % 16 + 1) / 16;
      }
      g_scrub_pos = std::min(g_scrub_pos, (double)std::max(0L, g_grab_len - 2));
      return true;
    case OP_SLC:
      if (c.i[0] >= 0 && c.i[0] < kSlices) {
        SliceDef& d = g_slc[c.i[0]];
        d.start = c.i[1]; d.end = c.i[2]; d.pitch = c.f[0]; d.rev = c.i[3] != 0;
      }
      return true;
    case OP_SL_ON: SliceOn(c.i[0], c.f[0], c.f[1], c.i[2]); return true;
    case OP_SL_OFF: SliceOff(c.i[0]); return true;
    case OP_SL_PRESS: SlicePress(c.i[0], c.f[0]); return true;
    case OP_SL_ALLOFF: for (auto& v : g_sv) if (v.on) v.rel = 1; return true;
    case OP_SCRUB: g_scrub_on = c.i[0] != 0; return true;
  }
  return false;
}

// command thread: parse sampler commands. Same return codes as Parse.
static int SamplerParse(const char* op, const char* a, Cmd* c) {
  if (!strcmp(op, "GRAB")) {
    int bars = 2;
    sscanf(a, "%d", &bars);
    // playing and more than half-way through a bar: wait for that bar to end
    int wait = -1;
    int nb = g_bar_n.load(std::memory_order_acquire);
    if (g_seq.playing && nb >= 1) {
      long bar_frames = (long)(4.0f * 60.0f / Bpm() * kSR);
      long since = g_tape_w.load() - g_bar_at[(nb - 1) % kBarHist].load();
      if (since > bar_frames / 2 && since < bar_frames * 2) {
        wait = nb + 1;
        printf("GRABWAIT %ld\n", std::max(0L, bar_frames - since) * 1000 / 48000);
        fflush(stdout);
      }
    }
    g_grab_wait_n.store(wait);
    g_grab_req.store(std::max(1, std::min(8, bars)));
    return 0;
  }
  if (!strcmp(op, "SLC")) {
    c->op = OP_SLC;
    return sscanf(a, "%d %d %d %f %d", &c->i[0], &c->i[1], &c->i[2], &c->f[0], &c->i[3]) == 5;
  }
  if (!strcmp(op, "SL")) {
    c->op = OP_SL_ON;
    c->f[1] = 0; c->i[2] = 0;
    return sscanf(a, "%d %f %f %d", &c->i[0], &c->f[0], &c->f[1], &c->i[2]) >= 2;
  }
  if (!strcmp(op, "SLO")) { c->op = OP_SL_OFF; return sscanf(a, "%d", &c->i[0]) == 1; }
  if (!strcmp(op, "SLP")) { c->op = OP_SL_PRESS; return sscanf(a, "%d %f", &c->i[0], &c->f[0]) == 2; }
  if (!strcmp(op, "SLX")) { c->op = OP_SL_ALLOFF; return 1; }
  if (!strcmp(op, "SCRUB")) { c->op = OP_SCRUB; return sscanf(a, "%d", &c->i[0]) == 1; }
  if (!strcmp(op, "SCRUBPOS")) {
    float x;
    if (sscanf(a, "%f", &x) == 1) g_scrub_target.store(std::max(0.0f, std::min(1.0f, x)));
    return 0;
  }
  if (!strcmp(op, "CHOPTRK")) {  // CHOPTRK <track|-1>: that track's notes play slices (36 = slice 1)
    int t = -1;
    sscanf(a, "%d", &t);
    g_chop_track.store(t >= 0 && t < kTracks ? t : -1);
    return 0;
  }
  if (!strcmp(op, "CHOPCFG")) {  // CHOPCFG <motion> <drift>: for sequenced and N-played slices
    int m = 0; float dr = 0;
    sscanf(a, "%d %f", &m, &dr);
    g_chop_motion.store(std::max(0, std::min(2, m)));
    g_chop_drift.store(std::max(0.0f, std::min(1.0f, dr)));
    return 0;
  }
  if (!strcmp(op, "LOAD") || !strcmp(op, "LOADAT") || !strcmp(op, "LOADKIT") || !strcmp(op, "SAVE")) {  // path = rest of the line
    double off = 0;
    if (!strcmp(op, "LOADAT")) { char* e = nullptr; off = strtod(a, &e); if (e) a = e; op = "LOAD"; }
    while (*a == ' ') ++a;
    std::string path(a);
    while (!path.empty() && (path.back() == '\n' || path.back() == '\r' || path.back() == ' ')) path.pop_back();
    { std::lock_guard<std::mutex> lk(g_load_mu); g_load_path = path; g_load_off = std::max(0.0, off); }
    g_load_kind.store(!strcmp(op, "LOAD") ? 1 : !strcmp(op, "LOADKIT") ? 2 : 3);
    return 0;
  }
  if (!strcmp(op, "GZ")) {  // GZ <start> <end>: 128 level points of that stretch of the latest grab
    long st = 0, en = 0;
    if (sscanf(a, "%ld %ld", &st, &en) != 2) return 0;
    const float* g = g_grab[g_grab_next ^ 1];
    st = std::max(0L, std::min(kGrabFrames - 1, st));
    en = std::max(st + 128, std::min(kGrabFrames, en));
    char hex[128 * 4 + 1];
    float lp = 0;
    for (int p = 0; p < 128; ++p) {
      long a0 = st + (en - st) * p / 128, b0 = st + (en - st) * (p + 1) / 128;
      float lo = 0, hi = 0;
      for (long i = a0; i < b0; ++i) {
        float m = 0.5f * (g[2 * i] + g[2 * i + 1]);
        lp += (m - lp) * 0.032f;
        lo = std::max(lo, fabsf(lp));
        hi = std::max(hi, fabsf(m - lp));
      }
      snprintf(hex + 4 * p, 5, "%02x%02x", (unsigned char)LevelByte(lo * 1.6f), (unsigned char)LevelByte(hi * 1.6f));
    }
    printf("GZ %ld %ld %s\n", st, en, hex);
    fflush(stdout);
    return 0;
  }
  if (!strcmp(op, "TAPEMON")) {
    int on = 0;
    sscanf(a, "%d", &on);
    g_tapemon.store(on != 0);
    return 0;
  }
  return -1;  // not a sampler command
}
