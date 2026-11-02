import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const summary = { found: 312, new_albums: 296, new_pressings: 13, already_in_library: 3, skipped_removed: ["Rumours", "Thriller"] };

afterEach(() => vi.unstubAllGlobals());

function setup(extra: Record<string, (body: unknown) => [number, unknown?]> = {}) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/import/discogs": () => [200, { state: "idle", done: 0, total: 0 }],
    ...extra,
  });
}

async function stream() {
  await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
  return FakeEventSource.instances.at(-1)!;
}

describe("importing a Discogs collection", () => {
  it("shows what would come in, then imports and follows the progress", async () => {
    const calls = setup({
      "POST /api/import/discogs/preview": () => [200, summary],
      "POST /api/import/discogs": () => [202, { state: "running", done: 0, total: 296 }],
    });
    renderApp("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Check my Discogs collection" }));
    expect(await screen.findByText(/312 records on Discogs/)).toBeInTheDocument();
    expect(screen.getByText(/296 new albums/)).toBeInTheDocument();
    expect(screen.getByText(/3 already here/)).toBeInTheDocument();
    expect(screen.getByText(/Rumours, Thriller/)).toBeInTheDocument();
    expect(screen.getByText(/makes a backup first/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Start import" }));
    expect(calls.some((c) => c.key === "POST /api/import/discogs")).toBe(true);
    expect(await screen.findByText(/0 of 296 albums/)).toBeInTheDocument();

    const s = await stream();
    act(() => s.emit("discogs_import", { state: "running", done: 120, total: 296 }));
    expect(await screen.findByText(/120 of 296 albums/)).toBeInTheDocument();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "120");

    act(() => s.emit("discogs_import", { state: "done", done: 296, total: 296, summary }));
    expect(await screen.findByText(/Added 296 albums/)).toBeInTheDocument();
    expect(screen.getByText(/covers and album lengths/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check my Discogs collection" })).toBeInTheDocument();
  });

  it("picks up an import that is already running", async () => {
    setup({ "GET /api/import/discogs": () => [200, { state: "running", done: 40, total: 296 }] });
    renderApp("/settings");
    expect(await screen.findByText(/40 of 296 albums/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Check my Discogs collection" })).not.toBeInTheDocument();
  });

  it("says when there is nothing new to import", async () => {
    setup({ "POST /api/import/discogs/preview": () => [200, { ...summary, new_albums: 0, new_pressings: 0, skipped_removed: [] }] });
    renderApp("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Check my Discogs collection" }));
    expect(await screen.findByText(/Nothing new to import/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start import" })).not.toBeInTheDocument();
  });

  it("explains a missing token", async () => {
    setup({ "POST /api/import/discogs/preview": () => [503, { error: "Discogs token not configured" }] });
    renderApp("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Check my Discogs collection" }));
    expect(await screen.findByText("Discogs token not configured")).toBeInTheDocument();
  });

  it("says why an import failed", async () => {
    setup({ "GET /api/import/discogs": () => [200, { state: "failed", done: 12, total: 296, error: "Discogs is rate limiting" }] });
    renderApp("/settings");
    expect(await screen.findByText(/Discogs is rate limiting/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check my Discogs collection" })).toBeInTheDocument();
  });

  it("carries the notice Discogs asks for", async () => {
    setup();
    renderApp("/settings");
    expect(
      await screen.findByText(
        "This application uses Discogs' API but is not affiliated with, sponsored or endorsed by Discogs. 'Discogs' is a trademark of Zink Media, LLC.",
      ),
    ).toBeInTheDocument();
  });
});
