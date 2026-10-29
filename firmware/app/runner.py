import json
import time

import machine
import rp2
from machine import WDT

from app import report
from app.debounce import Debouncer
from app.hold import Hold
from app.led import Led
from app.version import VERSION

READ_EVERY_MS = 200
WDT_TIMEOUT_MS = 8000
# Holding BOOTSEL this long while the stand runs forgets its Wi-Fi and opens the setup network again.
FORGET_HOLD_MS = 5000
# After this long without the usual Wi-Fi the setup network opens, while the stand keeps trying the old one.
SETUP_AFTER_OFFLINE_MS = 5 * 60 * 1000


def read_config():
    try:
        with open("config.json") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def watchdog(cfg):
    # The watchdog cannot be stopped once started, so allow turning it off while developing over USB.
    return WDT(timeout=WDT_TIMEOUT_MS) if cfg.get("watchdog", True) else _NoWatchdog()


class _NoWatchdog:
    def feed(self):
        pass


def fail_forever(led):
    led.show("error")
    while True:
        led.update()
        time.sleep_ms(50)


def read_tag(reader):
    try:
        return reader.read()
    except Exception as e:
        print("reader error:", e)
        return None


def run():
    led = Led()
    raw = read_config()
    try:
        if raw is not None and raw.get("led_pin") is not None:
            led = Led(raw["led_pin"])
        if report.needs_setup(raw):
            from app.setup import SetupPortal

            SetupPortal(raw, led, watchdog(raw or {})).run()
        cfg = report.check_config(raw)
        from app.net import WiFi, post_json
        from app.reader import Reader

        reader = Reader(cfg.get("pins"))
        wifi = WiFi(cfg["ssid"], cfg["password"])
    except Exception as e:
        print("setup error:", e)
        fail_forever(led)

    wdt = watchdog(cfg)
    forget = Hold(FORGET_HOLD_MS)
    debounce = Debouncer()
    url = cfg["server"] + "/api/device/v1/report"

    seq = 0
    failures = 0
    attempted = object()
    interval = report.DEFAULT_INTERVAL_MS
    next_read = time.ticks_ms()
    next_report = time.ticks_ms()
    led.show("offline")

    while True:
        wdt.feed()
        now = time.ticks_ms()

        if time.ticks_diff(now, next_read) >= 0:
            uid = debounce.feed(read_tag(reader))
            next_read = time.ticks_add(now, READ_EVERY_MS)
            if uid != attempted:
                next_report = now
            # reading BOOTSEL briefly pauses flash access, so it is checked with the reader rather than every pass
            if forget.feed(rp2.bootsel_button() == 1, now):
                from app.setup import save_config

                print("forgetting the Wi-Fi settings")
                save_config(report.forget_network(cfg))
                machine.reset()

        if not wifi.tick():
            led.show("offline")
            if wifi.down_ms() > SETUP_AFTER_OFFLINE_MS:
                from app.setup import SetupPortal

                SetupPortal(cfg, led, wdt).run(back_online=wifi.tick)
                led.show("offline")
        elif time.ticks_diff(now, next_report) >= 0:
            seq += 1
            attempted = debounce.current
            try:
                status, body = post_json(url, cfg["token"], report.build(debounce.current, VERSION, seq))
            except Exception as e:
                print("report failed:", e)
                status, body = 0, None
            wdt.feed()
            if status == 200:
                failures = 0
                state, interval = report.parse(body)
                led.show(state)
            elif status == 401:
                failures += 1
                print("server rejected the device token")
                led.show("error")
            else:
                failures += 1
                led.show("offline")
            next_report = time.ticks_add(time.ticks_ms(), report.retry_delay(interval, failures))

        led.update()
        time.sleep_ms(20)
