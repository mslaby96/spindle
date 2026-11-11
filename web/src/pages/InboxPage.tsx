import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type InboxCard } from "../api/client";
import { inboxKey } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { QueryState } from "../components/QueryState";
import { tabStyle } from "../colors";
import { Rating, RatingStars } from "../components/RatingStars";
import { formatDateTime, formatLength } from "../format";

export function useInbox() {
  return useQuery({ queryKey: inboxKey, queryFn: () => api<InboxCard[]>("/api/inbox") });
}


export const undoMs = 5000;

type Deferred = { key: string; commit: () => Promise<unknown> };
type Toast = { message: string; undo?: () => void };

// Skipping and answering take effect after a few seconds so a mis-tap can be undone; leaving the page commits them.
function useUndoable(onDone: () => void) {
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [toast, setToast] = useState<Toast | null>(null);
  const pending = useRef<{ deferred: Deferred; timer: ReturnType<typeof setTimeout> } | null>(null);
  const show = (key: string, on: boolean) =>
    setHidden((prev) => {
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });

  const commit = useCallback(
    (d: Deferred) =>
      d.commit().then(onDone, (err: Error) => {
        show(d.key, false);
        setToast({ message: err.message });
      }),
    [onDone],
  );
  const flush = useCallback(() => {
    if (!pending.current) return;
    clearTimeout(pending.current.timer);
    const { deferred } = pending.current;
    pending.current = null;
    setToast(null);
    return commit(deferred);
  }, [commit]);
  useEffect(() => () => void flush(), [flush]);

  function defer(deferred: Deferred, message: string) {
    flush();
    show(deferred.key, true);
    const timer = setTimeout(flush, undoMs);
    pending.current = { deferred, timer };
    setToast({
      message,
      undo: () => {
        clearTimeout(timer);
        pending.current = null;
        show(deferred.key, false);
        setToast(null);
      },
    });
  }
  function notify(message: string) {
    setToast({ message });
  }
  return { hidden, toast, defer, notify };
}

export function InboxPage() {
  const queryClient = useQueryClient();
  const inbox = useInbox();
  const refresh = useCallback(() => queryClient.invalidateQueries({ queryKey: inboxKey }), [queryClient]);
  const undoable = useUndoable(refresh);
  const cards = inbox.data;
  if (!cards) {
    return (
      <section>
        <h1>After listening</h1>
        <QueryState query={inbox} what="your inbox" />
      </section>
    );
  }
  const visible = cards.filter((c) => !undoable.hidden.has(`${c.session.id}:card`));
  return (
    <section>
      <h1>After listening</h1>
      {visible.length === 0 && <p className="muted">All caught up. Listens will show up here once a sleeve comes off the stand.</p>}
      {visible.map((c) => (
        <ListenCard
          key={c.session.id}
          card={c}
          questionHidden={undoable.hidden.has(`${c.session.id}:question`)}
          defer={undoable.defer}
          notify={undoable.notify}
        />
      ))}
      {undoable.toast && (
        <div className="toast" role="status">
          <span>{undoable.toast.message}</span>
          {undoable.toast.undo && (
            <button className="link" onClick={undoable.toast.undo}>
              Undo
            </button>
          )}
        </div>
      )}
    </section>
  );
}

const answers: Record<string, string> = {
  forgot: "forgot to take it off",
  repeated: "played several times",
  longer: "listened longer",
};

type CardProps = {
  card: InboxCard;
  questionHidden: boolean;
  defer: (d: Deferred, message: string) => void;
  notify: (message: string) => void;
};

function ListenCard({ card, questionHidden, defer, notify }: CardProps) {
  const queryClient = useQueryClient();
  const { session, album } = card;
  const [rating, setRating] = useState<number | undefined>(session.rating);
  const [note, setNote] = useState(session.note ?? "");
  const refresh = () => queryClient.invalidateQueries({ queryKey: inboxKey });

  const review = useMutation({
    mutationFn: () => api(`/api/sessions/${session.id}/review`, { method: "PUT", body: { rating, note } }),
    onSuccess: () => {
      notify(`Saved ${album?.title ?? "the listen"} to your diary`);
      refresh();
    },
  });
  const dismiss = (what: string) =>
    defer({ key: `${session.id}:card`, commit: () => api(`/api/sessions/${session.id}/dismiss`, { method: "POST" }) }, what);
  const resolve = (resolution: string) =>
    defer(
      { key: `${session.id}:question`, commit: () => api(`/api/sessions/${session.id}/resolve`, { body: { resolution } }) },
      `${album?.title}: ${answers[resolution]}`,
    );
  // The whole album was played and its stored length is what's wrong: the answer carries the real length.
  const resolveLength = (minutes: number) =>
    defer(
      {
        key: `${session.id}:question`,
        commit: () =>
          api(`/api/sessions/${session.id}/resolve`, { body: { resolution: "wrong_length", album_length_sec: minutes * 60 } }),
      },
      `${album?.title}: the album is ${minutes} min`,
    );
  const [fixingLength, setFixingLength] = useState(false);
  const error = review.error;

  if (!album) {
    return (
      <article className="listen">
        <div className="listen-head">
          <span className="tab">{formatDateTime(session.started_at)}</span>
        </div>
        <p>
          <strong>Unknown sleeve</strong> <span className="muted">· {formatLength(session.listened_sec || 0)}</span>
        </p>
        <p className="muted">
          Which album is <code>{card.uid}</code>? The listen will be counted once you pick it.
        </p>
        <AssignTag uid={card.uid} onDone={refresh} />
        <button className="link" onClick={() => dismiss("Deleted the unknown listen")}>
          Not a real listen, delete it
        </button>
        {error && <p className="error">{error.message}</p>}
      </article>
    );
  }

  const asking = session.state === "pending_confirmation";
  const pending = asking && !questionHidden;
  return (
    <article className="listen" style={tabStyle(album.cover_color)}>
      <div className="listen-head">
        <span className="tab">{formatDateTime(session.started_at)}</span>
      </div>
      <div className="row top">
        <Cover albumId={album.id} hasCover={album.has_cover} color={album.cover_color} className="thumb-xl" />
        <div className="grow">
          <p className="listen-title">{album.title}</p>
          <p className="artist">{album.artist}</p>
          <p className="muted">
            {formatLength(session.listened_sec)}
            {album.rating !== undefined && (
              <>
                {" "}· your rating <Rating value={album.rating} />
              </>
            )}
          </p>
          {!session.duration_confirmed && <span className="sticker">Unconfirmed</span>}
        </div>
      </div>

      {pending && (
        <div className="confirm">
          <p>
            The sleeve stayed on the stand for {formatLength(session.listened_sec)}
            {album.length_sec ? `, the album is ${formatLength(album.length_sec)}` : ""}. What happened?
          </p>
          {fixingLength ? (
            <AlbumLengthAnswer
              initialMinutes={Math.round(session.listened_sec / 60)}
              onSave={(minutes) => {
                setFixingLength(false);
                resolveLength(minutes);
              }}
              onBack={() => setFixingLength(false)}
            />
          ) : (
            <div className="choices">
              <button className="secondary" onClick={() => resolve("forgot")}>
                I forgot to take it off
              </button>
              <button className="secondary" onClick={() => resolve("repeated")}>
                I played it several times
              </button>
              <button className="secondary" onClick={() => resolve("longer")}>
                I listened longer
              </button>
              <button className="secondary" onClick={() => setFixingLength(true)}>
                I listened to the whole album, its length is wrong
              </button>
            </div>
          )}
        </div>
      )}

      <RatingStars value={rating} onChange={setRating} highlight={album.rating === undefined} />
      <textarea
        placeholder="A note about this listen (optional)"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        rows={2}
        aria-label="Note"
      />
      <div className="row">
        {asking ? (
          <span />
        ) : (
          <button className="link" onClick={() => dismiss(`Skipped ${album.title}`)}>
            Skip
          </button>
        )}
        <button onClick={() => review.mutate()} disabled={review.isPending || (rating === undefined && note.trim() === "")}>
          Save
        </button>
      </div>
      {error && <p className="error">{error.message}</p>}
    </article>
  );
}

const maxAlbumMinutes = 6 * 60;

function AlbumLengthAnswer({ initialMinutes, onSave, onBack }: { initialMinutes: number; onSave: (minutes: number) => void; onBack: () => void }) {
  const id = useId();
  const [minutes, setMinutes] = useState(String(initialMinutes));
  const value = Number(minutes);
  const valid = minutes.trim() !== "" && Number.isInteger(value) && value >= 1 && value <= maxAlbumMinutes;
  return (
    <form
      className="length-answer"
      onSubmit={(e) => {
        e.preventDefault();
        if (valid) onSave(value);
      }}
    >
      <label htmlFor={id}>How long is the album?</label>
      <span className="row inline">
        <input id={id} type="number" min={1} max={maxAlbumMinutes} value={minutes} onChange={(e) => setMinutes(e.target.value)} />
        <span className="muted">min</span>
      </span>
      <p className="muted small">This listen counts as one full play, and the album keeps this length.</p>
      <div className="choices">
        <button disabled={!valid}>Save length</button>
        <button type="button" className="secondary" onClick={onBack}>
          Back
        </button>
      </div>
    </form>
  );
}
