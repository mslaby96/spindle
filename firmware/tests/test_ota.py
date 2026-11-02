import json
import unittest

import ota


class FakeFlash:
    """Files by path; renaming onto an existing file fails, as the code must not rely on overwriting."""

    def __init__(self, files=None):
        self.files = dict(files or {})

    def exists(self, path):
        return path in self.files

    def read(self, path):
        return self.files[path]

    def write(self, path, data):
        self.files[path] = data

    def rename(self, src, dst):
        if dst in self.files:
            raise OSError("exists")
        self.files[dst] = self.files.pop(src)

    def remove(self, path):
        del self.files[path]

    def state(self):
        return json.loads(self.files[ota.STATE]) if ota.STATE in self.files else None


OLD = {"app/version.py": b"old version", "app/runner.py": b"old runner", "config.json": b"{}", "oled.py": b"mine"}


def staged(version="0.6.0", files=None):
    files = files or {"app/version.py": b"new version", "app/runner.py": b"new runner"}
    flash = FakeFlash(OLD)
    for path, data in files.items():
        flash.write(ota.NEXT + "/" + path, data)
    flash.write(ota.NEXT + "/" + ota.MANIFEST, json.dumps({"version": version, "files": list(files)}).encode())
    return flash


class InstallTest(unittest.TestCase):
    def test_nothing_staged_leaves_everything_alone(self):
        flash = FakeFlash(OLD)
        ota.boot(flash)
        self.assertEqual(flash.files, OLD)

    def test_puts_the_new_files_in_place_and_keeps_the_old_ones(self):
        flash = staged()
        ota.boot(flash)
        self.assertEqual(flash.read("app/version.py"), b"new version")
        self.assertEqual(flash.read("app/runner.py"), b"new runner")
        self.assertEqual(flash.read(ota.PREV + "/app/version.py"), b"old version")
        self.assertEqual(flash.read("config.json"), b"{}")
        self.assertEqual(flash.read("oled.py"), b"mine")
        self.assertFalse(any(p.startswith(ota.NEXT + "/") for p in flash.files))
        self.assertEqual(flash.state()["trying"], "0.6.0")
        self.assertTrue(ota.trying(flash))

    def test_a_file_new_in_this_version_just_arrives(self):
        flash = staged(files={"app/update.py": b"new"})
        ota.boot(flash)
        self.assertEqual(flash.read("app/update.py"), b"new")

    def test_carries_on_after_losing_power_halfway(self):
        flash = staged()
        # version.py was already moved aside but not replaced; runner.py was not touched yet
        flash.rename("app/version.py", ota.PREV + "/app/version.py")
        ota.boot(flash)
        self.assertEqual(flash.read("app/version.py"), b"new version")
        self.assertEqual(flash.read(ota.PREV + "/app/version.py"), b"old version")
        self.assertEqual(flash.read(ota.PREV + "/app/runner.py"), b"old runner")

    def test_replaces_backups_left_from_an_earlier_update(self):
        flash = staged()
        flash.write(ota.PREV + "/app/version.py", b"ancient")
        ota.boot(flash)
        self.assertEqual(flash.read(ota.PREV + "/app/version.py"), b"old version")


class RollbackTest(unittest.TestCase):
    def test_a_version_that_keeps_crashing_is_rolled_back(self):
        flash = staged()
        for _ in range(ota.MAX_TRIES):
            ota.boot(flash)
            self.assertEqual(flash.read("app/version.py"), b"new version")
        ota.boot(flash)
        self.assertEqual(flash.read("app/version.py"), b"old version")
        self.assertEqual(flash.read("app/runner.py"), b"old runner")
        self.assertEqual(ota.failed(flash), "0.6.0")
        self.assertFalse(ota.trying(flash))
        ota.boot(flash)
        self.assertEqual(flash.read("app/version.py"), b"old version")

    def test_rolling_back_on_request(self):
        flash = staged(files={"app/version.py": b"new version", "app/update.py": b"new"})
        ota.boot(flash)
        ota.rollback(flash)
        self.assertEqual(flash.read("app/version.py"), b"old version")
        self.assertEqual(ota.failed(flash), "0.6.0")

    def test_a_confirmed_version_stays(self):
        flash = staged()
        ota.boot(flash)
        ota.confirm(flash)
        self.assertFalse(ota.trying(flash))
        self.assertIsNone(ota.failed(flash))
        self.assertFalse(any(p.startswith(ota.PREV + "/") for p in flash.files))
        for _ in range(ota.MAX_TRIES + 1):
            ota.boot(flash)
        self.assertEqual(flash.read("app/version.py"), b"new version")

    def test_confirming_without_an_update_does_nothing(self):
        flash = FakeFlash(OLD)
        ota.confirm(flash)
        self.assertEqual(flash.files, OLD)

    def test_a_failed_download_is_remembered_until_the_next_try(self):
        flash = FakeFlash(OLD)
        ota.mark_failed(flash, "0.6.0")
        self.assertEqual(ota.failed(flash), "0.6.0")
        ota.confirm(flash)
        self.assertEqual(ota.failed(flash), "0.6.0")
        ota.clear(flash)
        self.assertIsNone(ota.failed(flash))

    def test_broken_state_is_ignored(self):
        flash = FakeFlash(dict(OLD, **{ota.STATE: b"{not json"}))
        ota.boot(flash)
        self.assertFalse(ota.trying(flash))
        self.assertIsNone(ota.failed(flash))


class SafePathTest(unittest.TestCase):
    def test_only_firmware_files(self):
        for good in ("app/runner.py", "lib/mfrc522.py"):
            self.assertTrue(ota.safe_path(good), good)
        # main.py and ota.py hold the rollback itself, so a bad update must not be able to replace them
        for bad in ("main.py", "ota.py", "config.json", "/main.py", "../x.py", "app/../config.json", "oled.py", "app/x.txt", "app/a/b.py",
                    ota.NEXT + "/main.py", ""):
            self.assertFalse(ota.safe_path(bad), bad)
