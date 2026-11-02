import struct
import unittest

from app import portal


def dns_query(name, qtype=1, txid=0x1234):
    q = b"".join(bytes([len(p)]) + p.encode() for p in name.split(".")) + b"\0"
    return struct.pack(">HHHHHH", txid, 0x0100, 1, 0, 0, 0) + q + struct.pack(">HH", qtype, 1)


class ApNameTest(unittest.TestCase):
    def test_uses_the_end_of_the_board_id(self):
        self.assertEqual(portal.ap_name(bytes([0xE6, 0x61, 0x3F, 0x2A])), "Spindle-3F2A")


class RequestTest(unittest.TestCase):
    def test_get(self):
        req = portal.parse_request(b"GET /generate_204 HTTP/1.1\r\nHost: connectivitycheck.gstatic.com\r\n\r\n")
        self.assertEqual((req.method, req.path, req.host), ("GET", "/generate_204", "connectivitycheck.gstatic.com"))

    def test_post_waits_for_the_whole_body(self):
        head = b"POST /setup HTTP/1.1\r\nHost: 192.168.4.1\r\nContent-Length: 9\r\n\r\n"
        self.assertIsNone(portal.parse_request(head + b"ssid="))
        self.assertEqual(portal.parse_request(head + b"ssid=home").body, b"ssid=home")

    def test_incomplete_headers(self):
        self.assertIsNone(portal.parse_request(b"GET / HTTP/1.1\r\nHost: x"))

    def test_garbage(self):
        self.assertEqual(portal.parse_request(b"\x16\x03\x01garbage\r\n\r\n").method, "")


class FormTest(unittest.TestCase):
    def test_decodes_plus_and_percent_escapes(self):
        form = portal.parse_form(b"ssid=My+Home+%C5%BC&password=p%26ss%3Dw0rd&empty=")
        self.assertEqual(form, {"ssid": "My Home ż", "password": "p&ss=w0rd", "empty": ""})

    def test_ignores_broken_escapes(self):
        self.assertEqual(portal.parse_form(b"a=100%25&b=%zz"), {"a": "100%", "b": "%zz"})


class CheckSetupTest(unittest.TestCase):
    def valid(self, **over):
        form = {"ssid": "Home", "other": "", "password": "secret", "server": "http://192.168.1.10:8080/", "code": "k7f 29q"}
        form.update(over)
        return form

    def test_valid(self):
        setup, error = portal.check_setup(self.valid())
        self.assertIsNone(error)
        self.assertEqual(setup, {"ssid": "Home", "password": "secret", "server": "http://192.168.1.10:8080", "code": "K7F29Q"})

    def test_typed_network_wins_over_the_list(self):
        setup, _ = portal.check_setup(self.valid(other="  Hidden net "))
        self.assertEqual(setup["ssid"], "Hidden net")

    def test_open_network_needs_no_password(self):
        self.assertIsNone(portal.check_setup(self.valid(password=""))[1])

    def test_errors_say_what_to_fix(self):
        cases = {
            "network": self.valid(ssid="", other=""),
            "server": self.valid(server="192.168.1.10:8080"),
            "code": self.valid(code="K7F-29"),
        }
        for field, form in cases.items():
            setup, error = portal.check_setup(form)
            self.assertIsNone(setup)
            self.assertIn(field, error.lower())

    def test_a_paired_stand_needs_no_code_and_keeps_its_server(self):
        form = self.valid(code="", server="http://attacker.example")
        setup, error = portal.check_setup(form, paired_server="http://192.168.1.10:8080")
        self.assertIsNone(error)
        self.assertEqual(setup, {"ssid": "Home", "password": "secret", "server": "http://192.168.1.10:8080"})

    def test_https_server_is_fine(self):
        self.assertEqual(portal.check_setup(self.valid(server="https://spindle.example.com"))[0]["server"],
                         "https://spindle.example.com")


class PageTest(unittest.TestCase):
    def test_lists_networks_strongest_first_without_repeats(self):
        page = portal.setup_page([("Weak", -80), ("Home", -40), ("Home", -70), ("", -30)])
        self.assertLess(page.index(">Home<"), page.index(">Weak<"))
        self.assertEqual(page.count(">Home<"), 1)
        self.assertNotIn("<option value=\"\">", page.replace('<option value="">Pick a network</option>', ""))

    def test_escapes_what_it_shows(self):
        page = portal.setup_page([("<script>", -50)], {"server": '"><b>'}, "bad & wrong")
        self.assertNotIn("<script>", page)
        self.assertNotIn('"><b>', page)
        self.assertIn("bad &amp; wrong", page)

    def test_keeps_what_was_typed(self):
        page = portal.setup_page([("Home", -40)], {"ssid": "Home", "server": "http://10.0.0.2:8080"})
        self.assertIn('<option value="Home" selected>', page)
        self.assertIn('value="http://10.0.0.2:8080"', page)

    def test_a_paired_stand_asks_only_for_the_network(self):
        page = portal.setup_page([("Home", -40)], {"server": "http://10.0.0.2:8080"}, paired_server="http://10.0.0.2:8080")
        self.assertNotIn('name="code"', page)
        self.assertNotIn('name="server"', page)
        self.assertIn("http://10.0.0.2:8080", page)
        self.assertIn('name="code"', portal.setup_page([("Home", -40)]))

    def test_never_shows_a_pairing_code_again(self):
        page = portal.setup_page([("Home", -40)], {"code": "K7F29Q"}, "Couldn't join Home.")
        self.assertNotIn("K7F29Q", page)

    def test_never_echoes_the_password(self):
        self.assertNotIn("hunter2", portal.setup_page([], {"password": "hunter2"}))


class ResponseTest(unittest.TestCase):
    def test_other_hosts_are_sent_to_the_portal(self):
        req = portal.parse_request(b"GET /hotspot-detect.html HTTP/1.1\r\nHost: captive.apple.com\r\n\r\n")
        self.assertTrue(portal.redirect_elsewhere(req, "192.168.4.1"))
        own = portal.parse_request(b"GET / HTTP/1.1\r\nHost: 192.168.4.1\r\n\r\n")
        self.assertFalse(portal.redirect_elsewhere(own, "192.168.4.1"))

    def test_response_shape(self):
        resp = portal.response(302, location="http://192.168.4.1/")
        self.assertTrue(resp.startswith(b"HTTP/1.1 302 Found\r\n"))
        self.assertIn(b"Location: http://192.168.4.1/\r\n", resp)
        page = portal.response(200, "<p>ż</p>")
        self.assertIn(b"Content-Length: 9\r\n", page)
        self.assertTrue(page.endswith("<p>ż</p>".encode()))


class DnsTest(unittest.TestCase):
    def test_answers_every_name_with_the_portal_address(self):
        reply = portal.dns_reply(dns_query("connectivitycheck.gstatic.com"), "192.168.4.1")
        txid, flags, qd, an = struct.unpack(">HHHH", reply[:8])
        self.assertEqual((txid, qd, an), (0x1234, 1, 1))
        self.assertTrue(flags & 0x8000)
        self.assertEqual(reply[-4:], bytes([192, 168, 4, 1]))

    def test_other_record_types_get_no_answer(self):
        reply = portal.dns_reply(dns_query("example.com", qtype=28), "192.168.4.1")
        self.assertEqual(struct.unpack(">H", reply[6:8])[0], 0)

    def test_ignores_garbage_and_replies(self):
        self.assertIsNone(portal.dns_reply(b"\x00\x01", "192.168.4.1"))
        reply = portal.dns_reply(dns_query("a.b"), "192.168.4.1")
        self.assertIsNone(portal.dns_reply(reply, "192.168.4.1"))


if __name__ == "__main__":
    unittest.main()


def request(raw):
    return portal.parse_request(raw)


class RouteTest(unittest.TestCase):
    IP = "192.168.4.1"
    FORM = b"ssid=Home&other=&password=pw&server=http%3A%2F%2F192.168.1.10%3A8080&code=K7F-29Q"

    def view(self, **over):
        v = {"state": "form", "networks": [("Home", -40)], "values": {}, "error": None}
        v.update(over)
        return v

    def post(self, body):
        return request(b"POST /setup HTTP/1.1\r\nHost: 192.168.4.1\r\nContent-Length: %d\r\n\r\n" % len(body) + body)

    def probe(self, host, path):
        return portal.route(request(b"GET %s HTTP/1.1\r\nHost: %s\r\n\r\n" % (path, host)), self.IP, self.view())

    def test_tells_phones_the_network_is_online_so_they_stay_on_it(self):
        apple, _ = self.probe(b"captive.apple.com", b"/hotspot-detect.html")
        self.assertTrue(apple.startswith(b"HTTP/1.1 200"))
        self.assertIn(b"<TITLE>Success</TITLE>", apple)
        self.assertNotIn(b"Set up your stand", apple)
        android, _ = self.probe(b"connectivitycheck.gstatic.com", b"/generate_204")
        self.assertTrue(android.startswith(b"HTTP/1.1 204"))
        windows, _ = self.probe(b"www.msftconnecttest.com", b"/connecttest.txt")
        self.assertTrue(windows.endswith(b"Microsoft Connect Test"))

    def test_apple_and_google_check_hosts_are_online_whatever_the_path(self):
        for host, path in [(b"captive.apple.com", b"/"), (b"www.apple.com", b"/library/test/success.html"),
                           (b"www.ibook.info", b"/x3k2/p0q9/abc.html"), (b"captive.apple.com", b"/hotspot-detect.html?x=1")]:
            resp, _ = self.probe(host, path)
            self.assertIn(b"<TITLE>Success</TITLE>", resp, host + path)
        for host in (b"clients3.google.com", b"connectivitycheck.android.com", b"www.google.com"):
            resp, _ = self.probe(host, b"/generate_204")
            self.assertTrue(resp.startswith(b"HTTP/1.1 204"), host)

    def test_any_other_name_leads_to_the_setup_page(self):
        resp, setup = self.probe(b"spindle.setup", b"/")
        self.assertTrue(resp.startswith(b"HTTP/1.1 302"))
        self.assertIn(b"Location: http://192.168.4.1/", resp)
        self.assertIsNone(setup)

    def test_shows_the_form(self):
        resp, _ = portal.route(request(b"GET / HTTP/1.1\r\nHost: 192.168.4.1\r\n\r\n"), self.IP, self.view())
        self.assertIn(b"Set up your stand", resp)
        self.assertIn(b">Home<", resp)

    def test_valid_form_starts_connecting(self):
        resp, setup = portal.route(self.post(self.FORM), self.IP, self.view())
        self.assertTrue(resp.startswith(b"HTTP/1.1 303"))
        self.assertEqual(setup["code"], "K7F29Q")

    def test_invalid_form_comes_back_with_what_was_typed(self):
        resp, setup = portal.route(self.post(self.FORM.replace(b"K7F-29Q", b"K7F")), self.IP, self.view())
        self.assertIsNone(setup)
        self.assertIn(b"six characters", resp)
        self.assertIn(b'value="http://192.168.1.10:8080"', resp)

    def test_shows_progress_and_result(self):
        get = request(b"GET / HTTP/1.1\r\nHost: 192.168.4.1\r\n\r\n")
        self.assertIn(b"Connecting", portal.route(get, self.IP, self.view(state="working"))[0])
        self.assertIn(b"All set", portal.route(get, self.IP, self.view(state="done"))[0])
        failed = portal.route(get, self.IP, self.view(error="Couldn't join Home."))[0]
        self.assertIn(b"Couldn't join Home.", failed)

    def test_a_paired_stand_takes_the_form_without_a_code(self):
        body = b"ssid=Home&other=&password=pw&server=http%3A%2F%2F192.168.1.10%3A8080"
        paired = self.view(paired_server="http://192.168.1.10:8080")
        resp, setup = portal.route(self.post(body), self.IP, paired)
        self.assertEqual(setup["ssid"], "Home")
        page, _ = portal.route(request(b"GET / HTTP/1.1\r\nHost: 192.168.4.1\r\n\r\n"), self.IP, paired)
        self.assertNotIn(b'name="code"', page)

    def test_second_submit_while_connecting_is_ignored(self):
        resp, setup = portal.route(self.post(self.FORM), self.IP, self.view(state="working"))
        self.assertIsNone(setup)
        self.assertTrue(resp.startswith(b"HTTP/1.1 303"))

    def test_garbage(self):
        resp, setup = portal.route(request(b"\x16\x03\x01\r\n\r\n"), self.IP, self.view())
        self.assertTrue(resp.startswith(b"HTTP/1.1 404"))
        self.assertIsNone(setup)


class SetupPasswordTest(unittest.TestCase):
    def test_matches_what_the_server_shows(self):
        # the same example is in the server's tests, so both sides keep deriving the same password
        self.assertEqual(portal.setup_password("spd_example"), "d97zws8agpqh")

    def test_differs_per_stand(self):
        self.assertNotEqual(portal.setup_password("spd_a"), portal.setup_password("spd_b"))
