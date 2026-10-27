import { useQuery } from "@tanstack/react-query";

import { api, type Device } from "../api/client";
import { nowPlayingKey, type TagSeen } from "../api/events";
import { devicesKey } from "./DevicesPage";

export function NowPlayingPage() {
  const { data: seen } = useQuery<Record<number, TagSeen>>({
    queryKey: nowPlayingKey,
    queryFn: () => ({}),
    staleTime: Infinity,
  });
  const { data: devices } = useQuery({ queryKey: devicesKey, queryFn: () => api<Device[]>("/api/devices") });

  const active = Object.values(seen ?? {}).filter((s) => s.uid);
  if (active.length === 0) {
    return (
      <section className="empty">
        <h1>Nothing on the stand</h1>
        <p className="muted">Put a sleeve on your stand and it will show up here.</p>
      </section>
    );
  }

  return (
    <section>
      <h1>Now playing</h1>
      {active.map((s) => (
        <article className="card" key={s.device_id}>
          <p className="muted">{devices?.find((d) => d.id === s.device_id)?.name ?? "Stand"}</p>
          <p>
            Tag <code>{s.uid}</code>
          </p>
        </article>
      ))}
    </section>
  );
}
