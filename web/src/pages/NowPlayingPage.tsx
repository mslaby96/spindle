import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "../api/client";
import { nowPlayingKey, type NowPlaying } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { formatCountdown, formatTime } from "../format";
import { useDevices } from "./DevicesPage";

export function NowPlayingPage() {
  const { data: playing } = useQuery({
    queryKey: nowPlayingKey,
    queryFn: () => api<NowPlaying[]>("/api/now-playing"),
  });
  const { data: devices } = useDevices();

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
        <article
          className={p.ends_at ? "card now away" : "card now"}
          key={p.device_id}
          style={{ borderColor: p.album?.cover_color }}
        >
          {p.album ? (
            <>
              <Cover albumId={p.album.id} hasCover={p.album.has_cover} color={p.album.cover_color} />
              <div>
                {showStand && <p className="muted">{devices?.find((d) => d.id === p.device_id)?.name}</p>}
                <h1>{p.album.title}</h1>
                <p>{p.album.artist}</p>
                <p className="muted">
                  {[p.album.year, ...p.album.styles].filter(Boolean).join(" · ")}
                  {p.started_at && ` · since ${formatTime(p.started_at)}`}
                </p>
                {p.ends_at && <EndsIn at={p.ends_at} />}
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

function EndsIn({ at }: { at: string }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const left = new Date(at).getTime() - now;
  return (
    <p className="ending">
      {left > 0
        ? `Sleeve off the stand. This listen ends in ${formatCountdown(left)} unless you put it back.`
        : "Sleeve off the stand. Wrapping up this listen…"}
    </p>
  );
}
