import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";

import { api, type DiaryEntry } from "../api/client";
import { inboxKey, nowPlayingKey, recentListensKey, type NowPlaying } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { tabStyle } from "../colors";
import { Divider } from "../components/Divider";
import { formatCountdown, formatElapsed, formatLength, formatTime, timeAgo } from "../format";
import { useDevices } from "./DevicesPage";

export function NowPlayingPage() {
  const { data: playing } = useQuery({
    queryKey: nowPlayingKey,
    queryFn: () => api<NowPlaying[]>("/api/now-playing"),
  });
  const { data: devices } = useDevices();

  if (!playing || playing.length === 0) {
    const silent = devices && devices.length > 0 && devices.every((d) => !d.online);
    return (
      <section>
        {silent ? (
          <div className="empty">
            <h1>{devices.length === 1 ? "Your stand isn't answering" : "Your stands aren't answering"}</h1>
            <p className="muted">
              Check that it's plugged in and that your Wi-Fi is up. It shows up again as soon as it reports.
            </p>
            <Link to="/devices">See your stands</Link>
          </div>
        ) : (
          <div className="empty">
            <h1>Nothing on the stand</h1>
            <p className="muted">Put a sleeve on your stand and it will show up here.</p>
          </div>
        )}
        <LastOnTheStand />
      </section>
    );
  }

  const showStand = (devices?.length ?? 0) > 1;
  return (
    <section>
      {playing.map((p) =>
        p.album ? (
          <article className={p.ends_at ? "now away" : "now"} key={p.device_id} style={tabStyle(p.album.cover_color)}>
            <div className="stage">
              <span className="tab">
                {["On the stand", showStand && devices?.find((d) => d.id === p.device_id)?.name, p.started_at && `since ${formatTime(p.started_at)}`]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
              <Link to={`/albums/${p.album.id}`} aria-label={`${p.album.title} by ${p.album.artist}`}>
                <Cover albumId={p.album.id} hasCover={p.album.has_cover} color={p.album.cover_color} />
              </Link>
            </div>
            <div>
              <div className="now-head">
                <div className="grow">
                  <h1>{p.album.title}</h1>
                  <p className="artist">{p.album.artist}</p>
                  <p className="muted meta">{[p.album.year, ...p.album.styles.slice(0, 2)].filter(Boolean).join(" · ")}</p>
                </div>
                {p.started_at && <Clock since={p.started_at} lengthSec={p.album.duration_sec} />}
              </div>
              {p.started_at && p.album.duration_sec ? <Progress since={p.started_at} lengthSec={p.album.duration_sec} /> : null}
              {p.ends_at && <EndsIn at={p.ends_at} deviceId={p.device_id} />}
            </div>
          </article>
        ) : (
          <article className="now" key={p.device_id}>
            <div className="grow">
              <h1>New sleeve</h1>
              <p className="muted">
                This listen is being recorded. Tell Spindle which album <code>{p.uid}</code> is.
              </p>
              {p.uid && <AssignTag uid={p.uid} />}
            </div>
          </article>
        ),
      )}
      <LastOnTheStand />
    </section>
  );
}

function useNow(everyMs: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), everyMs);
    return () => clearInterval(t);
  }, [everyMs]);
  return now;
}

function Clock({ since, lengthSec }: { since: string; lengthSec?: number }) {
  const now = useNow(1000);
  return (
    <p className="clock">
      <strong>{formatElapsed(now - new Date(since).getTime())}</strong>
      <span>{lengthSec ? `of ${formatLength(lengthSec)}` : "playing"}</span>
    </p>
  );
}

function Progress({ since, lengthSec }: { since: string; lengthSec: number }) {
  const now = useNow(5000);
  const share = Math.min(Math.max((now - new Date(since).getTime()) / (lengthSec * 1000), 0), 1);
  return (
    <span className="progress">
      <span style={{ width: `${share * 100}%` }} />
    </span>
  );
}

function EndsIn({ at, deviceId }: { at: string; deviceId: number }) {
  const now = useNow(1000);
  const queryClient = useQueryClient();
  const end = useMutation({
    mutationFn: () => api(`/api/now-playing/${deviceId}/end`, { method: "POST" }),
    onSuccess: () => {
      for (const queryKey of [nowPlayingKey, recentListensKey, inboxKey]) queryClient.invalidateQueries({ queryKey });
    },
  });
  const left = new Date(at).getTime() - now;
  return (
    <div className="ending" aria-live="polite">
      <p>
        <span className="sticker">Sleeve off</span>
        {left > 0 ? (
          <span>
            This listen ends in <strong className="countdown">{formatCountdown(left)}</strong> unless you put it back.
          </span>
        ) : (
          <span>Wrapping up this listen…</span>
        )}
      </p>
      <button className="secondary" onClick={() => end.mutate()} disabled={end.isPending}>
        End listen now
      </button>
      {end.error && <p className="error">{end.error.message}</p>}
    </div>
  );
}

function LastOnTheStand() {
  const { data } = useQuery({
    queryKey: recentListensKey,
    queryFn: () => api<DiaryEntry[]>("/api/diary?limit=4"),
  });
  if (!data || data.length === 0) return null;
  return (
    <>
      <Divider color="var(--teal)">Last on the stand</Divider>
      <ul className="shelf">
        {data.map((e) => (
          <li key={e.session_id}>
            <Link to={`/albums/${e.album_id}`} className="tile">
              <Cover albumId={e.album_id} hasCover={e.has_cover} color={e.cover_color} className="tile-cover" />
              <strong>{e.title}</strong>
              <span className="muted small">{timeAgo(e.started_at)}</span>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}
