#!/bin/sh
# Builds fmstep-dsp (the C++ sound engine) natively on arm64 Linux with gcc:
# a Raspberry Pi 4/5, any arm64 Linux box or VM (e.g. on an Apple Silicon Mac),
# or `docker run --platform linux/arm64`. Output: dsp/fmstep-dsp (static).
#
# Mutable Instruments' Plaits, Clouds and stmlib (MIT, by Emilie Gillet) are
# fetched at pinned commits into dsp/third_party (not stored in this repo).
set -e
EURORACK=f4cfa7697095a28d292bff758beb51deb49b5a86
STMLIB=d18def816c51d1da0c108236928b2bbd25c17481
cd "$(dirname "$0")"
TP=third_party
mkdir -p "$TP"
[ -d "$TP/eurorack/.git" ] || git clone -q https://github.com/pichenettes/eurorack "$TP/eurorack"
git -C "$TP/eurorack" checkout -q "$EURORACK"
[ -d "$TP/stmlib/.git" ] || git clone -q https://github.com/pichenettes/stmlib "$TP/stmlib"
git -C "$TP/stmlib" checkout -q "$STMLIB"
SRC="$TP/src"
mkdir -p "$SRC"
ln -sfn ../eurorack/plaits "$SRC/plaits"
ln -sfn ../eurorack/clouds "$SRC/clouds"
ln -sfn ../stmlib "$SRC/stmlib"

CXX=${CXX:-g++}
FLAGS="-DTEST -O2 -mcpu=cortex-a55 -ffast-math -fno-exceptions -fno-rtti -include cstdio"
OBJ="$TP/obj"
mkdir -p "$OBJ"
# Plaits engines (same file list as the device builds)
for f in \
  stmlib/utils/random.cc stmlib/dsp/units.cc \
  plaits/resources.cc plaits/dsp/voice.cc \
  plaits/dsp/chords/chord_bank.cc \
  plaits/dsp/engine/additive_engine.cc plaits/dsp/engine/bass_drum_engine.cc plaits/dsp/engine/chord_engine.cc \
  plaits/dsp/engine/fm_engine.cc plaits/dsp/engine/grain_engine.cc plaits/dsp/engine/hi_hat_engine.cc \
  plaits/dsp/engine/modal_engine.cc plaits/dsp/engine/noise_engine.cc plaits/dsp/engine/particle_engine.cc \
  plaits/dsp/engine/snare_drum_engine.cc plaits/dsp/engine/speech_engine.cc plaits/dsp/engine/string_engine.cc \
  plaits/dsp/engine/swarm_engine.cc plaits/dsp/engine/virtual_analog_engine.cc plaits/dsp/engine/waveshaping_engine.cc \
  plaits/dsp/engine/wavetable_engine.cc \
  plaits/dsp/engine2/chiptune_engine.cc plaits/dsp/engine2/phase_distortion_engine.cc plaits/dsp/engine2/six_op_engine.cc \
  plaits/dsp/engine2/string_machine_engine.cc plaits/dsp/engine2/virtual_analog_vcf_engine.cc plaits/dsp/engine2/wave_terrain_engine.cc \
  plaits/dsp/fm/algorithms.cc plaits/dsp/fm/dx_units.cc \
  plaits/dsp/physical_modelling/modal_voice.cc plaits/dsp/physical_modelling/resonator.cc \
  plaits/dsp/physical_modelling/string.cc plaits/dsp/physical_modelling/string_voice.cc \
  plaits/dsp/speech/lpc_speech_synth.cc plaits/dsp/speech/lpc_speech_synth_controller.cc \
  plaits/dsp/speech/lpc_speech_synth_phonemes.cc plaits/dsp/speech/lpc_speech_synth_words.cc \
  plaits/dsp/speech/naive_speech_synth.cc plaits/dsp/speech/sam_speech_synth.cc
do
  o="$OBJ/$(echo "$f" | tr / _ | sed 's/\.cc$/.o/')"
  [ "$o" -nt "$SRC/$f" ] 2>/dev/null || $CXX -c $FLAGS -Wno-unused-variable -I"$SRC" "$SRC/$f" -o "$o"
done
ar rcs "$TP/libplaits.a" "$OBJ"/*.o
$CXX $FLAGS -Wall -Wno-unused-local-typedefs -Wno-unused-variable -I. -I"$SRC" -static -pthread \
  fmstep_dsp.cc "$TP/libplaits.a" -o fmstep-dsp
strip fmstep-dsp
sha256sum fmstep-dsp
