import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const album = (id: number, title: string, artist: string, ids: { master?: number; release?: number }) => ({
  id,
  title,
  artist,
  discogs_master_id: ids.master,
  discogs_release_id: ids.release,
  genres: [],
  styles: [],
  has_cover: false,
  duration_status: "resolved",
});
const untagged = [
  album(1, "Blue Train", "John Coltrane", { master: 98 }),
  album(2, "Kind of Blue", "Miles Davis", { master: 5460 }),
  album(3, "Solo Violin", "Zbigniew Seifert", { release: 4455667 }),
];
const card = { uid: "04FF", session: { id: 7, state: "unclassified", started_at: "2026-11-02T20:00:00Z", listened_sec: 2400 } };

afterEach(() => vi.unstubAllGlobals());

function setup(extra: Record<string, (body: unknown) => [number, unknown?]> = {}) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/inbox": () => [200, [card]],
    "GET /api/library/untagged": () => [200, untagged],
    "POST /api/tags/04FF/assign": () => [200, { id: 5, uid: "04FF", album: untagged[0], created_at: 1 }],
    ...extra,
  });
}

describe("assigning a sleeve to a record already in the collection", () => {
  it("offers the records without a sleeve before searching Discogs", async () => {
    setup();
    renderApp("/inbox");
    const list = await screen.findByRole("list", { name: "From your collection" });
    expect(within(list).getAllByRole("button").map((b) => b.textContent)).toEqual([
      expect.stringContaining("Blue Train"),
      expect.stringContaining("Kind of Blue"),
      expect.stringContaining("Solo Violin"),
    ]);
    expect(screen.getByLabelText("Search albums")).toBeInTheDocument();
  });

  it("filters them by artist or title", async () => {
    setup();
    renderApp("/inbox");
    await screen.findByRole("list", { name: "From your collection" });
    await userEvent.type(screen.getByLabelText("Filter your collection"), "miles");
    const list = screen.getByRole("list", { name: "From your collection" });
    expect(within(list).getAllByRole("button")).toHaveLength(1);
    expect(within(list).getByRole("button", { name: /Kind of Blue/ })).toBeInTheDocument();
  });

  it("assigns by the album's master", async () => {
    const calls = setup();
    renderApp("/inbox");
    await userEvent.click(await screen.findByRole("button", { name: /Blue Train/ }));
    await waitFor(() => expect(calls.find((c) => c.key === "POST /api/tags/04FF/assign")?.body).toEqual({ master_id: 98 }));
  });

  it("assigns by the release when the album has no master", async () => {
    const calls = setup();
    renderApp("/inbox");
    await userEvent.click(await screen.findByRole("button", { name: /Solo Violin/ }));
    await waitFor(() => expect(calls.find((c) => c.key === "POST /api/tags/04FF/assign")?.body).toEqual({ release_id: 4455667 }));
  });

  it("goes straight to the Discogs search when every record has a sleeve", async () => {
    setup({ "GET /api/library/untagged": () => [200, []] });
    renderApp("/inbox");
    expect(await screen.findByLabelText("Search albums")).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "From your collection" })).not.toBeInTheDocument();
  });
});

describe("a large collection", () => {
  const many = Array.from({ length: 12 }, (_, i) => album(100 + i, `Record ${String(i + 1).padStart(2, "0")}`, `Artist ${i + 1}`, { master: 900 + i }));

  it("shows a few until asked for all, while the filter searches everything", async () => {
    setup({ "GET /api/library/untagged": () => [200, many] });
    renderApp("/inbox");
    const list = await screen.findByRole("list", { name: "From your collection" });
    expect(within(list).getAllByRole("button")).toHaveLength(8);
    await userEvent.type(screen.getByLabelText("Filter your collection"), "record 12");
    expect(within(screen.getByRole("list", { name: "From your collection" })).getAllByRole("button")).toHaveLength(1);
    await userEvent.clear(screen.getByLabelText("Filter your collection"));
    await userEvent.click(screen.getByRole("button", { name: "Show all 12" }));
    expect(within(screen.getByRole("list", { name: "From your collection" })).getAllByRole("button")).toHaveLength(12);
    expect(screen.queryByRole("button", { name: /Show all/ })).not.toBeInTheDocument();
  });
});
