import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };

function detail(album: Record<string, unknown>) {
  return {
    album: { id: 4, title: "Kind of Blue", artist: "Miles Davis", genres: [], styles: [], has_cover: false, ...album },
    stats: { album_id: 4, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, plays: 0, listened_sec: 0, added_at: "2026-01-01T00:00:00Z" },
    releases: [],
    history: [],
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("album length", () => {
  it("starts editing from the length the album has now", async () => {
    let album: Record<string, unknown> = { duration_status: "pending" };
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail(album)],
      "PUT /api/albums/4/duration": () => {
        album = { duration_status: "resolved", duration_sec: 2760 };
        return [204];
      },
    });
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "edit" }));
    await userEvent.click(screen.getByRole("button", { name: "Look it up again" }));
    expect(await screen.findByText(/Album length 46 min/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "edit" }));
    expect(screen.getByLabelText("Album length in minutes")).toHaveValue(46);
    expect(calls.filter((c) => c.key === "PUT /api/albums/4/duration")).toHaveLength(1);
  });

  it("does not send an empty length", async () => {
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "no_data" })],
    });
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "edit" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByLabelText("Album length in minutes")).toBeInTheDocument());
    expect(calls.some((c) => c.key === "PUT /api/albums/4/duration")).toBe(false);
  });
});
