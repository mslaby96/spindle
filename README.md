# Spindle

A listening diary for your vinyl collection.

Stick a small NFC tag on each record sleeve. When you put a record on, rest the sleeve on the Spindle stand and it notes what's playing. Over time you get a diary of what you listened to, what you play the most, and the records you haven't touched in ages.

- **Catalogue** your collection by placing sleeves on the stand one by one and picking the album (and pressing, if you like) from Discogs.
- **Listens are recorded automatically.** Take the sleeve off and the listen ends. Very short ones are ignored, and when a sleeve sat on the stand much longer than the album lasts, Spindle asks you what happened instead of guessing.
- **Rate and annotate** each listen, Letterboxd style.
- **Stats**: top albums, genres and styles, listens per week, and a forgotten shelf.
- **Home Assistant**: a "Now playing" sensor and the cover art over MQTT, so you can dim the lights when the needle drops.

Everything runs on your own machine. Spindle is a single Go binary with the web app built in, and it keeps its data in SQLite.

## Running the server

You need Docker and a free [Discogs personal access token](https://www.discogs.com/settings/developers).

```sh
git clone https://github.com/mslaby96/spindle.git
cd spindle/deploy
cp .env.example .env    # put your Discogs token in here
docker compose up -d
docker compose logs spindle | grep "setup code"
```

Open http://localhost:8080 and create your account with the setup code from the log. The code is only printed while there is no account yet.

Port 8080 taken? Run `SPINDLE_PORT=8090 docker compose up -d`.

### Configuration

All settings are environment variables, usually set in `deploy/.env`.

| Variable | Default | |
|---|---|---|
| `SPINDLE_DISCOGS_TOKEN` | | Discogs personal access token (required for search) |
| `SPINDLE_LASTFM_KEY` | | Optional Last.fm API key, one more source for album lengths |
| `SPINDLE_TZ` | system | Time zone for weekly stats, e.g. `Europe/Warsaw` |
| `SPINDLE_MQTT_URL` | | e.g. `mqtt://192.168.1.5:1883`, enables Home Assistant |
| `SPINDLE_MQTT_USERNAME`, `SPINDLE_MQTT_PASSWORD` | | Broker credentials |
| `SPINDLE_SECURE_COOKIES` | `false` | Set to `true` when Spindle sits behind HTTPS |
| `SPINDLE_ABSENCE_TOLERANCE` | `90s` | How long a sleeve can be off the stand without ending the listen |
| `SPINDLE_MIN_DURATION` | `5m` | Shorter listens are ignored |
| `SPINDLE_LONG_FACTOR` | `1.3` | Listens longer than this × the album length need confirming |
| `SPINDLE_FALLBACK_LONG` | `90m` | The same limit when the album length is unknown |
| `SPINDLE_AUTO_RESOLVE_AFTER` | `7d` | Unanswered confirmations count as "forgot to take it off" after this |
| `SPINDLE_FORGOTTEN_AFTER` | `180d` | When a record lands on the forgotten shelf |
| `SPINDLE_CATALOG_IDLE` | `10m` | Cataloguing switches off after this long without a new sleeve |

Forgot your password? `docker compose exec -it spindle /spindle reset-password <username>`

## Building the stand

Parts:

- Raspberry Pi Pico W
- RC522 (MFRC522) RFID reader module
- NTAG213 sticker tags, one per sleeve
- an LED, if you don't want to rely on the Pico's onboard one
- something to hold a sleeve upright with the reader behind its lower edge

Wiring. Power the reader from 3.3 V, never 5 V:

| RC522 | Pico W |
|---|---|
| SDA | GP1 |
| SCK | GP2 |
| MOSI | GP3 |
| MISO | GP4 |
| RST | GP0 |
| GND | GND |
| 3.3V | 3V3(OUT) |

The RC522 only reads a few centimetres, so stick the tags in the same spot on every sleeve (a lower corner works well) and mount the reader right behind that spot.

### Firmware

1. Flash [MicroPython for the Pico W](https://micropython.org/download/RPI_PICO_W/): hold BOOTSEL while plugging it in and drop the `.uf2` file onto the drive.
2. Install `mpremote`: `pipx install mpremote`.
3. In the web app go to **Stands → Add stand** and copy the token. It is shown once.
4. Copy `firmware/config.example.json` to `firmware/config.json` and fill in your Wi-Fi, the server address (as the Pico sees it, e.g. `http://192.168.1.10:8080`) and the token.
5. Upload it:

```sh
cd firmware
mpremote cp -r app lib :
mpremote cp main.py config.json :
mpremote reset
```

If your MicroPython build doesn't include `requests`, run `mpremote mip install requests` once.

To check the reader before setting everything up, upload `app` and `lib` and run `mpremote run tools/scan.py`. It prints the tag UID whenever a sleeve comes near.

While you're tinkering over USB, set `"watchdog": false` in `config.json`. The watchdog can't be stopped once it runs and keeps resetting the board after you interrupt it.

### What the LED means

| Pattern | Meaning |
|---|---|
| off | nothing on the stand |
| solid | playing a record Spindle knows |
| fast blink | a sleeve Spindle doesn't know yet, pick the album in the app |
| slow blink | cataloguing, waiting for the album to be picked |
| double flash | cataloguing, saved |
| short blip every 2 s | no connection to the server |
| triple flash | broken config or rejected token, check the USB console |

### Quick test

- A tagged sleeve shows up under **Now playing** within a couple of seconds.
- Taking it off for a moment and putting it back keeps the same listen going.
- Two sleeves stacked on the stand don't make it flicker between them.
- Turning the router off makes the LED blip, and it reconnects on its own when the router is back.

## Home Assistant

Point `SPINDLE_MQTT_URL` at the broker Home Assistant uses (with MQTT discovery enabled, which is the default). A **Spindle** device appears with:

- `sensor.spindle_now_playing`: `Artist – Album` or `idle`, with `artist`, `album`, `year`, `genres`, `styles`, `color_hex` and `color_rgb` attributes
- `image.spindle_cover`: the cover of what's playing

For example, to set a light to the cover's colour:

```yaml
automation:
  - alias: Light follows the record
    triggers:
      - trigger: state
        entity_id: sensor.spindle_now_playing
    conditions: "{{ trigger.to_state.state != 'idle' }}"
    actions:
      - action: light.turn_on
        target:
          entity_id: light.living_room
        data:
          rgb_color: "{{ state_attr('sensor.spindle_now_playing', 'color_rgb') }}"
          brightness_pct: 40
```

## Development

Requirements: Go 1.26+, Node 24, Python 3 for the firmware tests.

```sh
cd server && go test ./...
cd web && npm ci && npm test && npm run build
python3 -m unittest discover -s firmware/tests -t firmware
```

`npm run dev` in `web/` serves the app with hot reload and proxies `/api` to a server on port 8080 (`cd server && go run ./cmd/spindle`). `npm run build` writes into `server/internal/webui/dist`, which gets embedded in the binary.

Album data comes from [Discogs](https://www.discogs.com/). Album lengths also come from [MusicBrainz](https://musicbrainz.org/) and, optionally, [Last.fm](https://www.last.fm/).
