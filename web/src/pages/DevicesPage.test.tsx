import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const stand = (extra = {}) => ({
  id: 3,
  name: "Living room",
  created_at: "2026-01-01T00:00:00Z",
  last_seen_at: new Date().toISOString(),
  fw_version: "0.2.0",
  online: true,
  ...extra,
});

afterEach(() => vi.unstubAllGlobals());

function setup(devices: unknown[]) {
  mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/inbox": () => [200, []],
    "GET /api/devices": () => [200, devices],
  });
  renderApp("/devices");
}

async function row(name: string) {
  return (await screen.findByText(name)).closest("li")!;
}

describe("stand status", () => {
  it("shows a stand that is reporting with its diagnostics", async () => {
    setup([stand({ rssi: -58, reader_resets: 2, uptime_sec: 3 * 3600 + 12 * 60 })]);
    const r = within(await row("Living room"));
    expect(r.getByText("Online")).toBeInTheDocument();
    expect(r.getByText(/Wi-Fi strong/)).toBeInTheDocument();
    expect(r.getByText("−58 dBm")).toBeInTheDocument();
    expect(r.getByText("2 reader resets")).toBeInTheDocument();
    expect(r.getByText("up 3 h 12 min")).toBeInTheDocument();
    expect(r.queryByText(/last known/)).not.toBeInTheDocument();
  });

  it("says since when a stand is offline and marks its diagnostics as last known", async () => {
    setup([stand({ online: false, last_seen_at: "2026-10-29T15:18:00Z", rssi: -84, reader_resets: 1, uptime_sec: 600 })]);
    const r = within(await row("Living room"));
    expect(r.getByText("Offline")).toBeInTheDocument();
    expect(r.getByText(/^Offline since Thu 29 Oct, \d\d:18/)).toBeInTheDocument();
    expect(r.getByText(/Wi-Fi weak/)).toBeInTheDocument();
    expect(r.getByText("1 reader reset")).toBeInTheDocument();
    expect(r.getByText(/last known/)).toBeInTheDocument();
  });

  it("leaves out diagnostics the stand doesn't report", async () => {
    setup([stand({ reader_resets: 0 })]);
    const r = within(await row("Living room"));
    expect(r.getByText("0 reader resets")).toBeInTheDocument();
    expect(r.queryByText(/Wi-Fi/)).not.toBeInTheDocument();
    expect(r.queryByText(/^up /)).not.toBeInTheDocument();
  });

  it("adds no empty line under an online stand that never sent its firmware version", async () => {
    setup([stand({ fw_version: undefined })]);
    const r = await row("Living room");
    expect(within(r).getByText("Online")).toBeInTheDocument();
    for (const p of r.querySelectorAll("p")) expect(p.textContent?.trim()).not.toBe("");
  });

  it("shows a stand that never connected", async () => {
    setup([stand({ online: false, last_seen_at: undefined, fw_version: undefined })]);
    const r = within(await row("Living room"));
    expect(r.getByText("Never connected")).toBeInTheDocument();
    expect(r.queryByText("Online")).not.toBeInTheDocument();
  });
});
