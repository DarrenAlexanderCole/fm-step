# Reproducible build: pinned Go toolchain (see Dockerfile), no cgo, trimmed
# paths, empty build IDs, fixed archive timestamps.
VERSION ?= 0.1.0-recovery
SOURCE_DATE_EPOCH ?= 1767225600
GOFLAGS_REPRO := -trimpath -buildvcs=false -ldflags="-s -w -buildid="
export SOURCE_DATE_EPOCH CGO_ENABLED=0

.PHONY: all arm64 host test package probe lab synth club dsp release clean
APP_VERSION := 0.9.15
all: package

arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS_REPRO) -o build/arm64/fmstep-supervisor ./cmd/fmstep-supervisor
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS_REPRO) -o build/arm64/fmstep-stub ./cmd/fmstep-stub
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS_REPRO) -o build/arm64/fmstep-probe ./cmd/fmstep-probe
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS_REPRO) -o build/arm64/fmstep-lab ./cmd/fmstep-lab
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS_REPRO) -o build/arm64/fmstep ./cmd/fmstep

host:
	go build $(GOFLAGS_REPRO) -o build/host/fmstep-supervisor ./cmd/fmstep-supervisor
	go build $(GOFLAGS_REPRO) -o build/host/fmstep-stub ./cmd/fmstep-stub

test: host
	go vet ./...
	go test ./...
	python3 tests/test_recovery.py

# Phase-2 package: the "app" is the stub until the FM engine exists.
package: arm64
	python3 tools/package.py --version $(VERSION) \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep-stub \
	  --out dist/sd --zip dist/fmstep-$(VERSION)-sd.zip
	cd build/arm64 && sha256sum fmstep-supervisor fmstep-stub > ../../dist/SHA256SUMS.bin

# Probe package: first device run. ENABLE is pre-created; the MPC still asks Yes/No.
probe: arm64
	python3 tools/package.py --version $(VERSION)-probe \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep-probe \
	  --out dist/probe-sd --zip dist/fmstep-$(VERSION)-probe-sd.zip --enable

# Lab package: controls capture, first FM sound, recovery tests (FMSTEP/lab.txt).
lab: arm64
	python3 tools/package.py --version $(VERSION)-lab \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep-lab \
	  --out dist/lab-sd --zip dist/fmstep-$(VERSION)-lab-sd.zip --enable
	printf 'exit\n' > dist/lab-sd/FMSTEP/lab.txt

# Synth package: the FM-STEP app itself (Phase 4).
synth: arm64
	python3 tools/package.py --version 0.4.0-synth \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep \
	  --out dist/synth-sd --zip dist/fmstep-0.4.0-synth-sd.zip --enable

# FM-STEP 0.7: 8-track groovebox (Plaits + dub/club engine + sequencer). build/arm64/fmstep-dsp is built on an
# aarch64 machine (make dsp / dsp/build.sh) and copied in.
club: arm64
	python3 tools/package.py --version $(APP_VERSION) \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep \
	  --dsp build/arm64/fmstep-dsp \
	  --out dist/club-sd --zip dist/fmstep-$(APP_VERSION)-sd.zip --enable

# The sound engine (C++). Needs native arm64 Linux with gcc and git: a
# Raspberry Pi, an arm64 VM, or Docker on an Apple Silicon Mac
# (./build-on-mac.sh release). Fetches Plaits/Clouds/stmlib at pinned commits.
dsp:
	sh dsp/build.sh
	mkdir -p build/arm64 && cp dsp/fmstep-dsp build/arm64/fmstep-dsp

# Public release: everything from source, tests, and an SD-card zip that is
# OFF until the user creates FMSTEP/ENABLE.
release: test arm64 dsp
	python3 tools/package.py --version $(APP_VERSION) \
	  --supervisor build/arm64/fmstep-supervisor --app build/arm64/fmstep \
	  --dsp build/arm64/fmstep-dsp \
	  --out dist/release-sd --zip dist/fmstep-$(APP_VERSION)-sd.zip
	cd dist && sha256sum fmstep-$(APP_VERSION)-sd.zip > fmstep-$(APP_VERSION)-sd.zip.sha256

clean:
	rm -rf build dist
