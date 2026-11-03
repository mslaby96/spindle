import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router";

import { api, type Album, type AlbumDetail, type DiaryEntry, type Release } from "../api/client";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { tabStyle } from "../colors";
import { Divider } from "../components/Divider";
import { DiscogsLink } from "../components/DiscogsLink";
import { QueryState } from "../components/QueryState";
import { Rating, RatingStars } from "../components/RatingStars";
import { formatDate, formatDateTime, formatLength, formatPressing } from "../format";

export function AlbumPage() {
  const id = Number(useParams().id);
  const queryClient = useQueryClient();
  const key = ["album", id];
  const detail = useQuery({ queryKey: key, queryFn: () => api<AlbumDetail>(`/api/albums/${id}`) });
  const { data } = detail;
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: key });
    queryClient.invalidateQueries({ queryKey: ["library"] });
  };

  if (!data) return <QueryState query={detail} what="this album" />;
  const { album, stats, releases, history, sleeves } = data;

  return (
    <section>
      <div className="now album-head" style={tabStyle(album.cover_color)}>
        <div className="stage">
          {album.genres[0] && <span className="tab">{album.genres[0]}</span>}
          <Cover albumId={album.id} hasCover={album.has_cover} color={album.cover_color} />
        </div>
        <div>
          <h1>{album.title}</h1>
          <p className="artist">{album.artist}</p>
          <p className="muted">{[album.year, ...album.genres, ...album.styles.slice(0, 3)].filter(Boolean).join(" · ")}</p>
          <p className="muted facts">
            {stats.plays} {stats.plays === 1 ? "listen" : "listens"} · {formatLength(stats.listened_sec)} in total
            {stats.rating !== undefined && <> · <Rating value={stats.rating} /></>}
          </p>
          <AlbumLength albumId={album.id} seconds={album.duration_sec} status={album.duration_status} onSaved={refresh} />
          {album.discogs_master_id ? (
            <DiscogsLink kind="master" id={album.discogs_master_id} />
          ) : (
            album.discogs_release_id && <DiscogsLink kind="release" id={album.discogs_release_id} />
          )}
        </div>
      </div>

      {album.removed_at && <RemovedNotice album={album} removedAt={album.removed_at} />}

      {sleeves.length > 0 && (
        <>
          <Divider color={album.cover_color}>{sleeves.length === 1 ? "Sleeve" : "Sleeves"}</Divider>
          {sleeves.map((uid) => (
            <Sleeve key={uid} uid={uid} album={album} />
          ))}
        </>
      )}

      {releases.length > 0 && <Pressings color={album.cover_color} releases={releases} onChanged={refresh} />}

      <Divider color={album.cover_color}>Listens</Divider>
      {history.length === 0 && <p className="muted">Not played yet.</p>}
      <ul className="list">
        {history.map((h) => (
          <HistoryRow key={h.session_id} entry={h} onChanged={refresh} />
        ))}
      </ul>

      <AlbumActions album={album} plays={stats.plays} />
    </section>
  );
}

function useAlbumChange(albumId: number, action: "remove" | "restore" | "delete") {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      action === "delete"
        ? api(`/api/albums/${albumId}`, { method: "DELETE" })
        : api(`/api/albums/${albumId}/${action}`, { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

function RemovedNotice({ album, removedAt }: { album: Album; removedAt: string }) {
  const restore = useAlbumChange(album.id, "restore");
  return (
    <div className="notice">
      <p>
        No longer in your collection since {formatDate(removedAt)}. Its listens still count in your diary and stats.
      </p>
      <button className="secondary" onClick={() => restore.mutate()} disabled={restore.isPending}>
        Put back in collection
      </button>
      {restore.error && <p className="error">{restore.error.message}</p>}
    </div>
  );
}

function Sleeve({ uid, album }: { uid: string; album: Album }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [fixing, setFixing] = useState(false);
  return (
    <div className="card">
      <div className="row">
        <code>{uid}</code>
        <button className="link" onClick={() => setFixing(!fixing)}>
          {fixing ? "Cancel" : "Wrong album?"}
        </button>
      </div>
      {fixing && (
        <AssignTag
          uid={uid}
          current={album.title}
          onDone={(tag) => {
            queryClient.invalidateQueries();
            if (tag.album && tag.album.id !== album.id) navigate(`/albums/${tag.album.id}`);
            else setFixing(false);
          }}
        />
      )}
    </div>
  );
}

function AlbumActions({ album, plays }: { album: Album; plays: number }) {
  const navigate = useNavigate();
  const remove = useAlbumChange(album.id, "remove");
  const del = useAlbumChange(album.id, "delete");
  const listens = plays === 1 ? "1 listen" : `${plays} listens`;

  function removeFromCollection() {
    if (confirm(`Remove ${album.title} from your collection because you sold it or lost it? Its ${listens} stay in your diary and stats.`)) {
      remove.mutate();
    }
  }

  function deleteAlbum() {
    if (confirm(plays > 0 ? `Delete ${album.title} and its ${listens}? This can't be undone.` : `Delete ${album.title}? This can't be undone.`)) {
      del.mutate(undefined, { onSuccess: () => navigate("/library", { replace: true }) });
    }
  }

  return (
    <div className="row actions">
      {!album.removed_at && (
        <button className="secondary" onClick={removeFromCollection} disabled={remove.isPending}>
          Remove from collection
        </button>
      )}
      <button className="secondary danger" onClick={deleteAlbum} disabled={del.isPending}>
        Delete album
      </button>
      {(remove.error ?? del.error) && <p className="error">{(remove.error ?? del.error)?.message}</p>}
    </div>
  );
}

function AlbumLength({ albumId, seconds, status, onSaved }: { albumId: number; seconds?: number; status: string; onSaved: () => void }) {
  const [editing, setEditing] = useState(false);
  const [minutes, setMinutes] = useState("");
  const save = useMutation({
    mutationFn: (secs: number | null) => api(`/api/albums/${albumId}/duration`, { method: "PUT", body: { seconds: secs } }),
    onSuccess: () => {
      setEditing(false);
      onSaved();
    },
  });

  if (!editing) {
    const label =
      status === "pending" ? "Looking up the album length…" : seconds ? `Album length ${formatLength(seconds)}` : "Album length unknown";
    return (
      <p className="muted">
        {label} {status === "manual" && "(set by you)"}{" "}
        <button
          className="link"
          onClick={() => {
            setMinutes(seconds ? String(Math.round(seconds / 60)) : "");
            setEditing(true);
          }}
        >
          edit
        </button>
      </p>
    );
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    const secs = Math.round(Number(minutes) * 60);
    if (secs > 0) save.mutate(secs);
  }
  return (
    <form className="row inline" onSubmit={submit}>
      <input type="number" min={1} max={600} value={minutes} onChange={(e) => setMinutes(e.target.value)} aria-label="Album length in minutes" />
      <span className="muted">min</span>
      <button disabled={save.isPending}>Save</button>
      <button type="button" className="link" onClick={() => save.mutate(null)}>
        Look it up instead
      </button>
      {save.error && <span className="error">{save.error.message}</span>}
    </form>
  );
}

function toLocalInput(iso: string) {
  const d = new Date(iso);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

function HistoryRow({ entry, onChanged }: { entry: DiaryEntry; onChanged: () => void }) {
  const [editing, setEditing] = useState(false);
  const remove = useMutation({
    mutationFn: () => api(`/api/sessions/${entry.session_id}`, { method: "DELETE" }),
    onSuccess: onChanged,
  });

  return (
    <li className="card">
      <div className="row">
        <div>
          <p>
            {formatDateTime(entry.started_at)} · {formatLength(entry.listened_sec)}
            {entry.plays > 1 && ` · played ${entry.plays}×`}
            {!entry.duration_confirmed && (
              <>
                {" "}
                <span className="sticker">Unconfirmed</span>
              </>
            )}
            {entry.rating !== undefined && <> · <Rating value={entry.rating} /></>}
          </p>
          {entry.note && <p className="muted">{entry.note}</p>}
        </div>
        {!editing && (
          <div className="actions">
            <button className="link" onClick={() => setEditing(true)}>
              Edit
            </button>
            <button className="link" onClick={() => confirm("Delete this listen?") && remove.mutate()}>
              Delete
            </button>
          </div>
        )}
      </div>
      {editing && (
        <ListenEditor
          entry={entry}
          onDone={(saved) => {
            setEditing(false);
            if (saved) onChanged();
          }}
        />
      )}
      {remove.error && <p className="error">{remove.error.message}</p>}
    </li>
  );
}

// One form for everything about a listen; the server checks it all before changing anything.
function ListenEditor({ entry, onDone }: { entry: DiaryEntry; onDone: (saved: boolean) => void }) {
  const [rating, setRating] = useState<number | undefined>(entry.rating);
  const [note, setNote] = useState(entry.note ?? "");
  const [initialStart] = useState(() => toLocalInput(entry.started_at));
  const [initialMinutes] = useState(() => String(Math.round(entry.listened_sec / 60)));
  const [start, setStart] = useState(initialStart);
  const [minutes, setMinutes] = useState(initialMinutes);
  const save = useMutation({
    mutationFn: () => {
      // The shown length is rounded, and a listen's span can be longer than what counted, so times
      // only go back when the user changed them.
      const body: Record<string, unknown> = { review: { rating: rating ?? null, note } };
      if (start !== initialStart || minutes !== initialMinutes) {
        const started = new Date(start);
        body.started_at = started.toISOString();
        body.ended_at = new Date(started.getTime() + Number(minutes) * 60_000).toISOString();
      }
      return api(`/api/sessions/${entry.session_id}`, { method: "PUT", body });
    },
    onSuccess: () => onDone(true),
  });
  return (
    <form
      className="review"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <RatingStars value={rating} onChange={setRating} />
      <textarea
        placeholder="A note about this listen (optional)"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        rows={2}
        aria-label="Note"
      />
      <div className="when">
        <label>
          Started
          <input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
        </label>
        <label>
          Minutes listened
          <input type="number" min={1} value={minutes} onChange={(e) => setMinutes(e.target.value)} />
        </label>
      </div>
      <div className="choices">
        <button disabled={save.isPending}>Save</button>
        <button type="button" className="secondary" onClick={() => onDone(false)}>
          Cancel
        </button>
      </div>
      {save.error && <p className="error">{save.error.message}</p>}
    </form>
  );
}

function Pressings({ color, releases, onChanged }: { color?: string; releases: Release[]; onChanged: () => void }) {
  const owned = releases.filter((r) => !r.removed_at);
  const removed = releases.filter((r) => r.removed_at);
  return (
    <>
      <Divider color={color}>{owned.length > 1 ? "Your pressings" : "Your pressing"}</Divider>
      {owned.length === 0 && <p className="muted">No pressing of this album is in your collection.</p>}
      <ul className="list pressings">
        {owned.map((r) => (
          <PressingRow key={r.id} release={r} onChanged={onChanged} />
        ))}
      </ul>
      {removed.length > 0 && (
        <details className="removed-pressings">
          <summary>Removed pressings ({removed.length})</summary>
          <ul className="list pressings">
            {removed.map((r) => (
              <PressingRow key={r.id} release={r} onChanged={onChanged} />
            ))}
          </ul>
        </details>
      )}
    </>
  );
}

function PressingRow({ release: r, onChanged }: { release: Release; onChanged: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const change = useMutation({
    mutationFn: (action: "remove" | "restore") => api(`/api/pressings/${r.id}/${action}`, { method: "POST" }),
    onSuccess: () => {
      setConfirming(false);
      onChanged();
    },
  });
  return (
    <li className="pressing">
      <div className="row">
        <div>
          <p className="muted">{formatPressing(r)}</p>
          <DiscogsLink kind="release" id={r.discogs_release_id} />
        </div>
        {r.removed_at ? (
          <button className="link" onClick={() => change.mutate("restore")} disabled={change.isPending}>
            Put back
          </button>
        ) : (
          !confirming && (
            <button className="link" onClick={() => setConfirming(true)}>
              Remove
            </button>
          )
        )}
      </div>
      {confirming && (
        <div className="confirm">
          <p>Remove this pressing from your collection? Its listens stay, and an import from Discogs won't add it back.</p>
          <div className="choices">
            <button onClick={() => change.mutate("remove")} disabled={change.isPending}>
              Remove pressing
            </button>
            <button className="secondary" onClick={() => setConfirming(false)}>
              Keep it
            </button>
          </div>
        </div>
      )}
      {change.error && <p className="error">{change.error.message}</p>}
    </li>
  );
}
