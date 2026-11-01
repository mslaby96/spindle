# Putting a firmware the server sent in place at boot, and going back to the previous one when it doesn't come up.
# Kept outside app/ and free of other imports, so a broken app can still be rolled back.
import json
import os

NEXT = "next"
PREV = "prev"
MANIFEST = "manifest.json"
STATE = "ota.json"
# Boots a new version may take without reaching the server; every crash ends in a watchdog reset, i.e. a boot.
MAX_TRIES = 3
_DIRS = ("", "app/", "lib/")


def safe_path(path):
    """Only the stand's own code may be written: Python files at the top or in app/ and lib/, nothing else."""
    if not path.endswith(".py") or path.startswith("/") or ".." in path:
        return False
    head, _, name = path.rpartition("/")
    if path.count("/") > 1 or not name[:-3]:
        return False
    return (head + "/" if head else "") in _DIRS and (head != "" or name in ("main.py", "ota.py"))


def boot(flash):
    _install(flash)
    st = _state(flash)
    if "trying" not in st:
        return
    st["tries"] = st.get("tries", 0) + 1
    if st["tries"] > MAX_TRIES:
        rollback(flash)
    else:
        _save(flash, st)


def _install(flash):
    staged = NEXT + "/" + MANIFEST
    if not flash.exists(staged):
        return
    try:
        manifest = json.loads(flash.read(staged))
        version, files = manifest["version"], [p for p in manifest["files"] if safe_path(p)]
    except (ValueError, KeyError, TypeError):
        flash.remove(staged)
        return
    for path in files:
        # each file is checked on its own, so a boot that lost power halfway carries on where it stopped
        new = NEXT + "/" + path
        if not flash.exists(new):
            continue
        if flash.exists(path):
            _replace(flash, path, PREV + "/" + path)
        flash.rename(new, path)
    _save(flash, {"trying": version, "tries": 0, "files": files})
    flash.remove(staged)


def _replace(flash, src, dst):
    if flash.exists(dst):
        flash.remove(dst)
    flash.rename(src, dst)


def rollback(flash):
    st = _state(flash)
    for path in st.get("files", []):
        old = PREV + "/" + path
        if flash.exists(old):
            _replace(flash, old, path)
    _save(flash, {"failed": st.get("trying")})


def confirm(flash):
    """The new version reached the server: keep it."""
    st = _state(flash)
    if "trying" not in st:
        return
    for path in st.get("files", []):
        if flash.exists(PREV + "/" + path):
            flash.remove(PREV + "/" + path)
    _save(flash, {})


def trying(flash):
    return "trying" in _state(flash)


def failed(flash):
    return _state(flash).get("failed")


def mark_failed(flash, version):
    _save(flash, {"failed": version})


def clear(flash):
    _save(flash, {})


def _state(flash):
    if not flash.exists(STATE):
        return {}
    try:
        st = json.loads(flash.read(STATE))
    except ValueError:
        return {}
    return st if isinstance(st, dict) else {}


def _save(flash, st):
    flash.write(STATE, json.dumps(st).encode())


class Flash:
    """The Pico's file system."""

    def exists(self, path):
        try:
            os.stat(path)
            return True
        except OSError:
            return False

    def read(self, path):
        with open(path, "rb") as f:
            return f.read()

    def write(self, path, data):
        self._dirs_for(path)
        with open(path, "wb") as f:
            f.write(data)

    def rename(self, src, dst):
        self._dirs_for(dst)
        os.rename(src, dst)

    def remove(self, path):
        os.remove(path)

    def _dirs_for(self, path):
        parts = path.split("/")[:-1]
        for i in range(len(parts)):
            d = "/".join(parts[: i + 1])
            if not self.exists(d):
                os.mkdir(d)
