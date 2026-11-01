# How hard the stand works: flat out while a sleeve is on it, quietly while it waits for one.


class Pace:
    def __init__(self, read_every_ms, antenna_stays_on, radio_saves_power):
        self.read_every_ms = read_every_ms
        self.antenna_stays_on = antenna_stays_on
        self.radio_saves_power = radio_saves_power


_PLAYING = Pace(200, True, False)
# Three reads confirm a new sleeve, so it still shows up in under a second.
_WAITING = Pace(300, False, True)


def pace(present):
    return _PLAYING if present else _WAITING
