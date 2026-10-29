import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, mockApi, renderApp } from "./test-utils";

const me = { id: 1, username: "maciej" };

afterEach(() => vi.unstubAllGlobals());

describe("first run", () => {
  it("shows setup and lands on an empty now playing view", async () => {
    let created = false;
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: !created }],
      "POST /api/setup": () => {
        created = true;
        return [201, { username: "maciej" }];
      },
      "GET /api/me": () => (created ? [200, me] : [401, { error: "not logged in" }]),
      "GET /api/devices": () => [200, []],
      "GET /api/now-playing": () => [200, []],
    });
    renderApp();

    expect(await screen.findByRole("heading", { name: "Welcome to Spindle" })).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Setup code"), "ABCD234567");
    await userEvent.type(screen.getByLabelText("Username"), "maciej");
    await userEvent.type(screen.getByLabelText("Password"), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));

    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
    expect(calls.find((c) => c.key === "POST /api/setup")?.body).toEqual({
      code: "ABCD234567",
      username: "maciej",
      password: "correct horse",
    });
  });
});

describe("login", () => {
  it("redirects to login when the session is missing and sends remember me", async () => {
    let loggedIn = false;
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => (loggedIn ? [200, me] : [401, { error: "not logged in" }]),
      "POST /api/login": () => {
        loggedIn = true;
        return [204];
      },
      "GET /api/devices": () => [200, []],
    });
    renderApp("/devices");

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByLabelText("Remember me")).toBeChecked();
    await userEvent.type(screen.getByLabelText("Username"), "maciej");
    await userEvent.type(screen.getByLabelText("Password"), "correct horse");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("heading", { name: "Stands" })).toBeInTheDocument();
    expect(calls.find((c) => c.key === "POST /api/login")?.body).toMatchObject({ remember: true });
  });
});

describe("now playing", () => {
  it("follows what the stand reports", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, [{ id: 3, name: "Living room", created_at: "2026-01-01T00:00:00Z" }]],
      "GET /api/now-playing": () => [200, []],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    const stream = () => FakeEventSource.instances.at(-1)!;

    act(() =>
      stream().emit("now_playing", {
        device_id: 3,
        active: true,
        uid: "04A1B2C3D4E5F6",
        album: { id: 1, title: "The Dark Side Of The Moon", artist: "Pink Floyd", year: 1973, styles: ["Prog Rock"], genres: [], has_cover: true, duration_status: "resolved" },
        started_at: "2026-03-14T20:00:00Z",
      }),
    );
    expect(await screen.findByRole("heading", { name: "The Dark Side Of The Moon" })).toBeInTheDocument();
    expect(screen.getByText("Pink Floyd")).toBeInTheDocument();

    act(() =>
      stream().emit("now_playing", {
        device_id: 3,
        active: true,
        uid: "04A1B2C3D4E5F6",
        album: { id: 1, title: "The Dark Side Of The Moon", artist: "Pink Floyd", year: 1973, styles: ["Prog Rock"], genres: [], has_cover: true, duration_status: "resolved" },
        started_at: "2026-03-14T20:00:00Z",
        ends_at: new Date(Date.now() + 80_000).toISOString(),
      }),
    );
    expect(await screen.findByText(/This listen ends in 1:(19|20) unless you put it back/)).toBeInTheDocument();

    act(() => stream().emit("now_playing", { device_id: 3, active: false }));
    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
  });

  it("offers to assign an unknown sleeve while it plays", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "GET /api/now-playing": () => [200, [{ device_id: 3, active: true, uid: "04FFFFFF" }]],
    });
    renderApp();
    expect(await screen.findByRole("heading", { name: "New sleeve" })).toBeInTheDocument();
    expect(screen.getByLabelText("Search albums")).toBeInTheDocument();
  });
});

describe("live events", () => {
  it("reconnects after the stream closes", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "GET /api/now-playing": () => [200, []],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));

    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const first = FakeEventSource.instances[0];
      act(() => {
        first.readyState = FakeEventSource.CLOSED;
        first.onerror?.();
      });
      act(() => vi.advanceTimersByTime(3000));
    } finally {
      vi.useRealTimers();
    }
    expect(FakeEventSource.instances.length).toBe(2);
    act(() =>
      FakeEventSource.instances[1].emit("now_playing", {
        device_id: 1,
        active: true,
        uid: "04A1",
        album: { id: 1, title: "Rumours", artist: "Fleetwood Mac", styles: [], genres: [], has_cover: false, duration_status: "resolved" },
      }),
    );
    expect(await screen.findByRole("heading", { name: "Rumours" })).toBeInTheDocument();
  });
});

describe("stands", () => {
  it("pairs a new stand with a code", async () => {
    let devices: unknown[] = [];
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, devices],
      "POST /api/devices/pairing": () => [201, { code: "K7F-29Q", expires_at: new Date(Date.now() + 600_000).toISOString() }],
    });
    renderApp("/devices");

    await userEvent.type(await screen.findByPlaceholderText(/Stand name/), "Living room");
    await userEvent.click(screen.getByRole("button", { name: "Add stand" }));

    expect(await screen.findByText("K7F-29Q")).toBeInTheDocument();
    expect(screen.getByText(/Spindle-/)).toBeInTheDocument();
    expect(screen.getByText(/Code expires in (9:59|10:00)/)).toBeInTheDocument();
    expect(calls.find((c) => c.key === "POST /api/devices/pairing")?.body).toEqual({ name: "Living room" });

    devices = [{ id: 1, name: "Living room", fw_version: "0.2.0", created_at: "2026-01-01T00:00:00Z" }];
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    act(() => FakeEventSource.instances.at(-1)!.emit("device_paired", devices[0]));

    expect(await screen.findByText("Living room is connected.")).toBeInTheDocument();
    expect(screen.queryByText("K7F-29Q")).not.toBeInTheDocument();
    expect(await screen.findByText(/firmware 0.2.0/)).toBeInTheDocument();
  });

  it("offers a new code once the old one runs out", async () => {
    let issued = 0;
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "POST /api/devices/pairing": () => {
        issued++;
        return [201, { code: issued === 1 ? "AAA-AAA" : "BBB-BBB", expires_at: new Date(Date.now() + (issued === 1 ? 500 : 600_000)).toISOString() }];
      },
    });
    renderApp("/devices");
    await userEvent.type(await screen.findByPlaceholderText(/Stand name/), "Kitchen");
    await userEvent.click(screen.getByRole("button", { name: "Add stand" }));
    expect(await screen.findByText("AAA-AAA")).toBeInTheDocument();

    await userEvent.click(await screen.findByRole("button", { name: "New code" }, { timeout: 3000 }));
    expect(await screen.findByText("BBB-BBB")).toBeInTheDocument();
  });

  it("shows a token once for setting a stand up over USB", async () => {
    let devices: unknown[] = [];
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, devices],
      "POST /api/devices": (body) => {
        const device = { id: 1, name: (body as { name: string }).name, created_at: "2026-01-01T00:00:00Z" };
        devices = [device];
        return [201, { device, token: "spd_secret_token" }];
      },
    });
    renderApp("/devices");

    await userEvent.type(await screen.findByPlaceholderText(/Stand name/), "Living room");
    await userEvent.click(screen.getByRole("button", { name: "Set up over USB instead" }));

    expect(await screen.findByText("spd_secret_token")).toBeInTheDocument();
    expect(await screen.findByText("Living room")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("spd_secret_token")).not.toBeInTheDocument();
  });
});

describe("pairing instructions", () => {
  function pairingApi() {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "POST /api/devices/pairing": () => [201, { code: "K7F-29Q", expires_at: new Date(Date.now() + 600_000).toISOString() }],
    });
  }

  async function startPairing() {
    renderApp("/devices");
    await userEvent.type(await screen.findByPlaceholderText(/Stand name/), "Living room");
    await userEvent.click(screen.getByRole("button", { name: "Add stand" }));
    return screen.findByRole("dialog", { name: "Pair a stand" });
  }

  it("explains how adding a stand works before starting", async () => {
    pairingApi();
    renderApp("/devices");
    expect(await screen.findByText(/set it up from your phone/)).toBeInTheDocument();
  });

  it("walks through joining the stand's network and gives the address to type", async () => {
    vi.stubGlobal("location", { ...window.location, origin: "http://192.168.1.10:8080", hostname: "192.168.1.10" });
    pairingApi();
    const dialog = await startPairing();
    expect(within(dialog).getByText(/Spindle-/)).toBeInTheDocument();
    expect(within(dialog).getByText("http://192.168.1.10:8080")).toBeInTheDocument();
    expect(within(dialog).getByText("K7F-29Q")).toBeInTheDocument();
    expect(within(dialog).queryByText(/localhost/)).not.toBeInTheDocument();
    expect(within(dialog).getByText(/hold the BOOTSEL button/i)).toBeInTheDocument();
  });

  it("asks for the home network address when opened as localhost", async () => {
    vi.stubGlobal("location", { ...window.location, origin: "http://localhost:8080", hostname: "localhost", port: "8080" });
    pairingApi();
    const dialog = await startPairing();
    expect(within(dialog).getByText(/this computer's address on your home network with port 8080/)).toBeInTheDocument();
    expect(within(dialog).getByText("http://192.168.1.10:8080")).toBeInTheDocument();
    expect(within(dialog).queryByText("http://localhost:8080")).not.toBeInTheDocument();
  });
});

describe("expired session", () => {
  it("sends you to sign in when the server stops accepting the session", async () => {
    let expired = false;
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => (expired ? [401, { error: "not logged in" }] : [200, me]),
      "GET /api/now-playing": () => [200, []],
      "GET /api/devices": () => (expired ? [401, { error: "not logged in" }] : [200, []]),
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });

    expired = true;
    await userEvent.click(screen.getByRole("link", { name: "Stands" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("stops reconnecting the live stream once signed out", async () => {
    let expired = false;
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => (expired ? [401, { error: "not logged in" }] : [200, me]),
      "GET /api/now-playing": () => [200, []],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));

    expired = true;
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const first = FakeEventSource.instances[0];
      act(() => {
        first.readyState = FakeEventSource.CLOSED;
        first.onerror?.();
      });
      expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
      act(() => vi.advanceTimersByTime(10_000));
    } finally {
      vi.useRealTimers();
    }
    expect(FakeEventSource.instances.length).toBe(1);
  });
});

describe("server trouble", () => {
  it("offers to try again when the server can't be reached", async () => {
    let down = true;
    mockApi({
      "GET /api/setup": () => (down ? [502, { error: "bad gateway" }] : [200, { needed: false }]),
      "GET /api/me": () => [200, me],
      "GET /api/now-playing": () => [200, []],
    });
    renderApp();
    expect(await screen.findByText(/Can't reach Spindle/)).toBeInTheDocument();

    down = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
  });

  it("catches up on what is playing after the live stream reconnects", async () => {
    let playing: unknown[] = [];
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "GET /api/now-playing": () => [200, playing],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const stream = FakeEventSource.instances[0];
    act(() => stream.open());

    playing = [
      {
        device_id: 1,
        active: true,
        uid: "04A1",
        album: { id: 1, title: "Rumours", artist: "Fleetwood Mac", styles: [], genres: [], has_cover: false, duration_status: "resolved" },
      },
    ];
    act(() => stream.open());
    expect(await screen.findByRole("heading", { name: "Rumours" })).toBeInTheDocument();
  });
});

describe("unknown sleeve", () => {
  it("shows the album as soon as it is assigned, without waiting for the stream", async () => {
    const rumours = { id: 1, title: "Rumours", artist: "Fleetwood Mac", styles: [], genres: [], has_cover: false, duration_status: "resolved" };
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, []],
      "GET /api/inbox": () => [200, []],
      "GET /api/now-playing": () => [200, [{ device_id: 3, active: true, uid: "04FFFFFF" }]],
      "GET /api/discogs/search?q=rumours": () => [200, [{ kind: "release", id: 99, artist: "Fleetwood Mac", title: "Rumours" }]],
      "POST /api/tags/04FFFFFF/assign": () => [200, { id: 5, uid: "04FFFFFF", album: rumours, created_at: 1 }],
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText("Search albums"), "rumours");
    await userEvent.click(await screen.findByRole("button", { name: /Rumours/ }));
    expect(await screen.findByRole("heading", { name: "Rumours" })).toBeInTheDocument();
  });
});
