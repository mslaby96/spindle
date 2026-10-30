import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router";

import { api, type Album, type AlbumDetail, type DiaryEntry } from "../api/client";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { tabStyle } from "../colors";
import { Divider } from "../components/Divider";
import { Rating } from "../components/RatingStars";
import { formatDate, formatDateTime, formatLength, formatPressing } from "../format";

export function AlbumPage() {
  const id = Number(useParams().id);
  const queryClient = useQueryClient();
  const key = ["album", id];
  const { data, error } = useQuery({ queryKey: key, queryFn: () => api<AlbumDetail>(`/api/albums/${id}`) });
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: key });
    queryClient.invalidateQueries({ queryKey: ["library"] });
  };

  if (error) return <p className="error">{error.message}</p>;
  if (!data) return null;
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

      {releases.length > 0 && (
        <>
          <Divider color={album.cover_color}>Your pressing</Divider>
          {releases.map((r) => (
            <p key={r.id} className="muted">
              {formatPressing(r)}
            </p>
          ))}
        </>
      )}

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
        Look it up again
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
  const [start, setStart] = useState(toLocalInput(entry.started_at));
  const [minutes, setMinutes] = useState(String(Math.round(entry.listened_sec / 60)));
  const edit = useMutation({
    mutationFn: () => {
      const started = new Date(start);
      const ended = new Date(started.getTime() + Number(minutes) * 60_000);
      return api(`/api/sessions/${entry.session_id}`, {
        method: "PUT",
        body: { started_at: started.toISOString(), ended_at: ended.toISOString() },
      });
    },
    onSuccess: () => {
      setEditing(false);
      onChanged();
    },
  });
  const remove = useMutation({
    mutationFn: () => api(`/api/sessions/${entry.session_id}`, { method: "DELETE" }),
    onSuccess: onChanged,
  });

  return (
    <li className="card">
      {editing ? (
        <form
          className="row inline"
          onSubmit={(e) => {
            e.preventDefault();
            edit.mutate();
          }}
        >
          <input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} aria-label="Started" />
          <input type="number" min={1} value={minutes} onChange={(e) => setMinutes(e.target.value)} aria-label="Minutes listened" />
          <span className="muted">min</span>
          <button disabled={edit.isPending}>Save</button>
          <button type="button" className="link" onClick={() => setEditing(false)}>
            Cancel
          </button>
        </form>
      ) : (
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
          <div className="actions">
            <button className="link" onClick={() => setEditing(true)}>
              Edit
            </button>
            <button className="link" onClick={() => confirm("Delete this listen?") && remove.mutate()}>
              Delete
            </button>
          </div>
        </div>
      )}
      {(edit.error ?? remove.error) && <p className="error">{(edit.error ?? remove.error)!.message}</p>}
    </li>
  );
}
