# Everything the setup portal decides, kept free of sockets and Wi-Fi so it can be tested on a computer.
import hashlib
import struct

NETWORK_KEYS = ("ssid", "password", "server", "token")


_PASSWORD_CHARS = "abcdefghjkmnpqrstuvwxyz23456789"


def setup_password(token):
    """The password of a paired stand's setup network. The server works out the same one from the hash it keeps of the
    token and shows it in the app, so only the stand's owner can join."""
    token_hash = "".join("{:02x}".format(b) for b in hashlib.sha256(token.encode()).digest())
    digest = hashlib.sha256(b"spindle-setup-wifi:" + token_hash.encode()).digest()
    return "".join(_PASSWORD_CHARS[b % len(_PASSWORD_CHARS)] for b in digest[:12])


def ap_name(board_id):
    return "Spindle-" + "".join("{:02X}".format(b) for b in board_id[-2:])


class Request:
    def __init__(self, method, path, host, body):
        self.method = method
        self.path = path
        self.host = host
        self.body = body


def _text(b):
    try:
        return bytes(b).decode("utf-8")
    except UnicodeError:
        return "".join(chr(c) if c < 128 else "?" for c in b)


def parse_request(data):
    """Returns the request once all of it has arrived, None while more is still to come."""
    end = data.find(b"\r\n\r\n")
    if end < 0:
        return None
    lines = _text(data[:end]).split("\r\n")
    parts = lines[0].split(" ")
    method, path = (parts[0], parts[1]) if len(parts) == 3 and parts[2].startswith("HTTP/") else ("", "")
    host, length = "", 0
    for line in lines[1:]:
        name, _, value = line.partition(":")
        name = name.strip().lower()
        if name == "host":
            host = value.strip()
        elif name == "content-length":
            try:
                length = int(value.strip())
            except ValueError:
                length = 0
    body = data[end + 4:]
    if len(body) < length:
        return None
    return Request(method, path, host, body[:length])


def _unquote(s):
    out = bytearray()
    i = 0
    while i < len(s):
        c = s[i]
        if c == "+":
            out.append(32)
        elif c == "%" and _is_hex(s[i + 1:i + 3]):
            out.append(int(s[i + 1:i + 3], 16))
            i += 2
        else:
            out.extend(c.encode())
        i += 1
    return _text(out)


def _is_hex(s):
    return len(s) == 2 and all(c in "0123456789abcdefABCDEF" for c in s)


def parse_form(body):
    form = {}
    for pair in _text(body).split("&"):
        if pair:
            name, _, value = pair.partition("=")
            form[_unquote(name)] = _unquote(value)
    return form


def check_setup(form, paired_server=None):
    """Returns what to connect with, or None and a message saying what to fix. A paired stand keeps its token and the
    server it was given, so it needs no pairing code, and the token can't be sent anywhere else."""
    ssid = (form.get("other") or "").strip() or (form.get("ssid") or "").strip()
    if not ssid:
        return None, "Pick your Wi-Fi network or type its name."
    server = paired_server or (form.get("server") or "").strip().rstrip("/")
    if not (server.startswith("http://") or server.startswith("https://")) or len(server) < 10:
        return None, "The server address must start with http:// or https://."
    network = {"ssid": ssid, "password": form.get("password") or "", "server": server}
    if paired_server:
        return network, None
    code = (form.get("code") or "").replace("-", "").replace(" ", "").upper()
    if len(code) != 6:
        return None, "The pairing code has six characters, as shown in the Spindle app."
    network["code"] = code
    return network, None


def escape(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")


_STYLE = (
    "body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#222}"
    "label{display:block;margin:1rem 0 .3rem;font-weight:600}"
    "input,select,button{width:100%;box-sizing:border-box;padding:.6rem;font-size:1rem}"
    "button{margin-top:1.5rem;background:#222;color:#fff;border:0;border-radius:.4rem}"
    ".error{color:#b00020}.muted{color:#666;font-size:.9rem}"
)


def _page(title, body, refresh=None):
    meta = '<meta http-equiv="refresh" content="{}">'.format(refresh) if refresh else ""
    return (
        '<!doctype html><html><head><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width,initial-scale=1">' + meta +
        "<title>" + escape(title) + "</title><style>" + _STYLE + "</style></head><body><h1>" +
        escape(title) + "</h1>" + body + "</body></html>"
    )


def setup_page(networks, values=None, error=None, paired_server=None):
    values = values or {}
    best = {}
    for ssid, rssi in networks:
        if ssid and (ssid not in best or rssi > best[ssid]):
            best[ssid] = rssi
    options = ['<option value="">Pick a network</option>']
    for ssid in sorted(best, key=lambda s: -best[s]):
        selected = " selected" if ssid == values.get("ssid") else ""
        options.append('<option value="{0}"{1}>{0}</option>'.format(escape(ssid), selected))
    body = (
        ('<p class="error">' + escape(error) + "</p>" if error else "") +
        '<form method="post" action="/setup">'
        '<label for="ssid">Wi-Fi network</label><select id="ssid" name="ssid">' + "".join(options) + "</select>"
        '<label for="other">Or type its name</label><input id="other" name="other" autocomplete="off">'
        '<label for="password">Wi-Fi password</label><input id="password" name="password" type="password">' +
        (_paired_fields(paired_server) if paired_server else _new_fields(values)) +
        "<button>Connect</button></form>"
    )
    return _page("Connect your stand" if paired_server else "Set up your stand", body)


def _paired_fields(server):
    return '<p class="muted">The stand stays paired with ' + escape(server) + ".</p>"


def _new_fields(values):
    # the pairing code is never shown again: anyone on this open network could read it off the page
    return (
        '<label for="server">Spindle server</label>'
        '<input id="server" name="server" inputmode="url" placeholder="http://192.168.1.10:8080" value="' +
        escape(values.get("server", "")) + '">'
        '<p class="muted">The address you open Spindle at, as seen from your home network.</p>'
        '<label for="code">Pairing code</label>'
        '<input id="code" name="code" autocapitalize="characters" autocomplete="off" placeholder="K7F-29Q">'
        '<p class="muted">In the Spindle app go to Stands and press Add stand.</p>'
    )


def working_page():
    return _page("Connecting…", "<p>Joining your Wi-Fi and the Spindle server. This page refreshes on its own.</p>"
                 '<p class="muted">If your phone leaves this network meanwhile, join it again.</p>', refresh=4)


def done_page():
    return _page("All set", "<p>The stand is connected. You can close this page and rejoin your usual Wi-Fi.</p>")


# Phones fetch one of these to see whether a network reaches the internet. Answering as the real servers would keeps
# them from opening a captive portal sheet, which on an iPhone closes, and leaves the network, as soon as you switch to
# another app to copy a password. The setup page is opened in the browser instead.
_SUCCESS = (200, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>")
_NO_CONTENT = (204, "")
# iOS asks several of its own hosts, some with made-up paths, so those answer whatever the path.
_ONLINE_HOSTS = {
    "captive.apple.com": _SUCCESS,
    "www.apple.com": _SUCCESS,
    "www.appleiphonecell.com": _SUCCESS,
    "www.itools.info": _SUCCESS,
    "www.ibook.info": _SUCCESS,
    "www.airport.us": _SUCCESS,
    "www.thinkdifferent.us": _SUCCESS,
    "connectivitycheck.gstatic.com": _NO_CONTENT,
    "connectivitycheck.android.com": _NO_CONTENT,
    "clients3.google.com": _NO_CONTENT,
}
_ONLINE_PATHS = {
    "/generate_204": _NO_CONTENT,
    "/gen_204": _NO_CONTENT,
    "/connecttest.txt": (200, "Microsoft Connect Test"),
    "/ncsi.txt": (200, "Microsoft NCSI"),
}


def online_answer(req):
    """What a connectivity check expects to see when the network reaches the internet, or None for other requests."""
    host = req.host.split(":")[0].lower()
    return _ONLINE_HOSTS.get(host) or _ONLINE_PATHS.get(req.path.split("?")[0])


def redirect_elsewhere(req, own_ip):
    """Any other name the phone looks up leads here, so http://spindle.setup opens the setup page."""
    return req.host.split(":")[0] not in ("", own_ip)


_REASONS = {200: "OK", 204: "No Content", 302: "Found", 303: "See Other", 404: "Not Found"}


def response(status, body="", location=None):
    data = body.encode()
    head = "HTTP/1.1 {} {}\r\n".format(status, _REASONS.get(status, "OK"))
    if location:
        head += "Location: " + location + "\r\n"
    head += "Content-Type: text/html; charset=utf-8\r\nCache-Control: no-store\r\n"
    head += "Content-Length: {}\r\nConnection: close\r\n\r\n".format(len(data))
    return head.encode() + data


def dns_reply(query, ip):
    """Answers an A query for any name with the portal's address. Returns None for anything that isn't a query."""
    if len(query) < 12:
        return None
    txid, flags, qdcount = struct.unpack(">HHH", query[:6])
    if flags & 0x8000 or qdcount != 1:
        return None
    i = 12
    while i < len(query) and query[i] != 0:
        i += query[i] + 1
    if i + 5 > len(query):
        return None
    qtype = struct.unpack(">H", query[i + 1:i + 3])[0]
    question = query[12:i + 5]
    answer = b""
    if qtype == 1:
        answer = b"\xc0\x0c" + struct.pack(">HHIH", 1, 1, 30, 4) + bytes(int(p) for p in ip.split("."))
    header = struct.pack(">HHHHHH", txid, 0x8180, 1, 1 if answer else 0, 0, 0)
    return header + question + answer


def route(req, own_ip, view):
    """Answers one request. view holds the portal's state ("form", "working" or "done"), the networks it found and
    what was typed last. Returns the response and, for a valid form, what to connect with."""
    if req is None or not req.method:
        return response(404, "Not found"), None
    online = online_answer(req) if redirect_elsewhere(req, own_ip) else None
    if online:
        return response(*online), None
    if redirect_elsewhere(req, own_ip):
        return response(302, location="http://" + own_ip + "/"), None
    if req.method == "POST" and req.path == "/setup":
        if view["state"] != "form":
            return response(303, location="/"), None
        form = parse_form(req.body)
        setup, error = check_setup(form, view.get("paired_server"))
        if error:
            return response(200, setup_page(view["networks"], form, error, view.get("paired_server"))), None
        return response(303, location="/"), setup
    if view["state"] == "working":
        return response(200, working_page()), None
    if view["state"] == "done":
        return response(200, done_page()), None
    return response(200, setup_page(view["networks"], view["values"], view["error"], view.get("paired_server"))), None
