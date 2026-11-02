import { act, screen, waitFor } from "@testing-library/react";
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

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function withClock() {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
}

const undoWindow = () => act(() => vi.advanceTimersByTime(5000));

describe("inbox", () => {
  it("confirms a long listen, with a moment to take it back", async () => {
    const user = withClock();
    let cards = [{ uid: "04A1", album, session: session({ state: "pending_confirmation", listened_sec: 10800, duration_confirmed: false }) }];
    const calls = setup(cards, {
      "GET /api/inbox": () => [200, cards],
      "POST /api/sessions/7/resolve": () => {
        cards = [];
        return [204];
      },
    });
    renderApp("/inbox");
    expect(await screen.findByText(/stayed on the stand for 3 h 0 min, the album is 46 min/)).toBeInTheDocument();
    expect(screen.getByText("Unconfirmed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Skip" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "I played it several times" }));
    expect(screen.queryByText(/stayed on the stand/)).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Kind of Blue: played several times");
    expect(screen.queryByRole("button", { name: "Skip" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Undo" }));
    expect(screen.getByText(/stayed on the stand/)).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /api/sessions/7/resolve")).toBe(false);

    await user.click(screen.getByRole("button", { name: "I played it several times" }));
    undoWindow();
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
    expect(await screen.findByRole("img", { name: "4 stars" })).toBeInTheDocument();
    expect(screen.queryByText("Rate this album")).not.toBeInTheDocument();
  });

  it("skips a card, with a moment to undo", async () => {
    const user = withClock();
    let cards = [{ uid: "04A1", album, session: session({}) }];
    const calls = setup(cards, {
      "GET /api/inbox": () => [200, cards],
      "POST /api/sessions/7/dismiss": () => {
        cards = [];
        return [204];
      },
    });
    renderApp("/inbox");
    await user.click(await screen.findByRole("button", { name: "Skip" }));
    expect(screen.queryByText("Kind of Blue")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Skipped Kind of Blue");
    await user.click(screen.getByRole("button", { name: "Undo" }));
    expect(screen.getByText("Kind of Blue")).toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /api/sessions/7/dismiss")).toBe(false);

    await user.click(screen.getByRole("button", { name: "Skip" }));
    undoWindow();
    await waitFor(() => expect(calls.some((c) => c.key === "POST /api/sessions/7/dismiss")).toBe(true));
    expect(screen.queryByRole("button", { name: "Undo" })).not.toBeInTheDocument();
  });

  it("still skips when you leave the page before the undo runs out", async () => {
    const user = withClock();
    const calls = setup([{ uid: "04A1", album, session: session({}) }], {
      "POST /api/sessions/7/dismiss": () => [204],
      "GET /api/library?sort=last_played": () => [200, []],
    });
    renderApp("/inbox");
    await user.click(await screen.findByRole("button", { name: "Skip" }));
    await user.click(screen.getByRole("link", { name: "Library" }));
    await waitFor(() => expect(calls.some((c) => c.key === "POST /api/sessions/7/dismiss")).toBe(true));
  });

  it("brings the card back and says so when skipping fails", async () => {
    const user = withClock();
    setup([{ uid: "04A1", album, session: session({}) }], {
      "POST /api/sessions/7/dismiss": () => [500, { error: "database is locked" }],
    });
    renderApp("/inbox");
    await user.click(await screen.findByRole("button", { name: "Skip" }));
    undoWindow();
    expect(await screen.findByText(/database is locked/)).toBeInTheDocument();
    expect(screen.getByText("Kind of Blue")).toBeInTheDocument();
  });

  it("says a saved listen went to the diary", async () => {
    let cards = [{ uid: "04A1", album, session: session({}) }];
    setup(cards, {
      "GET /api/inbox": () => [200, cards],
      "PUT /api/sessions/7/review": () => {
        cards = [];
        return [204];
      },
    });
    renderApp("/inbox");
    await userEvent.click(await screen.findByRole("radio", { name: "4 stars" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Saved Kind of Blue to your diary");
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
