#!/usr/bin/env python3
"""Assemble the FM-STEP SD-card package deterministically.

Output layout (copy the contents of OUT to the root of the SD card):

  autorun/autorun.json   runner manifest (asks the user before running)
  autorun/autorun.sh     launcher, with firmware + payload hashes embedded
  autorun/autorun.check  sha256(autorun.json + autorun.sh + salt), lowercase hex
  FMSTEP/bin/...         supervisor, app, restore script (verified by autorun.sh)
  FMSTEP/fmstep.conf     supervisor settings
  FMSTEP/README.txt      install, enable, exit and recovery instructions
  (no FMSTEP/ENABLE: the user creates it to opt in; --enable for tests)

autorun.json / autorun.check formats follow am-mod's sd-clone-trial/build.py
(MIT). The salt is the published constant used by the stock autorun runner.
"""
import argparse, hashlib, json, os, shutil, stat, sys, zipfile

SALT = b"f7bed1c9-7e90-4731-9194-c996666b4331"
COMPAT_DEFAULT = [
    # version, sha256("/usr/bin/MPC Sample"), sha256(/usr/lib/systemd/system/ac50.service)
    ("1.3.0.12",
     "b6584cba76e416e432ad202509099d34373dc2c3d6a8fb625f5678bf0193b508",
     "6b2dc52826263daf19c6f1a945c94bd7ba69b15eb7843df68eebdd63ea311e5d"),
]
PROMPT = ("Start FM-STEP? Save your project first. Exit to Stock returns here. "
          "If anything goes wrong: hold POWER until off, remove the card, power on.")

def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", required=True)
    ap.add_argument("--supervisor", required=True)
    ap.add_argument("--app", required=True)
    ap.add_argument("--dsp", help="fmstep-dsp sound engine binary (optional)")
    ap.add_argument("--src", default=os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    ap.add_argument("--out", required=True)
    ap.add_argument("--zip")
    ap.add_argument("--compat", help="file with lines: version exe_sha unit_sha (tests only)")
    ap.add_argument("--no-ask", action="store_true", help="do not ask before running (not recommended)")
    ap.add_argument("--enable", action="store_true", help="create FMSTEP/ENABLE (tests only)")
    a = ap.parse_args()

    compat = COMPAT_DEFAULT
    if a.compat:
        compat = [tuple(l.split()) for l in open(a.compat) if l.strip() and not l.startswith("#")]

    if os.path.exists(a.out):
        shutil.rmtree(a.out)
    fm = os.path.join(a.out, "FMSTEP"); ar = os.path.join(a.out, "autorun")
    os.makedirs(os.path.join(fm, "bin")); os.makedirs(ar)
    os.makedirs(os.path.join(fm, "state")); os.makedirs(os.path.join(fm, "logs")); os.makedirs(os.path.join(fm, "user"))

    payload = {
        "bin/fmstep-supervisor": a.supervisor,
        "bin/fmstep-app": a.app,
        "bin/restore-stock.sh": os.path.join(a.src, "launcher", "restore-stock.sh"),
    }
    if a.dsp:
        payload["bin/fmstep-dsp"] = a.dsp
    lines = []
    for rel, src in sorted(payload.items()):
        dst = os.path.join(fm, rel)
        shutil.copyfile(src, dst)
        lines.append(f"{sha(dst)} {rel}")
    shutil.copyfile(os.path.join(a.src, "launcher", "fmstep.conf"), os.path.join(fm, "fmstep.conf"))
    shutil.copyfile(os.path.join(a.src, "docs", "SD_README.txt"), os.path.join(fm, "README.txt"))

    tmpl = open(os.path.join(a.src, "launcher", "autorun.sh.in")).read()
    sh = (tmpl.replace("@VERSION@", a.version)
              .replace("@COMPAT@", "\n".join(" ".join(c) for c in compat))
              .replace("@PAYLOAD@", "\n".join(lines)))
    if "@VERSION@" in sh or "@COMPAT@" in sh or "@PAYLOAD@" in sh:
        sys.exit("template placeholders left unreplaced")
    shb = sh.encode("ascii")
    manifest = {"value0": {"scripts": [{
        "path": "autorun.sh", "askQuestion": not a.no_ask, "message": PROMPT,
        "yesButtonText": "Start FM-STEP", "noButtonText": "Stock"}], "files": []}}
    js = (json.dumps(manifest, indent=2) + "\n").encode("ascii")
    open(os.path.join(ar, "autorun.json"), "wb").write(js)
    open(os.path.join(ar, "autorun.sh"), "wb").write(shb)
    open(os.path.join(ar, "autorun.check"), "wb").write(hashlib.sha256(js + shb + SALT).hexdigest().encode("ascii"))
    if a.enable:
        open(os.path.join(fm, "ENABLE"), "w").close()

    if a.zip:
        epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "1767225600"))
        import time
        dt = time.gmtime(max(epoch, 315532800))[:6]
        with zipfile.ZipFile(a.zip, "w", zipfile.ZIP_DEFLATED) as z:
            for root, dirs, files in os.walk(a.out):
                dirs.sort()
                for name in sorted(files) + [d + "/" for d in sorted(dirs) if not os.listdir(os.path.join(root, d))]:
                    full = os.path.join(root, name.rstrip("/"))
                    rel = os.path.relpath(full, a.out) + ("/" if name.endswith("/") else "")
                    zi = zipfile.ZipInfo(rel, dt)
                    zi.external_attr = ((0o40755 if rel.endswith("/") else 0o100644) << 16)
                    z.writestr(zi, b"" if rel.endswith("/") else open(full, "rb").read())
        print(f"{sha(a.zip)}  {os.path.basename(a.zip)}")
    print("payload:"); print("\n".join("  " + l for l in lines))

if __name__ == "__main__":
    main()
