import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const album = {
  id: 2,
  title: "Damn",
  artist: "Kendrick Lamar",
  year: 2017,
  genres: ["Hip Hop"],
  styles: ["Conscious", "Trap", "Boom Bap"],
  has_cover: false,
  cover_color: "#693845",
  duration_sec: 54 * 60,
  duration_status: "resolved",
};
const recent = (id: number, title: string) => ({
  session_id: id,
  album_id: id,
  title,
  artist: "Someone",
  has_cover: false,
  started_at: new Date(Date.now() - 86_400_000).toISOString(),
  plays: 1,
  listened_sec: 2400,
  duration_confirmed: true,
});

afterEach(() => vi.unstubAllGlobals());

function setup(playing: unknown[] | (() => unknown[]), diary: unknown[] = [], extra: Record<string, () => [number, unknown?]> = {}) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/devices": () => [200, []],
    "GET /api/inbox": () => [200, []],
    "GET /api/now-playing": () => [200, typeof playing === "function" ? playing() : playing],
    "GET /api/diary?limit=4": () => [200, diary],
    ...extra,
  });
}

describe("now playing", () => {
  it("shows how long the record has been playing against its length", async () => {
    const started = new Date(Date.now() - (23 * 60 + 42) * 1000).toISOString();
    setup([{ device_id: 1, active: true, uid: "04A1", album, started_at: started }]);
    renderApp();
    expect(await screen.findByRole("heading", { name: "Damn" })).toBeInTheDocument();
    expect(screen.getByText(/^23:4[23]$/)).toBeInTheDocument();
    expect(screen.getByText("of 54 min")).toBeInTheDocument();
    expect(screen.getByText("2017 · Conscious · Trap")).toBeInTheDocument();
    expect(document.querySelector(".progress")).not.toBeNull();
  });

  it("leaves out the length and progress when the album length is unknown", async () => {
    const started = new Date(Date.now() - 60_000).toISOString();
    setup([{ device_id: 1, active: true, uid: "04A1", album: { ...album, duration_sec: 0 }, started_at: started }]);
    renderApp();
    expect(await screen.findByText("playing")).toBeInTheDocument();
    expect(document.querySelector(".progress")).toBeNull();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });

  it("shows the last records on the stand, even with nothing playing", async () => {
    setup([], [recent(1, "Like A Prayer"), recent(2, "Blue Train")]);
    renderApp();
    expect(await screen.findByRole("heading", { name: "Last on the stand" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Like A Prayer/ })).toHaveAttribute("href", "/albums/1");
    expect(screen.getByRole("link", { name: /Blue Train/ })).toBeInTheDocument();
  });

  it("refreshes the last records when a listen ends", async () => {
    const calls = setup([], [recent(1, "Like A Prayer")]);
    renderApp();
    await screen.findByRole("heading", { name: "Last on the stand" });
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const before = calls.filter((c) => c.key === "GET /api/diary?limit=4").length;
    act(() => FakeEventSource.instances[0].emit("inbox_changed", {}));
    await waitFor(() => expect(calls.filter((c) => c.key === "GET /api/diary?limit=4").length).toBe(before + 1));
  });
});

describe("ending a listen early", () => {
  const away = { device_id: 3, active: true, uid: "04A1", album, started_at: new Date(Date.now() - 600_000).toISOString(), ends_at: new Date(Date.now() + 80_000).toISOString() };

  it("ends it at once while the sleeve is off the stand", async () => {
    let playing: unknown[] = [away];
    const calls = setup(() => playing, [], { "POST /api/now-playing/3/end": () => ((playing = []), [204]) });
    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: "End listen now" }));
    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /api/now-playing/3/end")).toBe(true);
  });

  it("isn't offered while the sleeve is on the stand", async () => {
    setup([{ ...away, ends_at: undefined }]);
    renderApp();
    await screen.findByRole("heading", { name: "Damn" });
    expect(screen.queryByRole("button", { name: "End listen now" })).not.toBeInTheDocument();
  });
});

describe("stand not answering", () => {
  const stands = (...online: boolean[]) => ({
    "GET /api/devices": () => [200, online.map((o, i) => ({ id: i + 1, name: `Stand ${i + 1}`, created_at: "2026-01-01T00:00:00Z", online: o }))] as [number, unknown],
  });

  it("says so when every stand is offline", async () => {
    setup([], [], stands(false));
    renderApp();
    expect(await screen.findByRole("heading", { name: "Your stand isn't answering" })).toBeInTheDocument();
    expect(screen.getByText(/plugged in/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See your stands" })).toHaveAttribute("href", "/devices");
  });

  it("speaks of stands when there are several and all are offline", async () => {
    setup([], [], stands(false, false));
    renderApp();
    expect(await screen.findByRole("heading", { name: "Your stands aren't answering" })).toBeInTheDocument();
  });

  it("keeps the usual empty state while any stand is online", async () => {
    setup([], [], stands(false, true));
    renderApp();
    expect(await screen.findByRole("heading", { name: "Nothing on the stand" })).toBeInTheDocument();
    expect(screen.queryByText(/answering/)).not.toBeInTheDocument();
  });
});
