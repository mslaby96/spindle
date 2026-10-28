LED_STATES = ("idle", "known", "unknown", "catalog_ready", "catalog_saved")
DEFAULT_INTERVAL_MS = 1500
MIN_INTERVAL_MS = 500
MAX_INTERVAL_MS = 10000
MAX_RETRY_MS = 30000
REQUIRED = ("ssid", "password", "server", "token")
PIN_NAMES = ("sck", "mosi", "miso", "cs", "rst", "spi_id")


def uid_hex(raw):
    return "".join("{:02X}".format(b) for b in raw)


def build(uid, fw, seq):
    return {"uid": uid.upper() if uid else None, "fw": fw, "seq": seq}


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


def retry_delay(interval, failures):
    return min(interval * 2 ** min(failures, 10), MAX_RETRY_MS)


def check_config(cfg):
    missing = [k for k in REQUIRED if not cfg.get(k)]
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
