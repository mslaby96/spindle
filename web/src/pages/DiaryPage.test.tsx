import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const entry = (id: number, title: string, started_at: string) => ({
  session_id: id,
  album_id: id,
  title,
  artist: "Someone",
  has_cover: false,
  started_at,
  plays: 1,
  listened_sec: 2400,
  duration_confirmed: true,
});

afterEach(() => vi.unstubAllGlobals());

describe("diary", () => {
  it("gives each listen its time and each day its date only once", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, me],
      "GET /api/inbox": () => [200, []],
      "GET /api/diary?limit=50": () => [
        200,
        [
          entry(3, "Late One", "2026-10-28T21:30:00"),
          entry(2, "Early One", "2026-10-28T18:05:00"),
          entry(1, "Day Before", "2026-10-27T20:00:00"),
        ],
      ],
    });
    renderApp("/diary");
    const late = (await screen.findByText("Late One")).closest("a")!;
    const early = screen.getByText("Early One").closest("a")!;
    const before = screen.getByText("Day Before").closest("a")!;
    expect(within(late).getByText("28")).toBeInTheDocument();
    expect(within(early).queryByText("28")).not.toBeInTheDocument();
    expect(within(before).getByText("27")).toBeInTheDocument();
    expect(within(late).getByText(/21:30/)).toBeInTheDocument();
    expect(within(early).getByText(/18:05/)).toBeInTheDocument();
  });
});
