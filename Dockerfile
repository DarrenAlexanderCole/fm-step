# On an Apple Silicon Mac this runs natively as linux/arm64 (same CPU
# architecture as the MPC Sample), so later C/C++ code builds with plain gcc.
#   ./build-on-mac.sh
FROM golang:1.24.7-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
      python3 make procps build-essential && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*
CMD ["make", "release"]
