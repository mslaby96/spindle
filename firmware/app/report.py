LED_STATES = ("idle", "known", "unknown", "catalog_ready", "catalog_saved")
DEFAULT_INTERVAL_MS = 1500
MIN_INTERVAL_MS = 500
MAX_INTERVAL_MS = 10000
REQUIRED = ("ssid", "password", "server", "token")


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


def check_config(cfg):
    missing = [k for k in REQUIRED if not cfg.get(k)]
    if missing:
        raise ValueError("config.json is missing: " + ", ".join(missing))
    cfg = dict(cfg)
    cfg["server"] = cfg["server"].rstrip("/")
    return cfg
