import unittest

from app import report


class BuildTest(unittest.TestCase):
    def test_tag_payload(self):
        self.assertEqual(
            report.build("04a1b2c3d4e5f6", "0.1.0", 7),
            {"uid": "04A1B2C3D4E5F6", "fw": "0.1.0", "seq": 7},
        )

    def test_diagnostics_go_along(self):
        self.assertEqual(
            report.build(None, "0.4.0", 3, rssi=-61, reader_resets=1, uptime_s=7200),
            {"uid": None, "fw": "0.4.0", "seq": 3, "rssi": -61, "reader_resets": 1, "uptime_sec": 7200},
        )

    def test_unknown_signal_is_left_out(self):
        self.assertNotIn("rssi", report.build(None, "0.4.0", 3, rssi=None, reader_resets=0, uptime_s=5))

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

    def test_an_empty_stand_may_report_every_half_minute(self):
        self.assertEqual(report.parse({"led": "idle", "interval_ms": 30000})[1], 30000)

    def test_garbage_response(self):
        self.assertEqual(report.parse(None), ("unknown", report.DEFAULT_INTERVAL_MS))


class ConfigTest(unittest.TestCase):
    def test_valid(self):
        cfg = report.check_config({"ssid": "home", "password": "pw", "server": "http://10.0.0.2:8080/", "token": "spd_x"})
        self.assertEqual(cfg["server"], "http://10.0.0.2:8080")

    def test_missing_fields(self):
        with self.assertRaises(ValueError):
            report.check_config({"ssid": "home"})

    def test_rejects_settings_that_would_crash_the_hardware_setup(self):
        valid = {"ssid": "home", "password": "pw", "server": "http://10.0.0.2:8080", "token": "spd_x"}
        for bad in (
            {"pins": {"sck": "GP2"}},
            {"pins": {"clk": 2}},
            {"pins": [2, 3, 4]},
            {"led_pin": ["LED"]},
            {"watchdog": "yes"},
            {"server": "10.0.0.2:8080"},
        ):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                report.check_config(dict(valid, **bad))

    def test_optional_settings(self):
        cfg = report.check_config(
            {"ssid": "home", "password": "pw", "server": "https://spindle.example", "token": "spd_x",
             "pins": {"sck": 10, "spi_id": 1}, "led_pin": 15, "watchdog": False}
        )
        self.assertEqual(cfg["pins"], {"sck": 10, "spi_id": 1})


class SetupNeededTest(unittest.TestCase):
    def test_without_config_or_network_details(self):
        self.assertTrue(report.needs_setup(None))
        self.assertTrue(report.needs_setup({"pins": {"sck": 2}}))
        self.assertTrue(report.needs_setup({"ssid": "home", "password": "pw", "server": "http://x"}))
        self.assertFalse(report.needs_setup({"ssid": "home", "password": "", "server": "http://x", "token": "spd_x"}))

    def test_forgetting_the_network_keeps_the_hardware_settings(self):
        cfg = {"ssid": "home", "password": "pw", "server": "http://x", "token": "spd_x", "pins": {"sck": 10}, "led_pin": 15}
        self.assertEqual(report.forget_network(cfg), {"pins": {"sck": 10}, "led_pin": 15})

    def test_joining_a_network_keeps_the_hardware_settings(self):
        cfg = report.join_network({"pins": {"sck": 10}, "ssid": "old"}, "home", "pw", "http://x", "spd_x")
        self.assertEqual(cfg, {"pins": {"sck": 10}, "ssid": "home", "password": "pw", "server": "http://x", "token": "spd_x"})


class RetryTest(unittest.TestCase):
    def test_backs_off_while_the_server_fails(self):
        self.assertEqual(report.retry_delay(1500, 0), 1500)
        self.assertEqual(report.retry_delay(1500, 1), 3000)
        self.assertEqual(report.retry_delay(1500, 3), 12000)
        self.assertEqual(report.retry_delay(1500, 50), report.MAX_RETRY_MS)


if __name__ == "__main__":
    unittest.main()


class UptimeTest(unittest.TestCase):
    def test_counts_whole_seconds_since_start(self):
        u = report.Uptime(start_ms=1000)
        self.assertEqual(u.seconds(1999), 0)
        self.assertEqual(u.seconds(4500), 3)

    def test_keeps_counting_past_the_millisecond_counter_wrapping(self):
        period = 1 << 30

        def ticks_diff(a, b):
            return ((a - b + period // 2) % period) - period // 2

        u = report.Uptime(start_ms=0, diff=ticks_diff)
        now = 0
        for _ in range(30):
            now = (now + 100_000_000) % period
            seconds = u.seconds(now)
        self.assertEqual(seconds, 3_000_000)
