# Building FM-STEP

Three programs end up on the card:

| Binary | Language | Built with |
|---|---|---|
| `fmstep-supervisor` (launch, watchdog, recovery) | Go | cross-compiles anywhere: `GOOS=linux GOARCH=arm64` |
| `fmstep-app` (UI, sequencer state, controls, screen) | Go | same |
| `fmstep-dsp` (Plaits voices, effects, sequencer clock, audio, spectrum) | C++ | **native arm64 Linux + gcc** (`dsp/build.sh`) |

## One command (Apple Silicon Mac + Docker Desktop)

```sh
./build-on-mac.sh        # docker --platform linux/arm64 … make release
```

Output: `dist/fmstep-<version>-sd.zip` and its `.sha256`. The image is `golang:1.24.7-bookworm` (arm64) with gcc and git.

## By hand (any arm64 Linux: Raspberry Pi 4/5, an arm64 VM, …)

Needs Go 1.24.x, gcc/g++, git, python3, make.

```sh
make test        # go vet, go test, host recovery simulation (25 cases)
make dsp         # dsp/build.sh: fetch Plaits/Clouds/stmlib at pinned commits, build fmstep-dsp
make release     # test + arm64 Go binaries + dsp + dist/fmstep-<version>-sd.zip (OFF until FMSTEP/ENABLE exists)
```

On an x86 machine you can build and test the Go parts (`make test arm64`); build `fmstep-dsp` on an arm64 host (or in an arm64 container) and copy it to `build/arm64/fmstep-dsp` before packaging.

## Reproducibility

- Go: `CGO_ENABLED=0`, `-trimpath`, empty build IDs, fixed archive timestamps (`SOURCE_DATE_EPOCH`); two clean builds give identical binaries.
- DSP: upstream sources pinned in `dsp/build.sh` (eurorack `f4cfa76…`, stmlib `d18def8…`). A from-scratch build with these pins rendered audio **bit-identical** to the binary tested on hardware (offline render compared byte for byte, 2026-11-05).

## Offline renderer (audition the engine on a computer)

`fmstep-dsp --render out.wav script.txt` renders the engine without the MPC. Scripts are the engine's normal commands plus `W <seconds>`:

```
P master 0.8
T 0 engine 8
T 0 sustain 1
N 45 0.8
W 2
```

`dsp/make_seq_demo.py` writes longer sequencer demos; `FMSTEP_METER=1` prints stage levels.

## Other tools

- `tools/gen_fonts.py` — pre-rasterise Inter into `internal/gfx/fonts_gen.go`.
- `tools/make_gifs.py` — render the interface in motion to `docs/press/media/*.gif` (the app's own drawing code).
- `tools/press_art/render.py`, `tools/press_pdf.py` — press page illustrations and the PDF report.
- `FMSTEP_PREVIEW=dir go test ./cmd/fmstep -run TestPreviewV2` — every screen as PNG.
