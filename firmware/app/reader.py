import time

from mfrc522 import MFRC522

from app.report import uid_hex

# a tag needs a few milliseconds of field to power up before it can answer
ANTENNA_SETTLE_MS = 5
DEFAULT_PINS = {"sck": 2, "mosi": 3, "miso": 4, "cs": 1, "rst": 0, "spi_id": 0}


class Reader:
    def __init__(self, pins=None):
        p = dict(DEFAULT_PINS)
        p.update(pins or {})
        self._rc = MFRC522(sck=p["sck"], mosi=p["mosi"], miso=p["miso"], rst=p["rst"], cs=p["cs"], spi_id=p["spi_id"])
        self._field = True

    def read(self, antenna_stays_on=True):
        """With antenna_stays_on False the field is only up for the read itself, which is most of what the reader draws."""
        if not self._field:
            self._rc.antenna_on(True)
            self._field = True
            time.sleep_ms(ANTENNA_SETTLE_MS)
        try:
            return self._read()
        finally:
            if not antenna_stays_on:
                self._rc.antenna_on(False)
                self._field = False

    def _read(self):
        # WUPA also wakes a tag that was halted after the previous read, so a sleeve left on the stand keeps answering.
        stat, _ = self._rc.request(self._rc.REQALL)
        if stat != self._rc.OK:
            return None
        stat, uid = self._rc.SelectTagSN()
        if stat != self._rc.OK or not uid:
            return None
        self._halt()
        return uid_hex(uid)

    def _halt(self):
        frame = [0x50, 0x00]
        frame += self._rc._crc(frame)
        self._rc._tocard(0x0C, frame)
