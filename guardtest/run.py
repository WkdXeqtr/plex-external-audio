"""Runs the guard through its scenarios in a sandbox. The Plex directory here is fake.

Run this before changing anything in cmd/guard. The guard is allowed to delete
files in the real Plex directory, and it once deleted the real transcoder for
real - scenario 3 below is that exact bug, kept as a regression test.
"""
import hashlib, io, json, os, shutil, subprocess, sys

BASE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(BASE)
BIN = os.path.join(REPO, "bin")
GUARD = os.path.join(BIN, "Plex External Audio Guard.exe")
WRAPPER = os.path.join(BIN, "Plex External Audio Transcoder.exe")

for p in (GUARD, WRAPPER):
    if not os.path.exists(p):
        sys.exit("not built: " + p + "\nrun setup\\build.ps1 first")

# Any exe without our marker can play the part of "the real transcoder".
FAKE_ORIGINAL = next(
    (p for p in (
        os.path.join(os.environ.get("SystemRoot", r"C:\Windows"), "System32", "where.exe"),
        os.path.join(os.environ.get("SystemRoot", r"C:\Windows"), "System32", "find.exe"),
    ) if os.path.exists(p)),
    None,
)
if FAKE_ORIGINAL is None:
    sys.exit("could not find a suitable exe to stand in for the original")

PLEXDIR = os.path.join(BASE, "plexdir")
STATEDIR = os.path.join(BASE, "state")
LIVE = os.path.join(PLEXDIR, "Plex Transcoder.exe")
PARKED = os.path.join(PLEXDIR, "Plex Transcoder_org.exe")
CFG = os.path.join(BASE, "config.json")

# Must match wrapperMarker in the transcoder, the guard and the tray. Never
# change it - that is the whole point of scenario 3.
MARKER = b"PLEX-CUSTOM-AUDIO-WRAPPER-MARKER-e9f1c0a4-DO-NOT-PARK-THIS-FILE"


def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest().upper()


def is_ours(p):
    return MARKER in open(p, "rb").read() if os.path.exists(p) else None


def write_cfg(wrapper_hash):
    json.dump({
        "plexDir": PLEXDIR, "plexExe": "nonexistent.exe",
        "wrapperSrc": WRAPPER, "wrapperSha256": wrapper_hash,
        "mapperExe": "nonexistent.exe", "ffprobe": FAKE_ORIGINAL,
        "dbPath": os.path.join(BASE, "fake.db"), "stateDir": STATEDIR, "scanRoots": [],
    }, open(CFG, "w"), indent=2)


def guard():
    """Runs the guard and returns whatever it appended to the log.

    We read the log rather than stdout: the guard is built for the GUI subsystem
    so the scheduled task does not flash a console window, which means it prints
    nothing at all when started through a pipe.

    -force is essential here. Without it the guard honours its check interval,
    so every run after the first returns immediately, writes nothing, and
    changes nothing - and scenarios 2 to 5 end up asserting against an empty
    log while quietly testing nothing at all. That is exactly how this suite
    used to "pass"."""
    log = os.path.join(STATEDIR, "guard.log")
    before = os.path.getsize(log) if os.path.exists(log) else 0
    subprocess.run([GUARD, "-config", CFG, "-force"], capture_output=True)
    if not os.path.exists(log):
        return ""
    with io.open(log, encoding="utf-8", errors="replace") as f:
        f.seek(before)
        return f.read()


def reset():
    shutil.rmtree(PLEXDIR, ignore_errors=True)
    shutil.rmtree(STATEDIR, ignore_errors=True)
    os.makedirs(PLEXDIR, exist_ok=True)
    shutil.copy(FAKE_ORIGINAL, LIVE)
    # The guard now refuses to do anything while the tray's pause marker is set,
    # and that marker lives in the real user profile - so a developer who quit
    # the tray would get a test run where the guard silently does nothing and
    # every scenario fails for the wrong reason.
    paused = os.path.join(os.environ.get("LOCALAPPDATA", ""), "Plex External Audio", "paused")
    if os.path.exists(paused):
        sys.exit("the program is paused (Exit was used in the tray).\n"
                 "Start the tray icon, or delete: " + paused)


fails = []


def check(name, cond, detail=""):
    print(("  OK   " if cond else "  FAIL ") + name + (("  <- " + detail) if detail and not cond else ""))
    if not cond:
        fails.append(name)


print("=== 1. clean install: the stand-in original must move to _org ===")
reset()
original_hash = sha(LIVE)
write_cfg(sha(WRAPPER))
out = guard()
check("original parked unchanged", os.path.exists(PARKED) and sha(PARKED) == original_hash)
check("our wrapper sits where the transcoder was", is_ours(LIVE) is True)

print("\n=== 2. second run: nothing should move ===")
before = sha(PARKED)
out = guard()
check("original untouched", sha(PARKED) == before)
check("said there was nothing to do", "nothing to do" in out, out.strip()[-120:])

print("\n=== 3. THE BIG ONE: wrapper rebuilt, the hash in the config no longer matches ===")
print("    (this is exactly where the old guard killed the original)")
write_cfg("0000000000000000000000000000000000000000000000000000000000000000")
out = guard()
check("ORIGINAL SURVIVED", os.path.exists(PARKED) and sha(PARKED) == original_hash,
      "_org holds " + ("our wrapper!" if is_ours(PARKED) else "something else"))
check("wrapper updated rather than parked", "older build" in out or "nothing to do" in out, out.strip()[-160:])

print("\n=== 4. corrupted state: _org holds one of our wrappers ===")
shutil.copy(WRAPPER, PARKED)
out = guard()
check("guard refused to act", "refusing" in out.lower(), out.strip()[-160:])
check("deleted nothing", os.path.exists(PARKED) and os.path.exists(LIVE))

print("\n=== 5. Plex updated: the stock file is back where the transcoder goes ===")
reset()
write_cfg(sha(WRAPPER))
guard()                            # first install
shutil.copy(FAKE_ORIGINAL, LIVE)   # as if an update had overwritten it
out = guard()
check("new original parked", sha(PARKED) == original_hash)
check("wrapper back in place", is_ours(LIVE) is True)

print()
if fails:
    print("FAILED: " + ", ".join(fails))
    sys.exit(1)
print("All scenarios passed.")
