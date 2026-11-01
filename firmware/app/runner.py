import json
import time

import machine
import rp2
from machine import WDT

import ota
from app import health, pace, report, update
from app.debounce import Debouncer
from app.hold import Hold
from app.led import Led
from app.version import VERSION

WDT_TIMEOUT_MS = 8000
# Holding BOOTSEL this long while the stand runs forgets its Wi-Fi and opens the setup network again.
FORGET_HOLD_MS = 5000
# After this long without the usual Wi-Fi the setup network opens, while the stand keeps trying the old one.
SETUP_AFTER_OFFLINE_MS = 5 * 60 * 1000
# How often an empty stand checks that the reader hasn't got stuck.
READER_CHECK_MS = 30000
# A new firmware that can't reach the server this long after starting is rolled back.
TRIAL_MS = 10 * 60 * 1000


def read_config():
    try:
        with open("config.json") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def watchdog(cfg):
    # The watchdog cannot be stopped once started, so allow turning it off while developing over USB. A new firmware on
    # trial already has one running from main.py, which then has to be fed.
    on = cfg.get("watchdog", True) or ota.trying(ota.Flash())
    return WDT(timeout=WDT_TIMEOUT_MS) if on else _NoWatchdog()


class _NoWatchdog:
    def feed(self):
        pass


def fail_forever(led):
    led.show("error")
    while True:
        led.update()
        time.sleep_ms(50)


def read_tag(reader, antenna_stays_on):
    try:
        return reader.read(antenna_stays_on)
    except Exception as e:
        print("reader error:", e)
        return None


def heal(reader):
    try:
        if reader.heal_if_stuck():
            print("reader was stuck and has been reset, {} times so far".format(reader.resets))
    except Exception as e:
        print("reader check failed:", e)


def obey(command, body, cfg, flash, wdt, led):
    """Carries out what the server asked for. Returns the version that failed to download, if that was it."""
    if command == "change_wifi":
        from app.setup import save_config

        print("the app asked to change the Wi-Fi")
        save_config(report.keep_pairing(cfg))
        machine.reset()
    if command == "update":
        from app.net import get

        led.show("update")
        base = cfg["server"] + "/api/device/v1/firmware/"
        try:
            version = update.download(lambda path: get(base + path, cfg["token"]), flash, wdt.feed)
        except Exception as e:
            print("update failed:", e)
            version = body.get("version") or "unknown"
            ota.mark_failed(flash, version)
            return version
        print("restarting into", version)
        machine.reset()
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
    checkup = health.Checkup(READER_CHECK_MS, time.ticks_ms(), time.ticks_diff)
    uptime = report.Uptime(time.ticks_ms(), time.ticks_diff)
    debounce = Debouncer()
    url = cfg["server"] + "/api/device/v1/report"
    flash = ota.Flash()
    on_trial = ota.trying(flash)
    update_failed = ota.failed(flash)
    started = time.ticks_ms()

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
        if on_trial and time.ticks_diff(now, started) > TRIAL_MS:
            print("the new firmware never reached the server, going back")
            ota.rollback(flash)
            machine.reset()

        if time.ticks_diff(now, next_read) >= 0:
            # the candidate counts as present too, so a sleeve being put on is confirmed at full speed
            present = debounce.current is not None or debounce.candidate is not None
            if checkup.due(now, empty=not present):
                heal(reader)
            now_pace = pace.pace(present)
            uid = debounce.feed(read_tag(reader, now_pace.antenna_stays_on))
            wifi.save_power(now_pace.radio_saves_power)
            next_read = time.ticks_add(now, now_pace.read_every_ms)
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
                payload = report.build(
                    debounce.current,
                    VERSION,
                    seq,
                    wifi.rssi(),
                    reader.resets,
                    uptime.seconds(time.ticks_ms()),
                    update_failed,
                )
                status, body = post_json(url, cfg["token"], payload)
            except Exception as e:
                print("report failed:", e)
                status, body = 0, None
            wdt.feed()
            if status == 200:
                failures = 0
                state, interval = report.parse(body)
                led.show(state)
                if on_trial:
                    ota.confirm(flash)
                    on_trial = False
                command = report.command(body)
                if command:
                    update_failed = obey(command, body, cfg, flash, wdt, led) or update_failed
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
