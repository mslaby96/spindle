import time

from mfrc522 import MFRC522

from app.reader import DEFAULT_PINS
from app.report import uid_hex

p = DEFAULT_PINS
PATTERNS = (0x00, 0x55, 0xAA, 0xFF, 0x3C)


def reader(baud):
    return MFRC522(sck=p["sck"], mosi=p["mosi"], miso=p["miso"], rst=p["rst"], cs=p["cs"], spi_id=p["spi_id"], baudrate=baud)


def registers(rc):
    print("  version:", hex(rc._rreg(0x37)), " TModeReg (expect 0x8d):", hex(rc._rreg(0x2A)),
          " ModeReg (expect 0x3d):", hex(rc._rreg(0x11)))
    bad = []
    for v in PATTERNS:
        rc._wreg(0x2D, v)
        got = rc._rreg(0x2D)
        if got != v:
            bad.append("wrote {} read {}".format(hex(v), hex(got)))
    rc._wreg(0x2D, 30)
    print("  write/read test:", "ok" if not bad else ", ".join(bad))


def tag_reads(rc):
    hits, last = 0, None
    for _ in range(25):
        rc.antenna_on(False)
        time.sleep_ms(10)
        rc.antenna_on(True)
        time.sleep_ms(10)
        stat, _ = rc.request(rc.REQALL)
        if stat == rc.OK:
            stat, uid = rc.SelectTagSN()
            if stat == rc.OK:
                hits, last = hits + 1, uid
        time.sleep_ms(200)
    return hits, uid_hex(last) if last else "-"


print("Hold a tag flat on the reader for the whole test.")
time.sleep(3)
for baud in (1_000_000, 100_000):
    print("SPI at {} kHz".format(baud // 1000))
    rc = reader(baud)
    registers(rc)
    hits, uid = tag_reads(rc)
    print("  tag reads: {}/25  {}".format(hits, uid))
