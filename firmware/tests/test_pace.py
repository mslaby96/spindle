import unittest

from app import pace


class PaceTest(unittest.TestCase):
    def test_a_sleeve_on_the_stand_gets_full_speed(self):
        p = pace.pace(present=True)
        self.assertEqual(p.read_every_ms, 200)
        self.assertTrue(p.antenna_stays_on)
        self.assertFalse(p.radio_saves_power)

    def test_an_empty_stand_saves_power(self):
        p = pace.pace(present=False)
        self.assertEqual(p.read_every_ms, 300)
        self.assertFalse(p.antenna_stays_on)
        self.assertTrue(p.radio_saves_power)

    def test_a_new_sleeve_is_still_noticed_within_a_second(self):
        idle = pace.pace(present=False)
        self.assertLessEqual(idle.read_every_ms * 3, 1000)


if __name__ == "__main__":
    unittest.main()
