import unittest

from app.hold import Hold


class HoldTest(unittest.TestCase):
    def test_fires_once_after_holding_long_enough(self):
        h = Hold(5000)
        self.assertFalse(h.feed(True, 0))
        self.assertFalse(h.feed(True, 4999))
        self.assertTrue(h.feed(True, 5000))
        self.assertFalse(h.feed(True, 9000))

    def test_letting_go_starts_over(self):
        h = Hold(5000)
        h.feed(True, 0)
        h.feed(False, 3000)
        self.assertFalse(h.feed(True, 6000))
        self.assertTrue(h.feed(True, 11000))


if __name__ == "__main__":
    unittest.main()
