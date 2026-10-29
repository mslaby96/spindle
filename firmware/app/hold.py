class Hold:
    """Tells when a button has been held down for long enough, once per press."""

    def __init__(self, hold_ms):
        self._hold_ms = hold_ms
        self._since = None
        self._fired = False

    def feed(self, pressed, now_ms):
        if not pressed:
            self._since, self._fired = None, False
            return False
        if self._since is None:
            self._since = now_ms
        if not self._fired and now_ms - self._since >= self._hold_ms:
            self._fired = True
            return True
        return False
