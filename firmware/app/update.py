# Downloading a firmware the server offers. Nothing is put in place here: ota.boot() does that after a restart, once
# every file has arrived whole.
import binascii
import hashlib
import json

import ota


class UpdateError(Exception):
    pass


def download(get, flash, feed):
    """get(path) fetches from the server's firmware endpoint, "" being the manifest. Returns the version staged."""
    version, files = _manifest(get)
    for f in files:
        status, data = get(f["path"])
        feed()
        if status != 200 or data is None:
            raise UpdateError("{} answered {}".format(f["path"], status))
        if len(data) != f["size"] or binascii.hexlify(hashlib.sha256(data).digest()).decode() != f["sha256"]:
            raise UpdateError(f["path"] + " arrived damaged")
        flash.write(ota.NEXT + "/" + f["path"], data)
        feed()
    manifest = {"version": version, "files": [f["path"] for f in files]}
    flash.write(ota.NEXT + "/" + ota.MANIFEST, json.dumps(manifest).encode())
    return version


def _manifest(get):
    status, body = get("")
    if status != 200 or body is None:
        raise UpdateError("manifest answered {}".format(status))
    try:
        m = json.loads(body)
        version, files = m["version"], m["files"]
        for f in files:
            if not ota.safe_path(f["path"]) or not isinstance(f["size"], int) or len(f["sha256"]) != 64:
                raise UpdateError("unexpected file " + str(f["path"]))
    except (ValueError, KeyError, TypeError):
        raise UpdateError("unreadable manifest")
    if not version or not isinstance(version, str):
        raise UpdateError("manifest has no version")
    return version, files
