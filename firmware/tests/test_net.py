import sys
import types
import unittest


class FakeTime:
    def __init__(self):
        self.now = 0

    def ticks_ms(self):
        return self.now

    def ticks_add(self, t, d):
        return t + d

    def ticks_diff(self, a, b):
        return a - b


class FakeWLAN:
    def __init__(self, _iface):
        self.state = "idle"
        self.connects = 0

    def active(self, _on):
        pass

    def isconnected(self):
        return self.state == "up"

    def status(self):
        return network.STAT_CONNECTING if self.state == "connecting" else 0

    def disconnect(self):
        self.state = "idle"

    def connect(self, _ssid, _password):
        self.connects += 1
        self.state = "connecting"


network = types.ModuleType("network")
network.STA_IF = 0
network.STAT_CONNECTING = 1
network.WLAN = FakeWLAN
sys.modules.setdefault("network", network)
sys.modules.setdefault("requests", types.ModuleType("requests"))

from app import net  # noqa: E402


class WiFiTest(unittest.TestCase):
    def setUp(self):
        self.clock = FakeTime()
        net.time = self.clock
        self.wifi = net.WiFi("home", "pw")

    def test_connects_and_reports_up(self):
        self.assertFalse(self.wifi.tick())
        self.wifi._wlan.state = "up"
        self.assertTrue(self.wifi.tick())

    def test_waits_for_a_connection_in_progress(self):
        self.wifi.tick()
        self.clock.now += 3000
        self.wifi.tick()
        self.assertEqual(self.wifi._wlan.connects, 1)

    def test_gives_up_on_an_attempt_that_never_finishes(self):
        self.wifi.tick()
        self.clock.now += net.CONNECT_TIMEOUT_MS + 1
        self.wifi.tick()
        self.assertEqual(self.wifi._wlan.connects, 2)

    def test_counts_how_long_the_network_has_been_gone(self):
        self.wifi.tick()
        self.clock.now += 1000
        self.wifi.tick()
        self.assertEqual(self.wifi.down_ms(), 1000)
        self.wifi._wlan.state = "up"
        self.wifi.tick()
        self.assertEqual(self.wifi.down_ms(), 0)
        self.wifi._wlan.state = "idle"
        self.clock.now += 500
        self.wifi.tick()
        self.clock.now += 700
        self.assertEqual(self.wifi.down_ms(), 700)


if __name__ == "__main__":
    unittest.main()
