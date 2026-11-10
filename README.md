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
cp .env.example .env && chmod 600 .env    # put your Discogs token in here
docker compose up -d
docker compose logs spindle | grep "setup code"
```

Open http://localhost:8080 and create your account with the setup code from the log. The code is only printed while there is no account yet and works for an hour; restart Spindle for a new one. Create your account before you make Spindle reachable from the internet.

Port 8080 taken? Run `SPINDLE_PORT=8090 docker compose up -d`.

### On Proxmox

`deploy/proxmox/spindle-lxc.sh` makes Spindle its own LXC container: an unprivileged Debian 12 container with Spindle running as a service, started at boot. Run it in the Proxmox host's shell:

```sh
bash -c "$(curl -fsSL https://raw.githubusercontent.com/mslaby96/spindle/main/deploy/proxmox/spindle-lxc.sh)"
```

It opens with a menu: default settings, advanced settings (container id, size, storage, bridge, a fixed address, starting at boot and a backup to start from), or updating an existing Spindle. It asks for your Discogs token without showing it, prints the address and the setup code at the end, and needs no Docker. `--yes` skips the menus and takes the defaults, for scripts. Add `--restore spindle-backup-….tar.gz` to start from a backup of another Spindle, and see `--help` for the container's id, size, storage and a static address. Give the container a fixed address, since a stand keeps the server's address it was set up with.

To update later, run it again with `--update <container id>`: it backs up first, then puts the newest Spindle in. Settings live in `/etc/spindle/spindle.env` inside the container. For backups on another disk, mount it at `/var/lib/spindle/backups` (for example `pct set <id> -mp0 /mnt/nas/spindle,mp=/var/lib/spindle/backups`).

### Without Docker

Spindle is a single file. `deploy/build-release.sh` builds it for Linux into `release/`. Run it with `spindle serve` and the settings below as environment variables; `deploy/proxmox/spindle-lxc.sh` shows a systemd service for it.

### Configuration

All settings are environment variables, usually set in `deploy/.env`.

| Variable | Default | |
|---|---|---|
| `SPINDLE_DISCOGS_TOKEN` | | Discogs personal access token (required for search) |
| `SPINDLE_LASTFM_KEY` | | Optional Last.fm API key, one more source for album lengths |
| `SPINDLE_TZ` | system | Time zone for weekly stats, e.g. `Europe/Warsaw` |
| `SPINDLE_MQTT_URL` | | e.g. `mqtt://192.168.1.5:1883`, enables Home Assistant |
| `SPINDLE_MQTT_USERNAME`, `SPINDLE_MQTT_PASSWORD` | | Broker credentials |
| `SPINDLE_SECURE_COOKIES` | `false` | The login cookie is marked secure whenever the browser comes over HTTPS; `true` marks it so always |
| `SPINDLE_TRUSTED_PROXIES` | | Addresses or networks of your reverse proxy, e.g. `172.16.0.0/12`, so Spindle sees the real client and that it used HTTPS |
| `SPINDLE_ABSENCE_TOLERANCE` | `90s` | How long a sleeve can be off the stand without ending the listen |
| `SPINDLE_MIN_DURATION` | `5m` | Shorter listens are ignored |
| `SPINDLE_LONG_FACTOR` | `1.3` | Listens longer than this × the album length need confirming |
| `SPINDLE_FALLBACK_LONG` | `90m` | The same limit when the album length is unknown |
| `SPINDLE_AUTO_RESOLVE_AFTER` | `7d` | Unanswered confirmations count as "forgot to take it off" after this |
| `SPINDLE_FORGOTTEN_AFTER` | `180d` | When a record lands on the forgotten shelf |
| `SPINDLE_CATALOG_IDLE` | `10m` | Cataloguing switches off after this long without a new sleeve |
| `SPINDLE_BACKUP_PATH` | `./backups` | With Docker: the folder on this machine that backups are written to |
| `SPINDLE_BACKUP_DIR` | `<data>/backups` | Without Docker: where backups are written |
| `SPINDLE_BACKUP_EVERY` | `24h` | How often a backup is made; `off` turns them off |
| `SPINDLE_BACKUP_KEEP` | `7` | How many backups are kept; older ones are deleted |

Forgot your password? `docker compose exec -it spindle /spindle reset-password <username>`

Cover colours look off after an update? `docker compose exec spindle /spindle recolor-covers` picks them again from the stored covers.

### Reaching Spindle from outside

Put Spindle behind a reverse proxy that does HTTPS, such as Caddy, and list the proxy in `SPINDLE_TRUSTED_PROXIES`. Without that, Spindle sees every visitor as the proxy, so one person mistyping a password slows down logins for everyone, and it can't tell that the browser used HTTPS. With it, the login cookie only travels over HTTPS and browsers are told to stick to HTTPS.

The stand talks plain HTTP, so give it the server's address on your home network, not the public one.

### Backups

Spindle backs itself up: once a day by default it writes `spindle-backup-<date>-<time>.tar.gz`, with the database and the covers, into `SPINDLE_BACKUP_PATH` and keeps the last seven. The server doesn't have to stop for it. Point `SPINDLE_BACKUP_PATH` at a folder that is itself backed up, or at another disk, so a failing disk doesn't take the backups with it. Running Spindle in the cloud, mount a volume or bucket there instead. On Linux the folder must be writable by the container's user (uid 65532), e.g. `sudo chown 65532 backups`.

To make one right away: `docker compose exec spindle /spindle backup`.

To restore one, stop the server first:

```sh
docker compose stop spindle
docker compose run --rm spindle restore /backups/spindle-backup-20261101-030000.tar.gz
docker compose start spindle
```

Restore checks the backup before touching anything and moves the data that was there into a `before-restore-*` folder instead of deleting it.

Already keeping your records on Discogs? **Settings → Import from Discogs** brings in the collection of the account your `SPINDLE_DISCOGS_TOKEN` belongs to. It only adds: run it again whenever you've bought something, and albums you removed from your collection in Spindle stay removed. A backup is made first.

Your own data is also a download away: **Settings → Your data** gives you everything as JSON, or your diary as a CSV for a spreadsheet.

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

The RC522 only reads about a centimetre with small stickers, and it loses a tag that lies directly on the antenna, so leave 3-5 mm of plastic or cardboard in between.

### Printed stand

`hardware/stand/` has a printable stand: `stand.scad` is the parametric source, the `.stl` files are ready to slice.

- `body.stl` prints upright. The ceiling of the hollow base needs support: normal (grid) supports on the build plate only, threshold angle 10° so none end up in the reader slot, 5 mm base spacing, 1 mm support/object XY distance and no bottom interface layers so the walls break out one by one.
- `cover.stl` is a shallow tray that goes under the base and adds 6 mm of room inside. It prints as exported, floor on the bed, with the spacer that holds the reader up standing on it. Change `tray_h` in `stand.scad` for more or less room.
- `foot.stl` prints as exported. You need four.
- `clip.stl` prints standing up, as exported.

Push the clip's two pegs into the mounting holes at the antenna end of the RC522, on the side with the parts, so its bow points away from the board. Slide the reader into the slot in the base from below, antenna end first, header end last and the pins pointing towards the back, until it stops. The clip presses the antenna against the front of the slot. The strip with the header stays inside the base, so a straight header with jumper wires fits. Put the tray under the base so its rim drops into the base and the spacer slides in under the reader. Each M3 x 20 mm machine screw goes through a foot, the tray's floor and the post above it into a boss and cuts its own thread there, so drive it in slowly. If the feet slip on your table, print them in TPU or stick felt under them.

Stick each tag on the back of the sleeve's outer sleeve, centred, 24 mm above the bottom edge (the value is printed when you render `stand.scad`). Line the sleeve up with the notch on the front of the stand and the tag lands on the mark over the reader.

### Firmware

1. Flash [MicroPython for the Pico W](https://micropython.org/download/RPI_PICO_W/): hold BOOTSEL while plugging it in and drop the `.uf2` file onto the drive.
2. Install `mpremote` (`pipx install mpremote`) and upload the firmware:

```sh
cd firmware
mpremote mkdir :lib
mpremote cp -r app :
mpremote cp lib/mfrc522.py :lib/
mpremote cp main.py :
mpremote cp ota.py :
mpremote reset
```

If your MicroPython build doesn't include `requests`, run `mpremote mip install requests` once.

### Updating the stand

You only need USB once. The Docker image carries the stand's firmware, so after you update the server, the Stands page shows when a stand runs a different version and offers **Update**. The stand picks it up on its next report while nothing is on it, downloads the new files next to the old ones, checks each one and restarts into them.

If the new version keeps crashing, or can't reach the server for ten minutes, the stand goes back to the version it had and the app says the update didn't take. Updates only replace files in `app/` and `lib/`: `main.py` and `ota.py`, which do the rolling back, `config.json` and any other files you keep on the Pico are never touched. MicroPython itself isn't updated this way.

### Connecting the stand

1. In the web app go to **Stands**, type a name and press **Add stand**. You get a pairing code that works for ten minutes.
2. Plug the stand in. With no settings yet, it opens its own Wi-Fi network called `Spindle-` and four characters, and the LED stays on with a short break every two seconds.
3. Join that network on your phone, then open `http://spindle.setup` (or `http://192.168.4.1`) in the browser. The stand tells the phone the network is online, so no captive portal sheet pops up and you can switch apps, say to copy your Wi-Fi password, without losing the page.
4. Pick your Wi-Fi, type its password, the server address (the one you open Spindle at, e.g. `http://192.168.1.10:8080`, or your cloud address) and the pairing code.
5. The stand joins your Wi-Fi, swaps the code for its own token and restarts. The app shows it as connected.

To move a stand to another Wi-Fi, press **Change Wi-Fi** next to it on the Stands page. Within half a minute it forgets its network and opens its setup network again. A paired stand protects that network with a password, shown next to the stand in the app, and its page asks only for the Wi-Fi: the stand stays paired with the same server and keeps its history. The stand also opens its setup network on its own when it can't reach your Wi-Fi for five minutes, and keeps trying the old network meanwhile, so a router that is already gone is covered too.

Holding the BOOTSEL button on the Pico for five seconds while the stand is running forgets everything, pairing included, and starts over from step 2.

You can still configure a stand by hand: press **Set up over USB instead** in the app, copy `firmware/config.example.json` to `config.json` with your Wi-Fi, server and that token, and upload it with `mpremote cp config.json :`.

To check the reader before setting everything up, upload `app` and `lib/mfrc522.py` as above and run `mpremote run tools/scan.py`. It prints the tag UID whenever a sleeve comes near.

An empty stand checks its reader every 30 seconds, and if the reader has got stuck, as cheap RC522 boards sometimes do after a dip in power, it resets it and carries on.

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
| on, short break every 2 s | waiting to be set up, join its Wi-Fi network |
| quick flicker | downloading a firmware update |

### Quick test

- A tagged sleeve shows up under **Now playing** within a couple of seconds.
- Taking it off for a moment and putting it back keeps the same listen going.
- Two sleeves stacked on the stand don't make it flicker between them.
- Turning the router off makes the LED blip, and it reconnects on its own when the router is back.

## Home Assistant

Point `SPINDLE_MQTT_URL` at the broker Home Assistant uses (with MQTT discovery enabled, which is the default). A **Spindle** device appears with:

- `sensor.spindle_now_playing`: `Artist – Album` or `idle`, with `artist`, `album`, `year`, `genres`, `styles`, `color_hex` and `color_rgb` attributes
- `image.spindle_cover`: the cover of what's playing

Each stand also shows up as its own device, linked to Spindle, with whether it is connected, its Wi-Fi signal, reader resets and uptime, and a firmware update entity whose **Install** button does the same as **Update** in the app.

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

CI runs the same tests, with `go test -race`, plus `go vet`, `gofmt`, `govulncheck`, `npm audit`, a Docker build, and [crap4go](https://github.com/unclebob/crap4go): no function may reach a CRAP score of 10. Run that one locally with `crap4go --test-command "go test ./internal/..."` in `server/`.

`npm run dev` in `web/` serves the app with hot reload and proxies `/api` to a server on port 8080 (`cd server && go run ./cmd/spindle`). `npm run build` writes into `server/internal/webui/dist`, which gets embedded in the binary.

Album data comes from [Discogs](https://www.discogs.com/). Album lengths also come from [MusicBrainz](https://musicbrainz.org/) and, optionally, [Last.fm](https://www.last.fm/).

This application uses Discogs' API but is not affiliated with, sponsored or endorsed by Discogs. 'Discogs' is a trademark of Zink Media, LLC.

## License

Copyright 2026 Maciej Słaby.

The software (server, web app and stand firmware) is licensed under the [GNU Affero General Public License v3.0](LICENSE): you can use, change and share it, and if you run a changed version for others, for example as a hosted service, you have to share its source with them too.

The hardware in [`hardware/`](hardware/) (the printed stand and its parts) is licensed under the [CERN Open Hardware Licence v2 – Strongly Reciprocal](hardware/LICENSE).

The RC522 driver in `firmware/lib/mfrc522.py` is by Stefan Wendler under the [MIT License](firmware/LICENSE-mfrc522).

