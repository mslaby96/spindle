import sys
import types
import unittest


class FakeRC:
    OK, ERR, REQALL = 0, 2, 0x52

    def __init__(self, **_):
        self.antenna = True

    def antenna_on(self, on=True):
        self.antenna = on

    def request(self, _mode):
        return (self.OK if self.antenna else self.ERR), None

    def SelectTagSN(self):
        return self.OK, [0x04, 0xA1, 0xB2, 0xC3]

    def _crc(self, _frame):
        return [0, 0]

    def _tocard(self, *_):
        pass


mfrc522 = types.ModuleType("mfrc522")
mfrc522.MFRC522 = FakeRC
sys.modules.setdefault("mfrc522", mfrc522)

import time  # noqa: E402

if not hasattr(time, "sleep_ms"):
    time.sleep_ms = lambda ms: None

from app.reader import Reader  # noqa: E402


class ReaderTest(unittest.TestCase):
    def test_reads_with_the_field_only_up_for_the_read(self):
        r = Reader()
        self.assertEqual(r.read(antenna_stays_on=False), "04A1B2C3")
        self.assertFalse(r._rc.antenna)

    def test_going_back_to_full_speed_turns_the_field_back_on(self):
        r = Reader()
        r.read(antenna_stays_on=False)
        self.assertEqual(r.read(antenna_stays_on=True), "04A1B2C3")
        self.assertTrue(r._rc.antenna)


if __name__ == "__main__":
    unittest.main()
