import time

from machine import Pin

from app import patterns


class Led:
    def __init__(self, pin="LED"):
        self._pin = Pin(pin, Pin.OUT)
        self._pattern = patterns.for_state("idle")
        self._since = time.ticks_ms()

    def show(self, state):
        p = patterns.for_state(state)
        if p is not self._pattern:
            self._pattern = p
            self._since = time.ticks_ms()

    def update(self):
        elapsed = time.ticks_diff(time.ticks_ms(), self._since)
        self._pin.value(1 if patterns.level(self._pattern, elapsed) else 0)
