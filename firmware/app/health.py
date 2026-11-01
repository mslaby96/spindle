# Telling a stuck reader from an empty stand. Both read "no tag", but a reader that browned out or lost the SPI bus no
# longer holds the settings it was given at start, or answers with all zeros or all ones.

VERSION_REG = 0x37
# what the driver writes in init(): timer mode and prescaler, 100 % ASK modulation, CRC preset
SETTINGS = {0x2A: 0x8D, 0x2B: 0x3E, 0x15: 0x40, 0x11: 0x3D}


def healthy(read_reg, version):
    """read_reg reads one register; version is what the version register said when the reader was set up."""
    if read_reg(VERSION_REG) != version:
        return False
    return all(read_reg(reg) == value for reg, value in SETTINGS.items())


class Checkup:
    """Says when to look at the reader: every interval while the stand is empty, never while a sleeve is on it."""

    def __init__(self, interval_ms, start_ms, diff=lambda a, b: a - b):
        # on the Pico diff is time.ticks_diff, as the millisecond counter wraps after about twelve days
        self._interval = interval_ms
        self._last = start_ms
        self._diff = diff

    def due(self, now_ms, empty):
        if not empty or self._diff(now_ms, self._last) < self._interval:
            return False
        self._last = now_ms
        return True
