import time

import network
import requests

# A report makes up to three blocking socket calls, which together must stay well inside the watchdog timeout.
HTTP_TIMEOUT_S = 2
CONNECT_TIMEOUT_MS = 20000
MIN_BACKOFF_MS = 5000
MAX_BACKOFF_MS = 60000


class WiFi:
    def __init__(self, ssid, password):
        self._ssid = ssid
        self._password = password
        self._wlan = network.WLAN(network.STA_IF)
        self._wlan.active(True)
        self._next_attempt = time.ticks_ms()
        self._attempt_started = self._next_attempt
        self._backoff = MIN_BACKOFF_MS
        self._down_since = self._next_attempt

    def down_ms(self):
        if self._down_since is None:
            return 0
        return time.ticks_diff(time.ticks_ms(), self._down_since)

    def tick(self):
        if self._wlan.isconnected():
            self._backoff = MIN_BACKOFF_MS
            self._down_since = None
            return True
        now = time.ticks_ms()
        if self._down_since is None:
            self._down_since = now
        if (
            self._wlan.status() == network.STAT_CONNECTING
            and time.ticks_diff(now, self._attempt_started) < CONNECT_TIMEOUT_MS
        ):
            return False
        if time.ticks_diff(now, self._next_attempt) >= 0:
            self._wlan.disconnect()
            self._wlan.connect(self._ssid, self._password)
            self._attempt_started = now
            self._next_attempt = time.ticks_add(now, self._backoff)
            self._backoff = min(self._backoff * 2, MAX_BACKOFF_MS)
        return False


def post_json(url, token, payload):
    headers = {"Authorization": "Bearer " + token} if token else {}
    resp = requests.post(url, json=payload, headers=headers, timeout=HTTP_TIMEOUT_S)
    try:
        if resp.status_code != 200:
            return resp.status_code, None
        return 200, resp.json()
    finally:
        resp.close()
