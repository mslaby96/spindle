import unittest

from app import patterns

STATES = ("idle", "known", "unknown", "catalog_ready", "catalog_saved", "offline", "error", "setup", "update")


class PatternTest(unittest.TestCase):
    def test_every_state_has_distinct_pattern(self):
        seen = {patterns.for_state(s) for s in STATES}
        self.assertEqual(len(seen), len(STATES))

    def test_unknown_state_uses_default(self):
        self.assertEqual(patterns.for_state("disco"), patterns.for_state("unknown"))

    def test_idle_is_off_and_known_is_on(self):
        for t in (0, 333, 5000):
            self.assertFalse(patterns.level(patterns.for_state("idle"), t))
            self.assertTrue(patterns.level(patterns.for_state("known"), t))

    def test_blink_follows_steps(self):
        p = patterns.for_state("unknown")
        self.assertTrue(patterns.level(p, 0))
        self.assertFalse(patterns.level(p, p[0] + 1))
        self.assertTrue(patterns.level(p, sum(p) + 1))


if __name__ == "__main__":
    unittest.main()
