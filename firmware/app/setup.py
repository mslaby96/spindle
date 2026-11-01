import json
import select
import socket
import time

import machine
import network

from app import portal, report
from app.net import post_json
from app.version import VERSION

JOIN_TIMEOUT_MS = 20000
# Long enough for the phone to load the "all set" page before the stand restarts and its network disappears.
DONE_LINGER_MS = 15000


def save_config(cfg):
    with open("config.json", "w") as f:
        json.dump(cfg, f)


class SetupPortal:
    """Runs the stand's own Wi-Fi network with the setup page until the stand is set up, then restarts it."""

    def __init__(self, cfg, led, wdt):
        self._cfg = cfg or {}
        self._led = led
        self._wdt = wdt
        self._ap = network.WLAN(network.AP_IF)
        self._sta = network.WLAN(network.STA_IF)
        self._view = {"state": "form", "networks": [], "values": {"server": self._cfg.get("server", "")}, "error": None}
        self._pending = None
        self._done_at = None

    def run(self, back_online=None):
        """Returns only if back_online reports that the stand's usual network is reachable again."""
        self._start()
        try:
            while True:
                self._wdt.feed()
                self._led.update()
                for sock, _ in self._poll.poll(50):
                    if sock is self._dns:
                        self._answer_dns()
                    else:
                        self._serve()
                if self._pending:
                    self._join(self._pending)
                    self._pending = None
                if self._done_at is not None and time.ticks_diff(time.ticks_ms(), self._done_at) > DONE_LINGER_MS:
                    machine.reset()
                if back_online and self._view["state"] == "form" and back_online():
                    return
        finally:
            self._stop()

    def _start(self):
        self._led.show("setup")
        self._sta.active(True)
        self._view["networks"] = self._scan()
        self._ap.config(ssid=portal.ap_name(machine.unique_id()), security=0)
        self._ap.active(True)
        self._ip = self._ap.ifconfig()[0]
        print("setup network", portal.ap_name(machine.unique_id()), "at", self._ip)
        self._http = socket.socket()
        self._http.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._http.bind(("0.0.0.0", 80))
        self._http.listen(2)
        self._dns = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self._dns.bind(("0.0.0.0", 53))
        self._poll = select.poll()
        self._poll.register(self._http, select.POLLIN)
        self._poll.register(self._dns, select.POLLIN)

    def _stop(self):
        for s in (self._http, self._dns):
            s.close()
        self._ap.active(False)

    def _scan(self):
        try:
            return [(n[0].decode(), n[3]) for n in self._sta.scan()]
        except Exception as e:
            print("wifi scan failed:", e)
            return []

    def _answer_dns(self):
        try:
            query, addr = self._dns.recvfrom(512)
            reply = portal.dns_reply(query, self._ip)
            if reply:
                self._dns.sendto(reply, addr)
        except OSError:
            pass

    def _serve(self):
        conn, _ = self._http.accept()
        try:
            conn.settimeout(2)
            data, req = b"", None
            while req is None and len(data) < 4096:
                chunk = conn.recv(512)
                if not chunk:
                    break
                data += chunk
                req = portal.parse_request(data)
            if req:
                print("setup page request:", req.method, req.host, req.path)
            resp, setup = portal.route(req, self._ip, self._view)
            conn.sendall(resp)
            if setup:
                self._view.update(state="working", values=setup, error=None)
                self._pending = setup
        except OSError:
            pass
        finally:
            conn.close()

    def _join(self, setup):
        error = self._connect(setup) or self._pair(setup)
        if error:
            self._view.update(state="form", error=error)

    def _connect(self, setup):
        self._sta.disconnect()
        self._sta.connect(setup["ssid"], setup["password"])
        start = time.ticks_ms()
        while not self._sta.isconnected():
            if time.ticks_diff(time.ticks_ms(), start) > JOIN_TIMEOUT_MS or self._sta.status() < 0:
                self._sta.disconnect()
                return "Couldn't join " + setup["ssid"] + ". Check the network name and password."
            self._wdt.feed()
            self._led.update()
            time.sleep_ms(100)
        return None

    def _pair(self, setup):
        try:
            status, body = post_json(setup["server"] + "/api/device/v1/pair", None, {"code": setup["code"], "fw": VERSION})
        except Exception as e:
            print("pairing failed:", e)
            return "Couldn't reach the Spindle server at " + setup["server"] + ". Check the address."
        self._wdt.feed()
        if status == 403:
            return "That pairing code is wrong or has run out. Get a new one in the app."
        if status == 429:
            return "Too many wrong codes. Wait a few minutes and try again."
        token = body.get("token") if isinstance(body, dict) else None
        if status != 200 or not token:
            return "The Spindle server answered with an error ({}).".format(status)
        save_config(report.join_network(self._cfg, setup["ssid"], setup["password"], setup["server"], token))
        self._view["state"] = "done"
        self._done_at = time.ticks_ms()
        return None
