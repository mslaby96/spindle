import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, mockApi, renderApp } from "../test-utils";

const me = { id: 1, username: "maciej" };
const stand = (extra = {}) => ({
  id: 3,
  name: "Living room",
  created_at: "2026-01-01T00:00:00Z",
  last_seen_at: new Date().toISOString(),
  fw_version: "0.2.0",
  online: true,
  wifi_change_pending: false,
  ...extra,
});

afterEach(() => vi.unstubAllGlobals());

function setup(devices: unknown[] | (() => unknown[]), extra: Record<string, () => [number, unknown?]> = {}) {
  const calls = mockApi({
    "GET /api/setup": () => [200, { needed: false }],
    "GET /api/me": () => [200, me],
    "GET /api/inbox": () => [200, []],
    "GET /api/devices": () => [200, typeof devices === "function" ? devices() : devices],
    ...extra,
  });
  renderApp("/devices");
  return calls;
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
    expect(r.queryByText(/Wi-Fi (strong|ok|weak)/)).not.toBeInTheDocument();
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

describe("changing a stand's Wi-Fi", () => {
  it("explains what happens and asks before sending the stand away", async () => {
    let pending = false;
    const calls = setup(() => [stand({ wifi_change_pending: pending })], {
      "POST /api/devices/3/wifi-change": () => {
        pending = true;
        return [204];
      },
    });
    const r = within(await row("Living room"));
    await userEvent.click(r.getByRole("button", { name: "Change Wi-Fi" }));
    expect(r.getByText(/open its own network/)).toBeInTheDocument();
    expect(r.getByText(/spindle\.setup/)).toBeInTheDocument();

    await userEvent.click(r.getByRole("button", { name: "Keep it" }));
    expect(r.queryByText(/open its own network/)).not.toBeInTheDocument();
    expect(calls.some((c) => c.key === "POST /api/devices/3/wifi-change")).toBe(false);

    await userEvent.click(r.getByRole("button", { name: "Change Wi-Fi" }));
    await userEvent.click(r.getByRole("button", { name: "Forget Wi-Fi" }));
    expect(await r.findByText(/Waiting for Living room to pick this up/)).toBeInTheDocument();
    expect(calls.filter((c) => c.key === "POST /api/devices/3/wifi-change")).toHaveLength(1);
  });

  it("lets a waiting request be cancelled", async () => {
    let pending = true;
    const calls = setup(() => [stand({ wifi_change_pending: pending })], {
      "DELETE /api/devices/3/wifi-change": () => {
        pending = false;
        return [204];
      },
    });
    const r = within(await row("Living room"));
    expect(r.getByText(/Waiting for Living room to pick this up/)).toBeInTheDocument();
    expect(r.queryByRole("button", { name: "Change Wi-Fi" })).not.toBeInTheDocument();
    await userEvent.click(r.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(r.queryByText(/Waiting for/)).not.toBeInTheDocument());
    expect(calls.some((c) => c.key === "DELETE /api/devices/3/wifi-change")).toBe(true);
    expect(r.getByRole("button", { name: "Change Wi-Fi" })).toBeInTheDocument();
    expect(r.queryByText(/opening its setup network/)).not.toBeInTheDocument();
  });

  it("says an offline stand picks it up when it is back", async () => {
    setup([stand({ online: false, wifi_change_pending: true })]);
    const r = within(await row("Living room"));
    expect(r.getByText(/when it comes back online/)).toBeInTheDocument();
  });

  it("tells what to do next once the stand has picked it up", async () => {
    setup([stand({ wifi_change_pending: true })]);
    const r = within(await row("Living room"));
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    act(() => FakeEventSource.instances.at(-1)!.emit("device_status", stand({ wifi_change_pending: false })));
    expect(await r.findByText(/opening its setup network/)).toBeInTheDocument();
    expect(r.getByText(/http:\/\/spindle\.setup/)).toBeInTheDocument();
    await userEvent.click(r.getByRole("button", { name: "Done" }));
    expect(r.queryByText(/opening its setup network/)).not.toBeInTheDocument();
  });
});
