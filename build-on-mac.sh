#!/bin/sh
# Build everything on a Mac with Docker Desktop installed and running.
# Results: dist/*.zip (copy the unzipped contents to the SD card root).
set -e
cd "$(dirname "$0")"
docker build --platform linux/arm64 -t fmstep-build .
docker run --rm --platform linux/arm64 -v "$PWD:/src" fmstep-build "$@"
