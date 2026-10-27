import time

from mfrc522 import MFRC522

from app.report import uid_hex

DEFAULT_PINS = {"sck": 2, "mosi": 3, "miso": 4, "cs": 1, "rst": 0, "spi_id": 0}


class Reader:
    def __init__(self, pins=None):
        p = dict(DEFAULT_PINS)
        p.update(pins or {})
        self._rc = MFRC522(sck=p["sck"], mosi=p["mosi"], miso=p["miso"], rst=p["rst"], cs=p["cs"], spi_id=p["spi_id"])

    def read(self):
        # A selected tag stays ACTIVE and ignores the next wake-up, so drop the field to reset it first.
        self._rc.antenna_on(False)
        time.sleep_ms(3)
        self._rc.antenna_on(True)
        time.sleep_ms(3)
        stat, _ = self._rc.request(self._rc.REQALL)
        if stat != self._rc.OK:
            return None
        stat, uid = self._rc.SelectTagSN()
        if stat != self._rc.OK or not uid:
            return None
        return uid_hex(uid)
