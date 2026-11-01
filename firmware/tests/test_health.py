import unittest

from app import health

GOOD = {0x2A: 0x8D, 0x2B: 0x3E, 0x15: 0x40, 0x11: 0x3D, 0x37: 0x82}


class HealthyTest(unittest.TestCase):
    def test_a_reader_holding_its_settings_is_healthy(self):
        self.assertTrue(health.healthy(GOOD.get, 0x82))

    def test_a_reader_that_lost_its_settings_is_not(self):
        reset_to_defaults = {**GOOD, 0x2A: 0x00, 0x11: 0x3F, 0x15: 0x00}
        self.assertFalse(health.healthy(reset_to_defaults.get, 0x82))

    def test_a_reader_that_stopped_answering_is_not(self):
        for silent in (0x00, 0xFF):
            self.assertFalse(health.healthy(lambda reg: silent, 0x82), silent)

    def test_a_different_chip_version_means_garbled_answers(self):
        self.assertFalse(health.healthy({**GOOD, 0x37: 0x12}.get, 0x82))


class CheckupTest(unittest.TestCase):
    def test_checks_every_half_minute_while_the_stand_is_empty(self):
        c = health.Checkup(30000, start_ms=0)
        self.assertFalse(c.due(29999, empty=True))
        self.assertTrue(c.due(30000, empty=True))
        self.assertFalse(c.due(30001, empty=True))
        self.assertTrue(c.due(60000, empty=True))

    def test_never_while_a_sleeve_is_on_the_stand(self):
        c = health.Checkup(30000, start_ms=0)
        self.assertFalse(c.due(90000, empty=False))
        self.assertTrue(c.due(90001, empty=True))
