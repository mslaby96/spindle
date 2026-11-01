import ota

flash = ota.Flash()
ota.boot(flash)
if ota.trying(flash):
    # Until a new version has reached the server, a crash or a hang ends in a reset, and enough of those roll it back.
    from machine import WDT

    WDT(timeout=8000)

from app.runner import run

run()
