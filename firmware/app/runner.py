import json
import time

from machine import WDT

from app import report
from app.debounce import Debouncer
from app.led import Led
from app.version import VERSION

READ_EVERY_MS = 200
WDT_TIMEOUT_MS = 8000


def load_config():
    with open("config.json") as f:
        return report.check_config(json.load(f))


class _NoWatchdog:
    def feed(self):
        pass


def fail_forever(led):
    led.show("error")
    while True:
        led.update()
        time.sleep_ms(50)


def run():
    led = Led()
    try:
        cfg = load_config()
    except Exception as e:
        print("config error:", e)
        fail_forever(led)

    # The watchdog cannot be stopped once started, so allow turning it off while developing over USB.
    wdt = WDT(timeout=WDT_TIMEOUT_MS) if cfg.get("watchdog", True) else _NoWatchdog()

    from app.net import WiFi, post_json
    from app.reader import Reader

    if cfg.get("led_pin") is not None:
        led = Led(cfg["led_pin"])
    reader = Reader(cfg.get("pins"))
    wifi = WiFi(cfg["ssid"], cfg["password"])
    debounce = Debouncer()
    url = cfg["server"] + "/api/device/v1/report"

    seq = 0
    reported = object()
    interval = report.DEFAULT_INTERVAL_MS
    next_read = time.ticks_ms()
    next_report = time.ticks_ms()
    led.show("offline")

    while True:
        wdt.feed()
        now = time.ticks_ms()

        if time.ticks_diff(now, next_read) >= 0:
            uid = debounce.feed(reader.read())
            next_read = time.ticks_add(now, READ_EVERY_MS)
            if uid != reported:
                next_report = now

        if not wifi.tick():
            led.show("offline")
        elif time.ticks_diff(now, next_report) >= 0:
            seq += 1
            try:
                status, body = post_json(url, cfg["token"], report.build(debounce.current, VERSION, seq))
            except Exception as e:
                print("report failed:", e)
                status, body = 0, None
            wdt.feed()
            if status == 200:
                state, interval = report.parse(body)
                led.show(state)
                reported = debounce.current
            elif status == 401:
                print("server rejected the device token")
                led.show("error")
            else:
                led.show("offline")
            next_report = time.ticks_add(time.ticks_ms(), interval)

        led.update()
        time.sleep_ms(20)
