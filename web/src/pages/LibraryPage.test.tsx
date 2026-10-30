import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const item = (id: number, title: string, artist: string, extra = {}) => ({
  album_id: id,
  title,
  artist,
  has_cover: false,
  plays: 0,
  listened_sec: 0,
  added_at: "2026-01-01T00:00:00Z",
  ...extra,
});

afterEach(() => vi.unstubAllGlobals());

function base(extra: Record<string, (body: unknown) => [number, unknown?]>) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    ...extra,
  });
}

describe("library", () => {
  it("lists, filters and sorts records", async () => {
    const calls = base({
      "GET /api/library?sort=last_played": () => [
        200,
        [
          item(1, "Kind of Blue", "Miles Davis", { plays: 3, last_played_at: new Date().toISOString(), rating: 9 }),
          item(2, "Blue Train", "John Coltrane"),
          item(3, "Rumours", "Fleetwood Mac"),
        ],
      ],
      "GET /api/library?sort=plays": () => [200, [item(1, "Kind of Blue", "Miles Davis", { plays: 3 })]],
    });
    renderApp("/library");
    expect(await screen.findByText("3 records")).toBeInTheDocument();
    expect(screen.getByText(/today · 3×/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "4.5 stars" })).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Filter library"), "blue");
    expect(screen.getByText("Kind of Blue")).toBeInTheDocument();
    expect(screen.getByText("Blue Train")).toBeInTheDocument();
    expect(screen.queryByText("Rumours")).not.toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("Sort by"), "plays");
    await waitFor(() => expect(calls.some((c) => c.key === "GET /api/library?sort=plays")).toBe(true));
  });

  it("counts what the filter leaves and marks records never played", async () => {
    base({
      "GET /api/library?sort=last_played": () => [
        200,
        [item(1, "Kind of Blue", "Miles Davis", { plays: 3, last_played_at: new Date().toISOString() }), item(2, "Blue Train", "John Coltrane"), item(3, "Rumours", "Fleetwood Mac")],
      ],
    });
    renderApp("/library");
    expect(await screen.findByText("3 records")).toBeInTheDocument();
    expect(screen.getAllByText("Unplayed")).toHaveLength(2);

    await userEvent.type(screen.getByLabelText("Filter library"), "blue");
    expect(screen.getByText("2 of 3 records")).toBeInTheDocument();
  });

  it("files records behind a divider per first letter, in the server's order", async () => {
    const keyed = (id: number, title: string, artist: string, artistKey: string) =>
      item(id, title, artist, { artist_key: artistKey, title_key: title.toLowerCase() });
    const sorted = [
      keyed(1, "Abbey Road", "The Beatles", "beatles"),
      keyed(2, "Bitches Brew", "Miles Davis", "miles davis"),
      keyed(3, "1999", "Prince", "prince"),
      keyed(4, "Talk Talk", "Talk Talk", "talk talk"),
      keyed(5, "Rain Dogs", "Tom Waits", "tom waits"),
    ];
    base({
      "GET /api/library?sort=last_played": () => [200, sorted],
      "GET /api/library?sort=artist": () => [200, sorted],
    });
    renderApp("/library");
    await screen.findByText("5 records");
    await userEvent.selectOptions(screen.getByLabelText("Sort by"), "artist");
    await waitFor(() => expect(screen.getByRole("heading", { name: "B" })).toBeInTheDocument());
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual(["B", "M", "P", "T"]);
    const t = screen.getByRole("heading", { name: "T" }).parentElement!;
    expect(within(t).getAllByRole("link")).toHaveLength(2);
  });

  it("shows the forgotten shelf", async () => {
    base({
      "GET /api/library/forgotten": () => [
        200,
        {
          not_recently: [item(1, "Old Favourite", "A", { plays: 5, last_played_at: "2025-01-01T00:00:00Z" })],
          never: [item(2, "Never Played", "B")],
        },
      ],
    });
    renderApp("/library?tab=forgotten");
    const notRecently = await screen.findByRole("heading", { name: "Not played in a long time" });
    expect(notRecently).toBeInTheDocument();
    expect(screen.getByText("Old Favourite")).toBeInTheDocument();
    expect(screen.getByText("Never Played")).toBeInTheDocument();
    expect(screen.getByText(/added 1 Jan 2026/)).toBeInTheDocument();
  });

  it("points to cataloguing when empty", async () => {
    base({ "GET /api/library?sort=last_played": () => [200, []] });
    renderApp("/library");
    expect(await screen.findByRole("link", { name: "Catalogue your records" })).toBeInTheDocument();
  });
});

describe("album page", () => {
  const detail = {
    album: { id: 1, title: "Kind of Blue", artist: "Miles Davis", year: 1959, genres: ["Jazz"], styles: ["Modal"], has_cover: false, duration_sec: 2760, duration_status: "resolved" },
    stats: item(1, "Kind of Blue", "Miles Davis", { plays: 2, listened_sec: 5520 }),
    releases: [{ id: 1, album_id: 1, discogs_release_id: 5, title: "Kind of Blue", country: "US", year: 1959, label: "Columbia", catno: "CL 1355" }],
    history: [
      { session_id: 11, album_id: 1, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, started_at: "2026-03-14T20:00:00Z", plays: 1, listened_sec: 2760, duration_confirmed: true, note: "rainy evening" },
    ],
  };

  it("shows history and deletes a listen", async () => {
    vi.stubGlobal("confirm", () => true);
    const calls = base({
      "GET /api/albums/1": () => [200, detail],
      "DELETE /api/sessions/11": () => [204],
    });
    renderApp("/albums/1");
    expect(await screen.findByRole("heading", { name: "Kind of Blue" })).toBeInTheDocument();
    expect(screen.getByText(/2 listens · 1 h 32 min in total/)).toBeInTheDocument();
    expect(screen.getByText(/US · 1959 · Columbia · CL 1355/)).toBeInTheDocument();
    expect(screen.getByText("rainy evening")).toBeInTheDocument();

    const row = screen.getByText("rainy evening").closest("li")!;
    await userEvent.click(within(row).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(calls.some((c) => c.key === "DELETE /api/sessions/11")).toBe(true));
  });

  it("sets the album length by hand", async () => {
    const calls = base({
      "GET /api/albums/1": () => [200, { ...detail, album: { ...detail.album, duration_sec: undefined, duration_status: "no_data" } }],
      "PUT /api/albums/1/duration": () => [200, detail.album],
    });
    renderApp("/albums/1");
    expect(await screen.findByText(/Album length unknown/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "edit" }));
    await userEvent.type(screen.getByLabelText("Album length in minutes"), "46");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls.find((c) => c.key === "PUT /api/albums/1/duration")?.body).toEqual({ seconds: 2760 }));
  });
});

describe("diary", () => {
  it("groups listens by month", async () => {
    base({
      "GET /api/diary?limit=50": () => [
        200,
        [
          { session_id: 2, album_id: 1, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, started_at: "2026-03-14T20:00:00Z", plays: 1, listened_sec: 2760, duration_confirmed: true, rating: 8, note: "first spin" },
          { session_id: 1, album_id: 2, title: "Rumours", artist: "Fleetwood Mac", has_cover: false, started_at: "2026-02-02T20:00:00Z", plays: 2, listened_sec: 4800, duration_confirmed: true },
        ],
      ],
    });
    renderApp("/diary");
    expect(await screen.findByRole("heading", { name: "March 2026" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "February 2026" })).toBeInTheDocument();
    expect(screen.getByText("first spin")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Older listens" })).not.toBeInTheDocument();
  });
});
