import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const dsotm = {
  id: 1,
  discogs_master_id: 10362,
  title: "The Dark Side Of The Moon",
  artist: "Pink Floyd",
  genres: ["Rock"],
  styles: ["Prog Rock"],
  has_cover: true,
  duration_status: "pending",
};

afterEach(() => vi.unstubAllGlobals());

function base(extra: Record<string, (body: unknown) => [number, unknown?]>) {
  return mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/catalog/mode": () => [200, { active: true }],
    "GET /api/tags/unassigned": () => [200, []],
    ...extra,
  });
}

async function placeSleeve(data: unknown) {
  await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
  act(() => FakeEventSource.instances.at(-1)!.emit("catalog_tag", data));
}

describe("catalogue", () => {
  it("assigns a new sleeve to an album, skipping the pressing", async () => {
    const calls = base({
      "GET /api/discogs/search?q=dark%20side": () => [
        200,
        [{ kind: "master", id: 10362, artist: "Pink Floyd", title: "The Dark Side Of The Moon", year: "1973" }],
      ],
      "GET /api/discogs/masters/10362/versions?": () => [200, []],
      "POST /api/tags/04A1B2C3/assign": () => [200, { id: 5, uid: "04A1B2C3", album: dsotm, created_at: 1 }],
    });
    renderApp("/catalog");
    expect(await screen.findByText("Put the next sleeve on the stand.")).toBeInTheDocument();

    await placeSleeve({ device_id: 1, new: true, tag: { id: 5, uid: "04A1B2C3", created_at: 1 } });
    await userEvent.type(await screen.findByLabelText("Search albums"), "dark side");
    await userEvent.click(await screen.findByRole("button", { name: /The Dark Side Of The Moon/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Skip, just save the album" }));

    expect(await screen.findByText(/Saved/)).toBeInTheDocument();
    expect(calls.find((c) => c.key === "POST /api/tags/04A1B2C3/assign")?.body).toEqual({ master_id: 10362 });
  });

  it("saves a standalone pressing straight from search", async () => {
    const calls = base({
      "GET /api/discogs/search?q=seifert": () => [
        200,
        [{ kind: "release", id: 4455667, artist: "Zbigniew Seifert", title: "Solo Violin", country: "Poland" }],
      ],
      "POST /api/tags/04A1B2C3/assign": () => [
        200,
        { id: 5, uid: "04A1B2C3", album: { ...dsotm, title: "Solo Violin" }, created_at: 1 },
      ],
    });
    renderApp("/catalog");
    await placeSleeve({ device_id: 1, new: true, tag: { id: 5, uid: "04A1B2C3", created_at: 1 } });
    await userEvent.type(await screen.findByLabelText("Search albums"), "seifert");
    await userEvent.click(await screen.findByRole("button", { name: /Solo Violin/ }));

    await waitFor(() =>
      expect(calls.find((c) => c.key === "POST /api/tags/04A1B2C3/assign")?.body).toEqual({ release_id: 4455667 }),
    );
  });

  it("shows a sleeve that is already in the library", async () => {
    base({});
    renderApp("/catalog");
    await screen.findByText("Put the next sleeve on the stand.");
    await placeSleeve({ device_id: 1, new: false, tag: { id: 5, uid: "04A1B2C3", album: dsotm, created_at: 1 } });
    expect(await screen.findByText(/Already in your library/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Search albums")).not.toBeInTheDocument();
  });

  it("toggles cataloguing", async () => {
    let active = false;
    const calls = base({
      "GET /api/catalog/mode": () => [200, { active }],
      "PUT /api/catalog/mode": (body) => {
        active = (body as { active: boolean }).active;
        return [200, { active }];
      },
    });
    renderApp("/catalog");
    await userEvent.click(await screen.findByRole("button", { name: "Start cataloguing" }));
    expect(await screen.findByRole("button", { name: "Stop cataloguing" })).toBeInTheDocument();
    expect(calls.find((c) => c.key === "PUT /api/catalog/mode")?.body).toEqual({ active: true });
  });
});
