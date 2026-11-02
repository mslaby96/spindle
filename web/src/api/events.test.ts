import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Device } from "./client";
import { applyDeviceRemoved, applyDeviceStatus, applyImportProgress, devicesKey, discogsImportKey, untaggedKey } from "./events";

const stand = (id: number, online: boolean): Device => ({ id, name: `Stand ${id}`, created_at: "2026-11-01T10:00:00Z", online, wifi_change_pending: false, setup_wifi_password: "d97zws8agpqh" });

describe("stand events", () => {
  it("updates the stand whose status changed and leaves the others", () => {
    const qc = new QueryClient();
    qc.setQueryData<Device[]>(devicesKey, [stand(1, true), stand(2, true)]);
    applyDeviceStatus(qc, { ...stand(2, false), last_seen_at: "2026-11-01T11:00:00Z" });
    expect(qc.getQueryData<Device[]>(devicesKey)).toEqual([
      stand(1, true),
      { ...stand(2, false), last_seen_at: "2026-11-01T11:00:00Z" },
    ]);
  });

  it("drops a removed stand", () => {
    const qc = new QueryClient();
    qc.setQueryData<Device[]>(devicesKey, [stand(1, true), stand(2, true)]);
    applyDeviceRemoved(qc, { id: 1 });
    expect(qc.getQueryData<Device[]>(devicesKey)).toEqual([stand(2, true)]);
  });

  it("does nothing before the stands were loaded", () => {
    const qc = new QueryClient();
    applyDeviceStatus(qc, stand(1, false));
    applyDeviceRemoved(qc, { id: 1 });
    expect(qc.getQueryData(devicesKey)).toBeUndefined();
  });
});

describe("discogs import events", () => {
  it("keeps the progress and refreshes the library once the import is done", () => {
    const qc = new QueryClient();
    qc.setQueryData(["library", "artist"], []);
    qc.setQueryData(untaggedKey, []);
    applyImportProgress(qc, { state: "running", done: 1, total: 4 });
    expect(qc.getQueryData(discogsImportKey)).toEqual({ state: "running", done: 1, total: 4 });
    expect(qc.getQueryState(["library", "artist"])?.isInvalidated).toBe(false);
    const done = {
      state: "done" as const,
      done: 4,
      total: 4,
      summary: { found: 5, new_albums: 4, new_pressings: 5, already_in_library: 0, skipped_removed: [], skipped_removed_pressings: 0 },
    };
    applyImportProgress(qc, done);
    expect(qc.getQueryData(discogsImportKey)).toEqual(done);
    expect(qc.getQueryState(["library", "artist"])?.isInvalidated).toBe(true);
    expect(qc.getQueryState(untaggedKey)?.isInvalidated).toBe(true);
  });
});
