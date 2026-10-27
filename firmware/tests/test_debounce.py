import unittest

from app.debounce import Debouncer

A = "04A1B2C3D4E5F6"
B = "04B2C3D4E5F6A1"


def feed(d, reads):
    out = None
    for r in reads:
        out = d.feed(r)
    return out


class DebouncerTest(unittest.TestCase):
    def test_new_tag_needs_three_matching_reads(self):
        d = Debouncer()
        self.assertIsNone(feed(d, [A, A]))
        self.assertEqual(d.feed(A), A)

    def test_two_misses_keep_tag(self):
        d = Debouncer()
        feed(d, [A, A, A])
        self.assertEqual(feed(d, [None, None]), A)

    def test_third_miss_reports_empty(self):
        d = Debouncer()
        feed(d, [A, A, A])
        self.assertIsNone(feed(d, [None, None, None]))

    def test_miss_counter_resets_on_read(self):
        d = Debouncer()
        feed(d, [A, A, A, None, None, A, None, None])
        self.assertEqual(d.current, A)

    def test_switch_to_new_tag_after_three_reads(self):
        d = Debouncer()
        feed(d, [A, A, A])
        self.assertEqual(feed(d, [B, B]), A)
        self.assertEqual(d.feed(B), B)

    def test_alternating_tags_keep_last_stable(self):
        d = Debouncer()
        feed(d, [A, A, A])
        self.assertEqual(feed(d, [B, A, B, A, B, A, B]), A)

    def test_alternating_from_empty_reports_nothing(self):
        d = Debouncer()
        self.assertIsNone(feed(d, [A, B, A, B, A, B]))


if __name__ == "__main__":
    unittest.main()
