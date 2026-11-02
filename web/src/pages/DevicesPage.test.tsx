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
  setup_wifi_password: "d97zws8agpqh",
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
    expect(r.getByRole("img", { name: "Wi-Fi signal: strong (3 of 3)" })).toBeInTheDocument();
    expect(r.queryByText(/dBm/)).not.toBeInTheDocument();
    expect(r.getByText("2 reader resets")).toBeInTheDocument();
    expect(r.getByText("up 3 h 12 min")).toBeInTheDocument();
    expect(r.queryByText(/last known/)).not.toBeInTheDocument();
  });

  it("says since when a stand is offline and marks its diagnostics as last known", async () => {
    setup([stand({ online: false, last_seen_at: "2026-10-29T15:18:00Z", rssi: -84, reader_resets: 1, uptime_sec: 600 })]);
    const r = within(await row("Living room"));
    expect(r.getByText("Offline")).toBeInTheDocument();
    expect(r.getByText(/^Offline since Thu 29 Oct, \d\d:18/)).toBeInTheDocument();
    expect(r.getByRole("img", { name: "Wi-Fi signal: weak (1 of 3)" })).toBeInTheDocument();
    expect(r.getByText("1 reader reset")).toBeInTheDocument();
    expect(r.getByText(/last known/)).toBeInTheDocument();
  });

  it("leaves out diagnostics the stand doesn't report", async () => {
    setup([stand({ reader_resets: 0 })]);
    const r = within(await row("Living room"));
    expect(r.getByText("0 reader resets")).toBeInTheDocument();
    expect(r.queryByRole("img", { name: /Wi-Fi signal/ })).not.toBeInTheDocument();
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

describe("updating a stand's firmware", () => {
  it("offers the newer version and asks for it", async () => {
    let update: unknown;
    const calls = setup(() => [stand({ fw_version: "0.5.0", update_available: "0.6.0", update })], {
      "POST /api/devices/3/update": () => {
        update = { version: "0.6.0", state: "requested" };
        return [204];
      },
    });
    const r = within(await row("Living room"));
    expect(r.getByText(/firmware 0\.5\.0 · 0\.6\.0 available/)).toBeInTheDocument();
    await userEvent.click(r.getByRole("button", { name: "Update" }));
    expect(await r.findByText(/waits until no record is on the stand/)).toBeInTheDocument();
    expect(calls.filter((c) => c.key === "POST /api/devices/3/update")).toHaveLength(1);
    expect(r.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();
  });

  it("offers nothing when the stand is up to date", async () => {
    setup([stand({ fw_version: "0.6.0" })]);
    const r = within(await row("Living room"));
    expect(r.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();
    expect(r.queryByText(/available/)).not.toBeInTheDocument();
    expect(r.queryByText(/now on/)).not.toBeInTheDocument();
  });

  it("lets a requested update be cancelled", async () => {
    let update: unknown = { version: "0.6.0", state: "requested" };
    const calls = setup(() => [stand({ fw_version: "0.5.0", update_available: "0.6.0", update })], {
      "DELETE /api/devices/3/update": () => {
        update = undefined;
        return [204];
      },
    });
    const r = within(await row("Living room"));
    await userEvent.click(r.getByRole("button", { name: "Cancel" }));
    expect(await r.findByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(calls.some((c) => c.key === "DELETE /api/devices/3/update")).toBe(true);
    expect(r.queryByText(/now on/)).not.toBeInTheDocument();
  });

  it("shows an installing update without a way to cancel it", async () => {
    setup([stand({ fw_version: "0.5.0", update_available: "0.6.0", update: { version: "0.6.0", state: "installing" } })]);
    const r = within(await row("Living room"));
    expect(r.getByText(/Installing 0\.6\.0/)).toBeInTheDocument();
    expect(r.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    expect(r.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();
  });

  it("explains a failed update, lets it be dismissed or tried again", async () => {
    let update: unknown = { version: "0.6.0", state: "failed" };
    const calls = setup(() => [stand({ fw_version: "0.5.0", update_available: "0.6.0", update })], {
      "DELETE /api/devices/3/update": () => {
        update = undefined;
        return [204];
      },
    });
    const r = within(await row("Living room"));
    expect(r.getByText(/0\.6\.0 didn't take/)).toBeInTheDocument();
    expect(r.getByText(/back on 0\.5\.0/)).toBeInTheDocument();
    expect(r.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    await userEvent.click(r.getByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(r.queryByText(/didn't take/)).not.toBeInTheDocument());
    expect(calls.some((c) => c.key === "DELETE /api/devices/3/update")).toBe(true);
  });

  it("says when the stand is on the new version", async () => {
    setup([stand({ fw_version: "0.5.0", update_available: "0.6.0", update: { version: "0.6.0", state: "installing" } })]);
    const r = within(await row("Living room"));
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    act(() => FakeEventSource.instances.at(-1)!.emit("device_status", stand({ fw_version: "0.6.0" })));
    expect(await r.findByText(/now on 0\.6\.0/)).toBeInTheDocument();
  });

  it("shows why the server refused an update", async () => {
    setup([stand({ fw_version: "0.5.0", update_available: "0.6.0" })], {
      "POST /api/devices/3/update": () => [409, { error: "already up to date" }],
    });
    const r = within(await row("Living room"));
    await userEvent.click(r.getByRole("button", { name: "Update" }));
    expect(await r.findByText("already up to date")).toBeInTheDocument();
  });
});

describe("the stand's setup network password", () => {
  it("is part of the Change Wi-Fi explanation", async () => {
    setup([stand()]);
    const r = within(await row("Living room"));
    await userEvent.click(r.getByRole("button", { name: "Change Wi-Fi" }));
    expect(r.getByText(/with the password/)).toBeInTheDocument();
    expect(r.getByText("d97zws8agpqh")).toBeInTheDocument();
  });

  it("is in the note on what to do once the stand has picked up the change", async () => {
    setup([stand({ wifi_change_pending: true })]);
    const r = within(await row("Living room"));
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    act(() => FakeEventSource.instances.at(-1)!.emit("device_status", stand({ wifi_change_pending: false })));
    const note = await r.findByRole("status");
    expect(within(note).getByText("d97zws8agpqh")).toBeInTheDocument();
  });

  it("is shown for an offline stand, which may have opened its setup network by itself", async () => {
    setup([stand({ online: false, last_seen_at: "2026-10-29T15:18:00Z" })]);
    const r = within(await row("Living room"));
    expect(r.getByText(/Setup network password/)).toBeInTheDocument();
    expect(r.getByText("d97zws8agpqh")).toBeInTheDocument();
  });

  it("stays out of the way for an online stand until asked for", async () => {
    setup([stand()]);
    const r = within(await row("Living room"));
    expect(r.queryByText("d97zws8agpqh")).not.toBeInTheDocument();
    await userEvent.click(r.getByRole("button", { name: "Setup network password" }));
    expect(r.getByText("d97zws8agpqh")).toBeInTheDocument();
  });

  it("can be copied where the browser allows it", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    setup([stand({ online: false, last_seen_at: "2026-10-29T15:18:00Z" })]);
    const r = within(await row("Living room"));
    await userEvent.click(r.getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith("d97zws8agpqh");
    expect(await r.findByText("Copied")).toBeInTheDocument();
  });
});

describe("the Wi-Fi signal icon", () => {
  it.each([
    [-50, "strong (3 of 3)", 3],
    [-67, "strong (3 of 3)", 3],
    [-68, "ok (2 of 3)", 2],
    [-80, "ok (2 of 3)", 2],
    [-81, "weak (1 of 3)", 1],
  ])("shows %i dBm as %s", async (rssi, label, lit) => {
    setup([stand({ rssi })]);
    const r = within(await row("Living room"));
    const icon = r.getByRole("img", { name: `Wi-Fi signal: ${label}` });
    expect(icon.querySelectorAll(".on")).toHaveLength(lit);
    expect(icon.querySelector("title")?.textContent).toBe(`Wi-Fi signal: ${label}`);
  });
});
