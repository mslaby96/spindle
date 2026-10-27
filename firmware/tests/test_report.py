import unittest

from app import report


class BuildTest(unittest.TestCase):
    def test_tag_payload(self):
        self.assertEqual(
            report.build("04a1b2c3d4e5f6", "0.1.0", 7),
            {"uid": "04A1B2C3D4E5F6", "fw": "0.1.0", "seq": 7},
        )

    def test_empty_payload(self):
        self.assertEqual(report.build(None, "0.1.0", 1)["uid"], None)

    def test_uid_from_bytes(self):
        self.assertEqual(report.uid_hex([0x04, 0xA1, 0x0B]), "04A10B")


class ParseTest(unittest.TestCase):
    def test_known_response(self):
        self.assertEqual(report.parse({"led": "known", "interval_ms": 1500}), ("known", 1500))

    def test_unknown_led_falls_back(self):
        self.assertEqual(report.parse({"led": "disco", "interval_ms": 1500})[0], "unknown")

    def test_interval_is_clamped(self):
        self.assertEqual(report.parse({"led": "idle", "interval_ms": 10})[1], report.MIN_INTERVAL_MS)
        self.assertEqual(report.parse({"led": "idle", "interval_ms": 10**9})[1], report.MAX_INTERVAL_MS)
        self.assertEqual(report.parse({"led": "idle"})[1], report.DEFAULT_INTERVAL_MS)

    def test_garbage_response(self):
        self.assertEqual(report.parse(None), ("unknown", report.DEFAULT_INTERVAL_MS))


class ConfigTest(unittest.TestCase):
    def test_valid(self):
        cfg = report.check_config({"ssid": "home", "password": "pw", "server": "http://10.0.0.2:8080/", "token": "spd_x"})
        self.assertEqual(cfg["server"], "http://10.0.0.2:8080")

    def test_missing_fields(self):
        with self.assertRaises(ValueError):
            report.check_config({"ssid": "home"})


if __name__ == "__main__":
    unittest.main()
