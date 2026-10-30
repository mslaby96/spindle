import { act, screen, waitFor } from "@testing-library/react";
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

function setup(playing: unknown[], diary: unknown[] = []) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/devices": () => [200, []],
    "GET /api/inbox": () => [200, []],
    "GET /api/now-playing": () => [200, playing],
    "GET /api/diary?limit=4": () => [200, diary],
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
