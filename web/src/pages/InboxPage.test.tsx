import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const album = { id: 1, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, length_sec: 2760 };

function session(over: Record<string, unknown>) {
  return {
    id: 7,
    device_id: 1,
    tag_id: 1,
    album_id: 1,
    state: "counted",
    started_at: "2026-03-14T20:00:00Z",
    ended_at: "2026-03-14T20:46:00Z",
    play_count: 1,
    listened_sec: 2760,
    duration_confirmed: true,
    inbox_state: "open",
    ...over,
  };
}

function setup(cards: unknown[], extra: Record<string, (body: unknown) => [number, unknown?]> = {}) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/inbox": () => [200, cards],
    ...extra,
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("inbox", () => {
  it("confirms a long listen", async () => {
    const calls = setup(
      [{ uid: "04A1", album, session: session({ state: "pending_confirmation", listened_sec: 10800, duration_confirmed: false }) }],
      { "POST /api/sessions/7/resolve": () => [204] },
    );
    renderApp("/inbox");
    expect(await screen.findByText(/stayed on the stand for 3 h 0 min, the album is 46 min/)).toBeInTheDocument();
    expect(screen.getByText(/\(unconfirmed\)/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Skip" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "I played it several times" }));
    await waitFor(() =>
      expect(calls.find((c) => c.key === "POST /api/sessions/7/resolve")?.body).toEqual({ resolution: "repeated" }),
    );
  });

  it("rates a listen in half stars with a note", async () => {
    const calls = setup([{ uid: "04A1", album, session: session({}) }], { "PUT /api/sessions/7/review": () => [204] });
    renderApp("/inbox");
    expect(await screen.findByText("Rate this album")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await userEvent.click(screen.getByRole("radio", { name: "4.5 stars" }));
    await userEvent.type(screen.getByLabelText("Note"), "late night, headphones");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.key === "PUT /api/sessions/7/review")?.body).toEqual({
        rating: 9,
        note: "late night, headphones",
      }),
    );
  });

  it("does not highlight rating for an album that already has one", async () => {
    setup([{ uid: "04A1", album: { ...album, rating: 8 }, session: session({}) }]);
    renderApp("/inbox");
    expect(await screen.findByText(/your rating ★★★★/)).toBeInTheDocument();
    expect(screen.queryByText("Rate this album")).not.toBeInTheDocument();
  });

  it("skips a card", async () => {
    const calls = setup([{ uid: "04A1", album, session: session({}) }], { "POST /api/sessions/7/dismiss": () => [204] });
    renderApp("/inbox");
    await userEvent.click(await screen.findByRole("button", { name: "Skip" }));
    await waitFor(() => expect(calls.some((c) => c.key === "POST /api/sessions/7/dismiss")).toBe(true));
  });

  it("asks which album an unknown sleeve is", async () => {
    setup([{ uid: "04FFFFFF", session: session({ album_id: undefined, state: "unclassified" }) }]);
    renderApp("/inbox");
    expect(await screen.findByText("Unknown sleeve")).toBeInTheDocument();
    expect(screen.getByLabelText("Search albums")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Not a real listen, delete it" })).toBeInTheDocument();
  });

  it("shows how many cards are waiting in the navigation", async () => {
    setup([
      { uid: "04A1", album, session: session({}) },
      { uid: "04A1", album, session: session({ id: 8 }) },
    ]);
    renderApp("/inbox");
    expect(await screen.findByRole("link", { name: "Inbox2" })).toBeInTheDocument();
  });
});
