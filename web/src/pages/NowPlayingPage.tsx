import { useQuery } from "@tanstack/react-query";

import { api, type Device } from "../api/client";
import { nowPlayingKey, type NowPlaying } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { devicesKey } from "./DevicesPage";

export function NowPlayingPage() {
  const { data: playing } = useQuery({
    queryKey: nowPlayingKey,
    queryFn: () => api<NowPlaying[]>("/api/now-playing"),
  });
  const { data: devices } = useQuery({ queryKey: devicesKey, queryFn: () => api<Device[]>("/api/devices") });

  if (!playing || playing.length === 0) {
    return (
      <section className="empty">
        <h1>Nothing on the stand</h1>
        <p className="muted">Put a sleeve on your stand and it will show up here.</p>
      </section>
    );
  }

  const showStand = (devices?.length ?? 0) > 1;
  return (
    <section>
      {playing.map((p) => (
        <article className="card now" key={p.device_id} style={{ borderColor: p.album?.cover_color }}>
          {p.album ? (
            <>
              {p.album.has_cover ? (
                <img className="cover" src={`/api/albums/${p.album.id}/cover`} alt="" />
              ) : (
                <div className="cover" style={{ background: p.album.cover_color }} />
              )}
              <div>
                {showStand && <p className="muted">{devices?.find((d) => d.id === p.device_id)?.name}</p>}
                <h1>{p.album.title}</h1>
                <p>{p.album.artist}</p>
                <p className="muted">
                  {[p.album.year, ...p.album.styles].filter(Boolean).join(" · ")}
                  {p.started_at && ` · since ${new Date(p.started_at).toLocaleTimeString("en-GB", { timeStyle: "short" })}`}
                </p>
              </div>
            </>
          ) : (
            <div className="grow">
              <h1>New sleeve</h1>
              <p className="muted">
                This listen is being recorded. Tell Spindle which album <code>{p.uid}</code> is.
              </p>
              {p.uid && <AssignTag uid={p.uid} />}
            </div>
          )}
        </article>
      ))}
    </section>
  );
}
