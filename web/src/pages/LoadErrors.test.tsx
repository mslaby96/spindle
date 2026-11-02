import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };

afterEach(() => vi.unstubAllGlobals());

const broken = () => [500, { error: "database is locked" }] as [number, unknown];

describe("a page whose data fails to load", () => {
  it.each([
    ["/", "GET /api/now-playing", "what is on the stand"],
    ["/inbox", "GET /api/inbox", "your inbox"],
    ["/library", "GET /api/library?sort=last_played", "your library"],
    ["/library?tab=forgotten", "GET /api/library/forgotten", "the forgotten shelf"],
    ["/diary", "GET /api/diary?limit=50", "your diary"],
    ["/stats", "GET /api/stats/top?period=30d&measure=plays", "your stats"],
    ["/albums/4", "GET /api/albums/4", "this album"],
    ["/devices", "GET /api/devices", "your stands"],
  ])("%s says what failed and can try again", async (path, route, what) => {
    let fail = true;
    const calls = mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/inbox": () => [200, []],
      "GET /api/devices": () => [200, []],
      "GET /api/now-playing": () => [200, []],
      "GET /api/stats/timeline?weeks=5": () => [200, []],
    "GET /api/stats/timeline?weeks=13": () => [200, []],
      [route]: () => (fail ? broken() : [200, []]),
    });
    renderApp(path);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(`Couldn't load ${what}.`);
    expect(alert).toHaveTextContent("database is locked");
    expect(screen.queryByText("Nothing on the stand")).not.toBeInTheDocument();
    fail = false;
    const before = calls.filter((c) => c.key === route).length;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(calls.filter((c) => c.key === route).length).toBeGreaterThan(before);
  });
});
