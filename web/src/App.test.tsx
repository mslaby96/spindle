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
  it("shows the tag reported by a stand", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/devices": () => [200, [{ id: 3, name: "Living room", created_at: "2026-01-01T00:00:00Z" }]],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));

    act(() => FakeEventSource.instances.at(-1)!.emit("tag_seen", { device_id: 3, uid: "04A1B2C3D4E5F6" }));
    expect(await screen.findByText("04A1B2C3D4E5F6")).toBeInTheDocument();
    expect(screen.getByText("Living room")).toBeInTheDocument();

    act(() => FakeEventSource.instances.at(-1)!.emit("tag_seen", { device_id: 3, uid: "" }));
    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
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
