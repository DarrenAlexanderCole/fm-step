#!/usr/bin/env python3
"""Wrap a built card (dist/release-sd) into "Install FM-STEP.app" for macOS,
and (with --card-zip) also write the card-only update zip that a kept
installer picks up from Downloads, so macOS asks for approval only once.

The app carries the card files inside it (so macOS app translocation can't
separate them), asks before installing, runs tools/card.sh in package mode
(save the last test's logs, clear the card, copy, eject) and shows the result.
Usage: mac_app.py --card dist/release-sd --version 0.9.17 --out DIR
"""
import argparse, os, shutil, stat

PLIST = """<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Install FM-STEP</string>
  <key>CFBundleDisplayName</key><string>Install FM-STEP</string>
  <key>CFBundleIdentifier</key><string>com.mightycole.fmstep.install</string>
  <key>CFBundleVersion</key><string>2</string>
  <key>CFBundleShortVersionString</key><string>2 (bundled build {v})</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>install</string>
  <key>CFBundleIconFile</key><string>fmstep</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
"""

RUN = r"""#!/bin/sh
# Install FM-STEP to the SD card (double-clicked app). Approve this app once
# and keep it: it installs the newest build it can find, either the one inside
# it or a newer card zip / folder in Downloads (FM-STEP-<version>-card.zip),
# so later updates are plain zips and macOS never asks again.
R="$(cd "$(dirname "$0")/../Resources" && pwd)"
DL="$HOME/Downloads"
TMP="${TMPDIR:-/tmp}/fmstep-install.$$"
mkdir -p "$TMP"
LOG="$TMP/install.log"
: > "$LOG"
trap 'rm -rf "$TMP"' EXIT
dlg() { /usr/bin/osascript -e "$1" 2>/dev/null; }
q() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
ver_of() { sed -n 's/^# FM-STEP SD launcher (\(.*\))\./\1/p' "$1/autorun/autorun.sh" 2>/dev/null | head -n 1; }
newer() { awk -v a="$1" -v b="$2" 'BEGIN{n=split(a,x,".");split(b,y,".");for(i=1;i<=4;i++){p=x[i]+0;q=y[i]+0;if(p>q)exit 0;if(p<q)exit 1}exit 1}'; }

# the newest build: the one inside this app, then Downloads (folders and zips)
BEST="$R/card"; BV=$(ver_of "$BEST"); FROM="this installer"
consider() {
    v=$(ver_of "$1")
    [ -n "$v" ] || return 0
    if newer "$v" "$BV"; then BEST="$1"; BV="$v"; FROM="$2"; fi
}
# a zip or unzipped card folder -> consider it
consider_zip() {  # $1 zip, $2 label
    n=$((n + 1))
    /usr/bin/ditto -x -k "$1" "$TMP/z$n" 2>>"$LOG" || return 0
    find "$TMP/z$n" -maxdepth 6 -path '*/autorun/autorun.sh' 2>/dev/null | head -n 1 > "$TMP/f"
    f=$(cat "$TMP/f")
    [ -n "$f" ] && consider "$(dirname "$(dirname "$f")")" "$2"
    return 0
}
n=0
DLOK=1
if [ -d "$DL" ]; then
    # macOS may not let this app read Downloads (Privacy & Security): say so
    if ! ls "$DL" > /dev/null 2>>"$LOG"; then DLOK=0; echo "DOWNLOADS NOT READABLE" >> "$LOG"; fi
    find "$DL" -maxdepth 5 -path '*FM-STEP*' -path '*/autorun/autorun.sh' 2>>"$LOG" | while read -r f; do
        printf '%s\n' "$(dirname "$(dirname "$f")")"
    done > "$TMP/dirs"
    while read -r d; do consider "$d" "Downloads"; done < "$TMP/dirs"
    ls -t "$DL"/FM-STEP*.zip 2>/dev/null | head -n 5 > "$TMP/zips"
    while read -r z; do consider_zip "$z" "Downloads ($(basename "$z"))"; done < "$TMP/zips"
fi
echo "FOUND: $BV from $FROM (Downloads readable: $DLOK)" >> "$LOG"
xattr -cr "$BEST" 2>/dev/null

CARD=
for v in /Volumes/*; do [ -d "$v/FMSTEP" ] && { CARD="$v"; break; }; done
[ -n "$CARD" ] || { [ -d /Volumes/SAMPLES ] && CARD=/Volumes/SAMPLES; }
if [ -z "$CARD" ]; then
    dlg 'display dialog "No SD card found.\n\nPut the FM-STEP card in your Mac (it shows up as SAMPLES), then open Install FM-STEP again." with title "FM-STEP" buttons {"OK"} default button 1 with icon caution'
    exit 0
fi
NAME=$(basename "$CARD")
NOTE=""
[ "$DLOK" = 0 ] && NOTE="\n\nmacOS is not letting Install FM-STEP look in Downloads, so it can't see newer zips there. Use Choose zip, or allow it: System Settings › Privacy & Security › Files and Folders › Install FM-STEP › Downloads."
while :; do
    A=$(dlg 'display dialog "Install FM-STEP '"$BV"' on the card \"'"$(q "$NAME")"'\"?\n\nBuild from: '"$(q "$FROM")"'\n\nNot the version you expected? Choose zip and pick the FM-STEP card zip.'"$(q "$NOTE")"'\n\nYour last test'"'"'s logs are saved to Documents › FM-STEP test logs, the card is cleared for a fresh test, and it is ejected when done. Projects and sounds are kept." with title "FM-STEP" buttons {"Cancel", "Choose zip…", "Install"} default button 3 cancel button 1')
    case "$A" in
        *Install*) break ;;
        *Choose*)
            Z=$(/usr/bin/osascript -e 'POSIX path of (choose file with prompt "Pick the FM-STEP card zip (or a card folder'"'"'s autorun.sh)" default location (path to downloads folder))' 2>>"$LOG")
            [ -n "$Z" ] || continue
            echo "CHOSEN: $Z" >> "$LOG"
            OLDBV="$BV"
            BV=""; BEST=""
            case "$Z" in
                *.zip) consider_zip "$Z" "$(basename "$Z")" ;;
                */autorun/autorun.sh) consider "$(dirname "$(dirname "$Z")")" "$(basename "$(dirname "$(dirname "$Z")")")" ;;
            esac
            if [ -z "$BV" ]; then
                dlg 'display dialog "That file has no FM-STEP card in it. Pick the FM-STEP-x.x.x-card.zip." with title "FM-STEP" buttons {"OK"} default button 1 with icon caution'
                BEST="$R/card"; BV=$(ver_of "$BEST"); FROM="this installer"
            fi
            xattr -cr "$BEST" 2>/dev/null
            ;;
        *) exit 0 ;;
    esac
done

if FMSTEP_PKG="$BEST" sh "$R/card/card.sh" "$CARD" >> "$LOG" 2>&1; then
    OUT=$(sed -n 's/.*Saved last test.s logs: \(.*\)$/\1/p' "$LOG" | sed 's/\x1b\[[0-9;]*m//g' | tail -n 1)
    MSG="FM-STEP $BV is on the card and the card is ejected.\n\nPut it in the MPC and power on."
    if [ -n "$OUT" ]; then
        T=$(sed -n 's/^== launch to playable: \(.*\)$/\1/p' "$OUT/SUMMARY.txt" 2>/dev/null)
        MSG="$MSG\n\nLast test's logs saved."
        [ -n "$T" ] && MSG="$MSG\nLast launch to playable: $T"
        B=$(dlg 'display dialog "'"$(q "$MSG")"'" with title "FM-STEP" buttons {"Show logs", "OK"} default button 2')
        case "$B" in *Show*) open "$OUT" ;; esac
    else
        dlg 'display dialog "'"$(q "$MSG")"'" with title "FM-STEP" buttons {"OK"} default button 1'
    fi
else
    ERR=$(sed 's/\x1b\[[0-9;]*m//g' "$LOG" | tail -n 6)
    dlg 'display dialog "Something went wrong. Nothing was removed from your projects.\n\n'"$(q "$ERR")"'" with title "FM-STEP" buttons {"OK"} default button 1 with icon stop'
fi
mkdir -p "$HOME/Documents/FM-STEP test logs"
cp "$LOG" "$HOME/Documents/FM-STEP test logs/last-install.txt" 2>/dev/null
"""

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--card", required=True)
    ap.add_argument("--version", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--card-zip", help="also write FM-STEP-<version>-card.zip here")
    a = ap.parse_args()
    src = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    app = os.path.join(a.out, "Install FM-STEP.app")
    if os.path.exists(app):
        shutil.rmtree(app)
    c = os.path.join(app, "Contents")
    os.makedirs(os.path.join(c, "MacOS")); os.makedirs(os.path.join(c, "Resources"))
    open(os.path.join(c, "Info.plist"), "w").write(PLIST.format(v=a.version))
    exe = os.path.join(c, "MacOS", "install")
    open(exe, "w").write(RUN)
    shutil.copyfile(os.path.join(src, "tools", "icon", "fmstep.icns"), os.path.join(c, "Resources", "fmstep.icns"))
    card = os.path.join(c, "Resources", "card")
    shutil.copytree(a.card, card, ignore=shutil.ignore_patterns("state", "logs", "user"))
    shutil.copyfile(os.path.join(src, "tools", "card.sh"), os.path.join(card, "card.sh"))
    for p in (exe, os.path.join(card, "card.sh")):
        os.chmod(p, 0o755)
    print("ok", app)
    if a.card_zip:
        import zipfile
        name = f"FM-STEP-{a.version}-card"
        z = os.path.join(a.card_zip, name + ".zip")
        with zipfile.ZipFile(z, "w", zipfile.ZIP_DEFLATED) as zf:
            for root, dirs, files in os.walk(a.card):
                dirs[:] = [d for d in dirs if d not in ("state", "logs", "user")]
                for f in files:
                    p = os.path.join(root, f)
                    info = zipfile.ZipInfo(os.path.join(name, os.path.relpath(p, a.card)), (2026, 11, 5, 0, 0, 0))
                    info.external_attr = (os.stat(p).st_mode & 0xFFFF) << 16
                    info.compress_type = zipfile.ZIP_DEFLATED
                    zf.writestr(info, open(p, "rb").read())
        print("ok", z)

if __name__ == "__main__":
    main()
