import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const top = (value: number) => ({
  summary: { plays: 12, listened_sec: 30600, unconfirmed_sec: 3600, albums: 5 },
  albums: [{ album_id: 1, title: "Kind of Blue", artist: "Miles Davis", has_cover: false, value }],
  genres: [
    { name: "Jazz", value },
    { name: "Rock", value: Math.round(value / 2) },
  ],
  styles: [{ name: "Modal", value }],
});
const weeks = [
  { start: "2026-09-21", plays: 3, listened_sec: 7200 },
  { start: "2026-09-28", plays: 0, listened_sec: 0 },
];

afterEach(() => vi.unstubAllGlobals());

function setup(empty = false) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/stats/top?period=30d&measure=plays": () => [200, empty ? { ...top(0), albums: [], genres: [], styles: [] } : top(6)],
    "GET /api/stats/top?period=30d&measure=time": () => [200, top(9000)],
    "GET /api/stats/top?period=year&measure=plays": () => [200, top(40)],
    "GET /api/stats/timeline?weeks=26": () => [200, weeks],
  });
}

describe("stats", () => {
  it("shows the summary and top lists", async () => {
    setup();
    renderApp("/stats");
    expect(await screen.findByText("12")).toBeInTheDocument();
    expect(screen.getByText("8 h 30 min")).toBeInTheDocument();
    expect(screen.getByText("incl. 1 h 0 min unconfirmed")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Top albums" })).toBeInTheDocument();
    expect(screen.getAllByText("6×").length).toBeGreaterThan(0);
    expect(screen.getByText("Jazz")).toBeInTheDocument();
  });

  it("switches measure and period", async () => {
    const calls = setup();
    renderApp("/stats");
    await screen.findByText("Jazz");
    await userEvent.click(screen.getByRole("tab", { name: "Time" }));
    expect((await screen.findAllByText("2 h 30 min")).length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("tab", { name: "Year" }));
    await userEvent.click(screen.getByRole("tab", { name: "Listens" }));
    await waitFor(() => expect(calls.some((c) => c.key === "GET /api/stats/top?period=year&measure=plays")).toBe(true));
  });

  it("offers the weekly chart as a table", async () => {
    setup();
    renderApp("/stats");
    await userEvent.click(await screen.findByRole("button", { name: "Show as table" }));
    expect(screen.getByRole("columnheader", { name: "Week of" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "21 Sept" })).toBeInTheDocument();
  });

  it("says when there is nothing to show", async () => {
    setup(true);
    renderApp("/stats");
    expect(await screen.findByText("No listens in this period.")).toBeInTheDocument();
  });
});
