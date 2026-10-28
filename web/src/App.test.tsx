import { act, screen, waitFor } from "@testing-library/react";
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
  it("shows a new stand token only once", async () => {
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
    await userEvent.click(screen.getByRole("button", { name: "Add stand" }));

    expect(await screen.findByText("spd_secret_token")).toBeInTheDocument();
    expect(await screen.findByText("Living room")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("spd_secret_token")).not.toBeInTheDocument();
  });
});
