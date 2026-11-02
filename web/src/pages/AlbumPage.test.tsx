import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };

function detail(album: Record<string, unknown>, extra: Record<string, unknown> = {}) {
  return {
    sleeves: [],
    album: { id: 4, title: "Kind of Blue", artist: "Miles Davis", genres: [], styles: [], has_cover: false, ...album },
    stats: { album_id: 4, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, plays: 0, listened_sec: 0, added_at: "2026-01-01T00:00:00Z" },
    releases: [],
    history: [],
    ...extra,
  };
}

function withPlays(plays: number) {
  return { stats: { album_id: 4, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, plays, listened_sec: plays * 2760, added_at: "2026-01-01T00:00:00Z" } };
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

describe("leaving the collection", () => {
  it("removes a sold record but keeps its listens, and can put it back", async () => {
    let removed = false;
    const asked: string[] = [];
    vi.stubGlobal("confirm", (q: string) => (asked.push(q), true));
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "resolved", removed_at: removed ? "2026-10-01T12:00:00Z" : undefined }, withPlays(3))],
      "POST /api/albums/4/remove": () => ((removed = true), [204]),
      "POST /api/albums/4/restore": () => ((removed = false), [204]),
    });
    renderApp("/albums/4");

    await userEvent.click(await screen.findByRole("button", { name: "Remove from collection" }));
    expect(asked[0]).toMatch(/sold it or lost it\? Its 3 listens stay/);
    expect(await screen.findByText(/No longer in your collection/)).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /api/albums/4/remove")).toBe(true);

    await userEvent.click(screen.getByRole("button", { name: "Put back in collection" }));
    expect(await screen.findByRole("button", { name: "Remove from collection" })).toBeInTheDocument();
  });

  it("deletes an album added by mistake and goes to the library", async () => {
    const asked: string[] = [];
    vi.stubGlobal("confirm", (q: string) => (asked.push(q), true));
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "resolved" }, withPlays(2))],
      "DELETE /api/albums/4": () => [204],
      "GET /api/library?sort=last_played": () => [200, []],
    });
    renderApp("/albums/4");

    await userEvent.click(await screen.findByRole("button", { name: "Delete album" }));
    expect(asked[0]).toMatch(/Delete Kind of Blue and its 2 listens\?/);
    expect(await screen.findByRole("heading", { name: "Library" })).toBeInTheDocument();
    expect(calls.some((c) => c.key === "DELETE /api/albums/4")).toBe(true);
  });

  it("does nothing when you change your mind", async () => {
    vi.stubGlobal("confirm", () => false);
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "resolved" })],
    });
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Delete album" }));
    await userEvent.click(screen.getByRole("button", { name: "Remove from collection" }));
    expect(calls.filter((c) => !c.key.startsWith("GET "))).toEqual([]);
  });
});

describe("wrong album on a sleeve", () => {
  function api(assign: (body: unknown) => [number, unknown?]) {
    return mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "resolved" }, { sleeves: ["04A1B2C3"] })],
      "GET /api/albums/9": () => [200, detail({ id: 9, title: "Sketches of Spain", duration_status: "resolved" })],
      "GET /api/discogs/search?q=sketches": () => [200, [{ kind: "release", id: 77, artist: "Miles Davis", title: "Sketches of Spain" }]],
      "POST /api/tags/04A1B2C3/assign": assign,
    });
  }

  async function pickOtherAlbum() {
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Wrong album?" }));
    await userEvent.type(screen.getByLabelText("Search albums"), "sketches");
    await userEvent.click(await screen.findByRole("button", { name: /Sketches of Spain/ }));
  }

  const moved = { id: 5, uid: "04A1B2C3", album: { id: 9, title: "Sketches of Spain", artist: "Miles Davis", genres: [], styles: [], has_cover: false, duration_status: "resolved" }, created_at: 1 };

  it("asks whether the listens move when the sleeve has some", async () => {
    const calls = api((body) =>
      (body as { listens?: string }).listens ? [200, moved] : [409, { error: "has listens", listens: 3 }],
    );
    await pickOtherAlbum();

    expect(await screen.findByText(/This sleeve has 3 listens of Kind of Blue/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Move them to the new album" }));

    expect(await screen.findByRole("heading", { name: "Sketches of Spain" })).toBeInTheDocument();
    expect(calls.filter((c) => c.key === "POST /api/tags/04A1B2C3/assign").map((c) => c.body)).toEqual([
      { release_id: 77 },
      { release_id: 77, listens: "move" },
    ]);
  });

  it("just changes it when there are no listens", async () => {
    const calls = api(() => [200, moved]);
    await pickOtherAlbum();
    expect(await screen.findByRole("heading", { name: "Sketches of Spain" })).toBeInTheDocument();
    expect(screen.queryByText(/listens of/)).not.toBeInTheDocument();
    expect(calls.filter((c) => c.key === "POST /api/tags/04A1B2C3/assign")).toHaveLength(1);
  });
});

describe("rating any listen", () => {
  const listen = (over: Record<string, unknown> = {}) => ({
    session_id: 11,
    album_id: 4,
    title: "Kind of Blue",
    artist: "Miles Davis",
    has_cover: false,
    started_at: "2026-03-14T20:00:00Z",
    plays: 1,
    listened_sec: 2760,
    duration_confirmed: true,
    ...over,
  });

  function setup(history: unknown[], review: (body: unknown) => [number, unknown?] = () => [204]) {
    return mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/albums/4": () => [200, detail({ duration_status: "resolved" }, { history })],
      "PUT /api/sessions/11/review": review,
    });
  }

  it("rates a listen that left the inbox without a rating", async () => {
    const calls = setup([listen()]);
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Rate" }));
    await userEvent.click(screen.getByRole("radio", { name: "4 stars" }));
    await userEvent.type(screen.getByLabelText("Note"), "side B again");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.key === "PUT /api/sessions/11/review")?.body).toEqual({ rating: 8, note: "side B again" }),
    );
    await waitFor(() => expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument());
  });

  it("starts from what the listen has and keeps the rating when only the note changes", async () => {
    const calls = setup([listen({ rating: 9, note: "rainy evening" })]);
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Change rating or note" }));
    expect(screen.getByRole("radio", { name: "4.5 stars" })).toBeChecked();
    const note = screen.getByLabelText("Note");
    expect(note).toHaveValue("rainy evening");
    await userEvent.clear(note);
    await userEvent.type(note, "rainy night");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.key === "PUT /api/sessions/11/review")?.body).toEqual({ rating: 9, note: "rainy night" }),
    );
  });

  it("can be put away without saving", async () => {
    const calls = setup([listen()]);
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Rate" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    expect(calls.some((c) => c.key === "PUT /api/sessions/11/review")).toBe(false);
  });

  it("shows why the rating couldn't be saved", async () => {
    setup([listen()], () => [500, { error: "database is locked" }]);
    renderApp("/albums/4");
    await userEvent.click(await screen.findByRole("button", { name: "Rate" }));
    await userEvent.click(screen.getByRole("radio", { name: "3 stars" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("database is locked")).toBeInTheDocument();
    expect(screen.getByRole("radiogroup")).toBeInTheDocument();
  });
});
