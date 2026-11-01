LED_STATES = ("idle", "known", "unknown", "catalog_ready", "catalog_saved")
DEFAULT_INTERVAL_MS = 1500
MIN_INTERVAL_MS = 500
MAX_INTERVAL_MS = 60000
MAX_RETRY_MS = 30000
REQUIRED = ("ssid", "password", "server", "token")
PIN_NAMES = ("sck", "mosi", "miso", "cs", "rst", "spi_id")
COMMANDS = ("change_wifi", "update")


def uid_hex(raw):
    return "".join("{:02X}".format(b) for b in raw)


def build(uid, fw, seq, rssi=None, reader_resets=None, uptime_s=None, update_failed=None):
    payload = {"uid": uid.upper() if uid else None, "fw": fw, "seq": seq}
    for key, value in (
        ("rssi", rssi),
        ("reader_resets", reader_resets),
        ("uptime_sec", uptime_s),
        ("update_failed", update_failed),
    ):
        if value is not None:
            payload[key] = value
    return payload


class Uptime:
    """Counts time since start in steps, so the millisecond counter wrapping after about twelve days doesn't matter as
    long as it is asked at least every few days."""

    def __init__(self, start_ms, diff=lambda a, b: a - b):
        self._last = start_ms
        self._ms = 0
        self._diff = diff

    def seconds(self, now_ms):
        self._ms += self._diff(now_ms, self._last)
        self._last = now_ms
        return self._ms // 1000


def parse(body):
    if not isinstance(body, dict):
        return "unknown", DEFAULT_INTERVAL_MS
    led = body.get("led")
    if led not in LED_STATES:
        led = "unknown"
    interval = body.get("interval_ms")
    if not isinstance(interval, int):
        interval = DEFAULT_INTERVAL_MS
    return led, max(MIN_INTERVAL_MS, min(MAX_INTERVAL_MS, interval))


def command(body):
    """What the server asks the stand to do besides showing its state, or None."""
    c = body.get("command") if isinstance(body, dict) else None
    return c if c in COMMANDS else None


def retry_delay(interval, failures):
    return min(interval * 2 ** min(failures, 10), MAX_RETRY_MS)


def needs_setup(cfg):
    return cfg is None or "password" not in cfg or any(not cfg.get(k) for k in ("ssid", "server", "token"))


def forget_network(cfg):
    return {k: v for k, v in cfg.items() if k not in REQUIRED}


def keep_pairing(cfg):
    """Forgets only the Wi-Fi, so the stand can join another network and stay the same stand in Spindle."""
    return {k: v for k, v in cfg.items() if k not in ("ssid", "password")}


def paired(cfg):
    return bool(cfg) and bool(cfg.get("server")) and bool(cfg.get("token"))


def join_network(cfg, ssid, password, server, token):
    cfg = forget_network(cfg)
    cfg.update({"ssid": ssid, "password": password, "server": server, "token": token})
    return cfg


def check_config(cfg):
    missing = [k for k in REQUIRED if k not in cfg or (k != "password" and not cfg[k])]
    if missing:
        raise ValueError("config.json is missing: " + ", ".join(missing))
    if not (cfg["server"].startswith("http://") or cfg["server"].startswith("https://")):
        raise ValueError("server must start with http:// or https://")
    pins = cfg.get("pins", {})
    if not isinstance(pins, dict) or any(k not in PIN_NAMES or not isinstance(v, int) for k, v in pins.items()):
        raise ValueError("pins must map " + ", ".join(PIN_NAMES) + " to GPIO numbers")
    led_pin = cfg.get("led_pin")
    if led_pin is not None and not isinstance(led_pin, (str, int)):
        raise ValueError("led_pin must be a pin name or number")
    if not isinstance(cfg.get("watchdog", True), bool):
        raise ValueError("watchdog must be true or false")
    cfg = dict(cfg)
    cfg["server"] = cfg["server"].rstrip("/")
    return cfg
