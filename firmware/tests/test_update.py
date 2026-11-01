import binascii
import hashlib
import json
import unittest

import ota
from app import update
from tests.test_ota import FakeFlash


def entry(path, data):
    return {"path": path, "size": len(data), "sha256": binascii.hexlify(hashlib.sha256(data).digest()).decode()}


FILES = {"main.py": b"new main", "app/runner.py": b"new runner"}
MANIFEST = {"version": "0.6.0", "files": [entry(p, d) for p, d in FILES.items()]}


def server(manifest=MANIFEST, files=FILES, broken=None):
    def get(path):
        if path == "":
            return 200, json.dumps(manifest).encode()
        if path == broken:
            return 500, None
        return (200, files[path]) if path in files else (404, None)

    return get


class DownloadTest(unittest.TestCase):
    def setUp(self):
        self.flash = FakeFlash({"main.py": b"old main"})
        self.fed = 0

    def feed(self):
        self.fed += 1

    def test_stages_every_file_and_marks_it_ready(self):
        self.assertEqual(update.download(server(), self.flash, self.feed), "0.6.0")
        self.assertEqual(self.flash.read(ota.NEXT + "/main.py"), b"new main")
        self.assertEqual(self.flash.read(ota.NEXT + "/app/runner.py"), b"new runner")
        self.assertEqual(json.loads(self.flash.read(ota.NEXT + "/" + ota.MANIFEST)),
                         {"version": "0.6.0", "files": ["main.py", "app/runner.py"]})
        self.assertEqual(self.flash.read("main.py"), b"old main")
        self.assertGreaterEqual(self.fed, 2)
        ota.boot(self.flash)
        self.assertEqual(self.flash.read("main.py"), b"new main")

    def test_a_damaged_file_stops_it_before_anything_is_ready(self):
        files = dict(FILES, **{"app/runner.py": b"new runnex"})
        with self.assertRaises(update.UpdateError):
            update.download(server(files=files), self.flash, self.feed)
        self.assertFalse(self.flash.exists(ota.NEXT + "/" + ota.MANIFEST))

    def test_a_short_file_is_damaged_too(self):
        files = dict(FILES, **{"main.py": b"new"})
        with self.assertRaises(update.UpdateError):
            update.download(server(files=files), self.flash, self.feed)

    def test_server_errors_stop_it(self):
        with self.assertRaises(update.UpdateError):
            update.download(server(broken="app/runner.py"), self.flash, self.feed)
        self.assertFalse(self.flash.exists(ota.NEXT + "/" + ota.MANIFEST))

    def test_refuses_files_outside_the_firmware(self):
        manifest = {"version": "0.6.0", "files": [entry("config.json", b"{}")]}
        with self.assertRaises(update.UpdateError):
            update.download(server(manifest, {"config.json": b"{}"}), self.flash, self.feed)
        self.assertFalse(self.flash.exists(ota.NEXT + "/config.json"))

    def test_refuses_a_manifest_it_cannot_read(self):
        for manifest in ({}, {"version": "0.6.0"}, {"version": "0.6.0", "files": [{"path": "main.py"}]},
                         {"version": "", "files": []}, ["x"]):
            with self.assertRaises(update.UpdateError, msg=manifest):
                update.download(server(manifest), self.flash, self.feed)
