import time

from app.reader import Reader

reader = Reader()
last = None
print("Hold a tagged sleeve over the reader. Ctrl+C to stop.")
while True:
    uid = reader.read()
    if uid != last:
        print(time.ticks_ms(), uid or "-")
        last = uid
    time.sleep_ms(200)
