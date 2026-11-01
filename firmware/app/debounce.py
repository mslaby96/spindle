class Debouncer:
    def __init__(self, absent_after=3, confirm_after=3):
        self.absent_after = absent_after
        self.confirm_after = confirm_after
        self.current = None
        self._candidate = None
        self._hits = 0
        self._misses = 0

    @property
    def candidate(self):
        """A sleeve that has been read but not often enough yet to count as on the stand."""
        return self._candidate

    def feed(self, uid):
        if uid is None:
            self._misses += 1
            self._candidate = None
            if self._misses >= self.absent_after:
                self.current = None
            return self.current

        self._misses = 0
        if uid == self.current:
            self._candidate = None
            return self.current

        if uid == self._candidate:
            self._hits += 1
        else:
            self._candidate = uid
            self._hits = 1
        if self._hits >= self.confirm_after:
            self.current = uid
            self._candidate = None
        return self.current
