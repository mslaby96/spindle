import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const stats = { album_id: 4, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, plays: 0, listened_sec: 0, added_at: "2026-01-01T00:00:00Z", artist_key: "miles davis", title_key: "kind of blue" };
const albumDetail = (ids: Record<string, number>) => ({
  sleeves: [],
  album: { id: 4, title: "Kind of Blue", artist: "Miles Davis", genres: [], styles: [], has_cover: false, duration_status: "resolved", ...ids },
  stats,
  releases: [],
  history: [],
});

afterEach(() => vi.unstubAllGlobals());

function base(extra: Record<string, (body: unknown) => [number, unknown?]>) {
  return mockApi({ "GET /api/setup": () => [200, { needed: false }], "GET /api/me": () => [200, me], "GET /api/inbox": () => [200, []], ...extra });
}

function expectDiscogsLink(link: HTMLElement, href: string) {
  expect(link).toHaveAttribute("href", href);
  expect(link.getAttribute("rel") ?? "").not.toMatch(/nofollow/);
}

describe("Discogs attribution", () => {
  it("links an album to its Discogs master", async () => {
    base({ "GET /api/albums/4": () => [200, albumDetail({ discogs_master_id: 5460 })] });
    renderApp("/albums/4");
    expectDiscogsLink(await screen.findByRole("link", { name: "Data provided by Discogs" }), "https://www.discogs.com/master/5460");
  });

  it("links an album without a master to its release", async () => {
    base({ "GET /api/albums/4": () => [200, albumDetail({ discogs_release_id: 4455667 })] });
    renderApp("/albums/4");
    expectDiscogsLink(await screen.findByRole("link", { name: "Data provided by Discogs" }), "https://www.discogs.com/release/4455667");
  });

  it("credits every search result and pressing next to it", async () => {
    base({
      "GET /api/inbox": () => [200, [{ uid: "04FF", session: { id: 7, state: "unclassified", started_at: "2026-11-02T20:00:00Z", listened_sec: 2400 } }]],
      "GET /api/library/untagged": () => [200, []],
      "GET /api/discogs/search?q=kind": () => [
        200,
        [
          { kind: "master", id: 5460, artist: "Miles Davis", title: "Kind of Blue", year: "1959" },
          { kind: "release", id: 777, artist: "Miles Davis", title: "Kind of Blue", year: "1959" },
        ],
      ],
      "GET /api/discogs/masters/5460/versions?": () => [200, [{ id: 1873013, title: "Kind of Blue", label: "Columbia", catno: "CL 1355", country: "US", released: "1959", format: "Vinyl", thumb: "" }]],
    });
    renderApp("/inbox");
    await userEvent.type(await screen.findByLabelText("Search albums"), "kind");
    const results = await screen.findAllByRole("link", { name: "Data provided by Discogs" });
    expect(results.map((l) => l.getAttribute("href"))).toEqual(["https://www.discogs.com/master/5460", "https://www.discogs.com/release/777"]);

    await userEvent.click(screen.getAllByRole("button", { name: /Kind of Blue/ })[0]);
    const pressing = (await screen.findByRole("button", { name: /US 1959/ })).closest("li")!;
    expectDiscogsLink(within(pressing).getByRole("link", { name: "Data provided by Discogs" }), "https://www.discogs.com/release/1873013");
  });
});
