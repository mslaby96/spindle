import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Device } from "./client";
import { applyDeviceRemoved, applyDeviceStatus, devicesKey } from "./events";

const stand = (id: number, online: boolean): Device => ({ id, name: `Stand ${id}`, created_at: "2026-11-01T10:00:00Z", online });

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
