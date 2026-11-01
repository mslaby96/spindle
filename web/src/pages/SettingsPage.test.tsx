import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("your data", () => {
  it("offers all of it for download", async () => {
    mockApi({
      "GET /api/setup": () => [200, { needed: false }],
      "GET /api/me": () => [200, { id: 1, username: "maciej" }],
    });
    renderApp("/settings");
    const json = await screen.findByRole("link", { name: /Everything \(JSON\)/ });
    expect(json).toHaveAttribute("href", "/api/export.json");
    expect(json).toHaveAttribute("download");
    expect(screen.getByRole("link", { name: /Diary \(CSV\)/ })).toHaveAttribute("href", "/api/export.csv");
  });
});
