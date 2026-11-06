#!/bin/sh
# FM-STEP test-card update (macOS). One command between hardware tests:
#
#   ./tools/card.sh              build, save the last test's logs, clear the
#                                card for a fresh run, copy the new build, eject
#   ./tools/card.sh --no-build   same, using the build already in dist/
#   ./tools/card.sh --clear      only save logs + clear (no new build copied)
#   ./tools/card.sh --fresh      also forget the cached firmware check, so the
#                                next launch does the full 2.4 s hash
#   ./tools/card.sh --keep       don't eject at the end
#   ./tools/card.sh /Volumes/X   use this card (default: the card that already
#                                has FMSTEP on it, else /Volumes/SAMPLES)
#
# Never touched: FMSTEP/user (projects, sounds, tour progress).
# The last test's logs and state are copied to test-logs/<time>-<version>/
# before the card is cleared, with a short timing summary.
set -e
HERE=$(cd "$(dirname "$0")" && pwd)
# Inside a card package ("Install to card.command" next to autorun/ and
# FMSTEP/): no build, copy that package, logs go to Documents.
# FMSTEP_PKG=<card folder> picks a different package (the installer app uses
# it for builds found in Downloads).
if [ -n "$FMSTEP_PKG" ]; then HERE="$FMSTEP_PKG"; fi
if [ -f "$HERE/autorun/autorun.sh" ] && [ ! -f "$HERE/Makefile" ]; then
    PKG="$HERE"; SRC="$HERE"; LOGROOT="$HOME/Documents/FM-STEP test logs"
    VERSION=$(sed -n 's/^# FM-STEP SD launcher (\(.*\))\./\1/p' "$PKG/autorun/autorun.sh" | head -n 1)
    BUILD=0
else
    cd "$HERE/.."
    SRC=$(pwd); PKG="$SRC/dist/release-sd"; LOGROOT="$SRC/test-logs"
    VERSION=$(sed -n 's/^APP_VERSION := *//p' Makefile)
    BUILD=1
fi

COPY=1 FRESH=0 EJECT=1 CARD=
for a in "$@"; do
    case "$a" in
        --no-build) BUILD=0 ;;
        --clear) BUILD=0 COPY=0 ;;
        --fresh) FRESH=1 ;;
        --keep) EJECT=0 ;;
        -h|--help) sed -n 2,17p "$0"; exit 0 ;;
        /*) CARD="$a" ;;
        *) echo "unknown option: $a (try --help)"; exit 2 ;;
    esac
done

say() { printf '\033[1m%s\033[0m\n' "$*"; }
die() { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }

# --- find the card -----------------------------------------------------------
if [ -z "$CARD" ]; then
    for v in /Volumes/*; do
        [ -d "$v/FMSTEP" ] && { CARD="$v"; break; }
    done
fi
[ -n "$CARD" ] || CARD=/Volumes/SAMPLES
[ -d "$CARD" ] || die "No card at $CARD. Insert the SD card (or pass its path, e.g. /Volumes/SAMPLES)."
case "$CARD" in /Volumes/Macintosh*|/) die "Refusing to use $CARD" ;; esac
FM="$CARD/FMSTEP"
say "Card: $CARD    Build: $VERSION"

# --- 1. build ----------------------------------------------------------------
if [ "$BUILD" = 1 ]; then
    say "1/4 Building (Docker, make release)…"
    ./build-on-mac.sh
fi
NEW="$PKG"
if [ "$COPY" = 1 ]; then
    [ -f "$NEW/autorun/autorun.sh" ] && [ -d "$NEW/FMSTEP/bin" ] || die "No build in dist/release-sd. Run without --no-build."
    grep -q "version=$VERSION" "$NEW/autorun/autorun.sh" 2>/dev/null || grep -q "($VERSION)" "$NEW/autorun/autorun.sh" \
        || say "   note: dist/release-sd is not $VERSION; copying it anyway"
    ZIP="$SRC/dist/fmstep-$VERSION-sd.zip"
    if [ "$PKG" != "$SRC" ] && [ -f "$ZIP.sha256" ]; then
        (cd "$SRC/dist" && shasum -a 256 -c "fmstep-$VERSION-sd.zip.sha256" >/dev/null) || die "Checksum mismatch for $ZIP"
    fi
fi

# --- 2. save the last test's logs ---------------------------------------------
if [ -d "$FM/logs" ] && [ -n "$(ls -A "$FM/logs" 2>/dev/null | grep -v '^\._')" ]; then
    PREV=$(sed -n 's/.*LAUNCHER START version=\([^ ]*\).*/\1/p' "$FM/logs/launcher.log" 2>/dev/null | tail -n 1)
    OUT="$LOGROOT/$(date +%Y-%m-%d_%H%M)-${PREV:-unknown}"
    mkdir -p "$OUT"
    cp -R "$FM/logs/." "$OUT/" 2>/dev/null || true
    [ -d "$FM/state" ] && mkdir -p "$OUT/state" && cp -R "$FM/state/." "$OUT/state/" 2>/dev/null || true
    [ -f "$FM/DISABLED_BY_BREAKER.txt" ] && cp "$FM/DISABLED_BY_BREAKER.txt" "$OUT/"
    find "$OUT" -name '._*' -delete 2>/dev/null || true
    {
        echo "FM-STEP test summary (version ${PREV:-unknown}, saved $(date))"
        echo
        echo "== launcher (last 3 launches)"
        grep -E 'LAUNCHER (START|FIRMWARE_OK|PAYLOAD_OK|SUPERVISOR_STARTED|STAY_STOCK)' "$OUT/launcher.log" 2>/dev/null | tail -n 15
        echo
        echo "== supervisor"
        grep -E 'SUPERVISOR_START|STOCK_STOPPED|APP_STARTED|APP_READY|APP_HEALTHY|APP_ENDED|APP_KILL|STOCK_RESTORED|BREAKER|RESTART' "$OUT/recovery.log" 2>/dev/null | tail -n 15
        echo
        echo "== app startup"
        grep -E 'FMSTEP_START|INTRO_BUDGET|INTRO_DONE|DSP_READY|CONTROL_SURFACE|STARTUP |DSP_FAILED|SCREEN_FAILED' "$OUT/app.log" 2>/dev/null | tail -n 12
        echo
        echo "== audio / screen (last)"
        grep 'DSP STATS' "$OUT/app.log" 2>/dev/null | tail -n 2
        grep 'UI_STATS' "$OUT/app.log" 2>/dev/null | tail -n 2
        echo
        echo "== problems"
        grep -E -i 'FAIL|ERROR|panic|xruns=[1-9]|KILL|STAY_STOCK' "$OUT"/*.log 2>/dev/null | grep -v 'xruns=0' | tail -n 20 || true
        # launch time: launcher START uptime -> app READY uptime (same boot)
        LS=$(sed -n 's/.* up=\([0-9.]*\) LAUNCHER START.*/\1/p' "$OUT/launcher.log" 2>/dev/null | tail -n 1)
        AR=$(sed -n 's/.*STARTUP .*playable_ms=[0-9]* uptime=\([0-9.]*\).*/\1/p' "$OUT/app.log" 2>/dev/null | tail -n 1)
        if [ -n "$LS" ] && [ -n "$AR" ]; then
            echo
            echo "== launch to playable: $(echo "$AR - $LS" | bc) s (budget 5 s)"
        fi
    } > "$OUT/SUMMARY.txt"
    say "2/4 Saved last test's logs: $OUT"
    sed -n '/== launch to playable/p' "$OUT/SUMMARY.txt"
    if [ ! -s "$OUT/app.log" ] || ! grep -q "EXIT_TO_STOCK done" "$OUT/app.log" 2>/dev/null; then
        say "   note: the last session has no clean exit in app.log (power-off or card pulled?)"
    fi
else
    say "2/4 No logs on the card to save"
fi

# --- 3. clear the card for a fresh run ------------------------------------------
say "3/4 Clearing logs and launch state"
mkdir -p "$FM/logs" "$FM/state" "$FM/user"
rm -f "$FM"/logs/* "$FM"/logs/._* 2>/dev/null || true
rm -f "$FM/state/launch_pending" "$FM/state/last_result" "$FM/state/crash_count" "$FM/DISABLED_BY_BREAKER.txt"
echo 0 > "$FM/state/consecutive_failures"
[ "$FRESH" = 1 ] && rm -f "$FM/state/firmware.cache" && echo "   firmware check cache cleared (next launch hashes in full)"

# --- 4. copy the new build -------------------------------------------------------
if [ "$COPY" = 1 ]; then
    say "4/4 Copying $VERSION to the card"
    rm -rf "$CARD/autorun" "$FM/bin"
    cp -R "$NEW/autorun" "$CARD/autorun"
    cp -R "$NEW/FMSTEP/bin" "$FM/bin"
    if [ -f "$FM/fmstep.conf" ] && ! cmp -s "$FM/fmstep.conf" "$NEW/FMSTEP/fmstep.conf"; then
        cp "$FM/fmstep.conf" "$FM/fmstep.conf.prev"
        echo "   fmstep.conf replaced (your old one is fmstep.conf.prev)"
    fi
    cp "$NEW/FMSTEP/fmstep.conf" "$FM/fmstep.conf"
    cp "$NEW/FMSTEP/README.txt" "$FM/README.txt"
else
    say "4/4 Skipped copying (--clear)"
fi
touch "$FM/ENABLE"

dot_clean "$CARD/autorun" 2>/dev/null || true
dot_clean "$FM" 2>/dev/null || true
find "$CARD/autorun" "$FM" -name '._*' -delete 2>/dev/null || true
sync
if [ "$EJECT" = 1 ]; then
    diskutil eject "$CARD" >/dev/null && say "Done. Card ejected: put it in the MPC and power on."
else
    say "Done. Card still mounted ($CARD)."
fi
