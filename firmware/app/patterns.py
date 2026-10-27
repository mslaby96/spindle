# Each pattern alternates on/off durations in milliseconds, starting with "on".
_PATTERNS = {
    "idle": (0, 1000),
    "known": (1000, 0),
    "unknown": (100, 100),
    "catalog_ready": (600, 600),
    "catalog_saved": (80, 80, 80, 760),
    "offline": (40, 1960),
    "error": (100, 100, 100, 100, 100, 1400),
}


def for_state(state):
    return _PATTERNS.get(state, _PATTERNS["unknown"])


def level(pattern, t_ms):
    t = t_ms % sum(pattern)
    on = True
    for step in pattern:
        if t < step:
            return on
        t -= step
        on = not on
    return on
