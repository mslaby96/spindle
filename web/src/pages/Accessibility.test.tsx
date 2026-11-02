import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const album = { id: 2, title: "Damn", artist: "Kendrick Lamar", year: 2017, genres: [], styles: [], has_cover: false, duration_status: "resolved" };

afterEach(() => vi.unstubAllGlobals());

function base(extra: Record<string, () => [number, unknown?]> = {}) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/inbox": () => [200, []],
    "GET /api/devices": () => [200, []],
    "GET /api/now-playing": () => [200, []],
    "GET /api/stats/timeline?weeks=5": () => [200, []],
    "GET /api/stats/timeline?weeks=13": () => [200, []],
    ...extra,
  });
}

describe("moving around with the keyboard", () => {
  it("offers a way past the navigation", async () => {
    base();
    renderApp();
    await screen.findByRole("heading", { name: "Nothing on the stand" });
    await userEvent.tab();
    const skip = screen.getByRole("link", { name: "Skip to content" });
    expect(skip).toHaveFocus();
    expect(skip).toHaveAttribute("href", "#main");
    expect(document.getElementById("main")).toHaveAttribute("tabindex", "-1");
  });

  it("reads the inbox count as words", async () => {
    base({ "GET /api/inbox": () => [200, [{}, {}]] });
    renderApp("/stats");
    expect(await screen.findByRole("link", { name: "Inbox, 2 waiting" })).toBeInTheDocument();
  });

  it("switches the library's tabs with the arrow keys", async () => {
    base({
      "GET /api/library?sort=last_played": () => [200, []],
      "GET /api/library/forgotten": () => [200, { not_recently: [], never: [] }],
    });
    renderApp("/library");
    const all = await screen.findByRole("tab", { name: "All records" });
    expect(all).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel", { name: "All records" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Forgotten shelf" })).toHaveAttribute("tabindex", "-1");
    all.focus();
    await userEvent.keyboard("{ArrowRight}");
    const forgotten = screen.getByRole("tab", { name: "Forgotten shelf" });
    expect(forgotten).toHaveFocus();
    expect(forgotten).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByRole("tabpanel", { name: "Forgotten shelf" })).toBeInTheDocument();
  });

  it("treats the stats period and measure as pressed buttons", async () => {
    base({
      "GET /api/stats/top?period=30d&measure=plays": () => [200, { summary: { plays: 0, listened_sec: 0, unconfirmed_sec: 0, albums: 0 }, albums: [], genres: [], styles: [] }],
      "GET /api/stats/top?period=90d&measure=plays": () => [200, { summary: { plays: 0, listened_sec: 0, unconfirmed_sec: 0, albums: 0 }, albums: [], genres: [], styles: [] }],
    });
    renderApp("/stats");
    const period = await screen.findByRole("group", { name: "Period" });
    expect(within(period).getByRole("button", { name: "30 days", pressed: true })).toBeInTheDocument();
    await userEvent.click(within(period).getByRole("button", { name: "90 days" }));
    expect(within(period).getByRole("button", { name: "90 days", pressed: true })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Measure" })).toBeInTheDocument();
  });
});

describe("a sleeve taken off the stand", () => {
  it("is announced once, not every second", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    base({
      "GET /api/now-playing": () => [
        200,
        [{ device_id: 1, active: true, uid: "04A1", album, started_at: new Date().toISOString(), ends_at: new Date(Date.now() + 80_000).toISOString() }],
      ],
    });
    renderApp();
    await screen.findByRole("heading", { name: "Damn" });
    const live = document.querySelectorAll("[aria-live]");
    expect(live).toHaveLength(1);
    const said = live[0].textContent;
    expect(said).toMatch(/Sleeve off the stand/);
    vi.advanceTimersByTime(3000);
    expect(document.querySelector("[aria-live]")!.textContent).toBe(said);
    expect(document.querySelector("[aria-live] .countdown")).toBeNull();
    vi.useRealTimers();
  });
});

describe("the album search", () => {
  it("doesn't grab focus in the inbox, where it would pop the keyboard over the cards", async () => {
    base({ "GET /api/inbox": () => [200, [{ uid: "04FF", session: { id: 7, state: "unclassified", started_at: new Date().toISOString(), listened_sec: 2400 } }]] });
    renderApp("/inbox");
    const search = await screen.findByLabelText("Search albums");
    expect(search).not.toHaveFocus();
  });
});
