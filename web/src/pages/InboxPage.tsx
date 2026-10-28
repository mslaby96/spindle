import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type InboxCard } from "../api/client";
import { inboxKey } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { Cover } from "../components/Cover";
import { formatRating, RatingStars } from "../components/RatingStars";
import { formatDateTime, formatLength } from "../format";

export function useInbox() {
  return useQuery({ queryKey: inboxKey, queryFn: () => api<InboxCard[]>("/api/inbox") });
}


export function InboxPage() {
  const { data: cards } = useInbox();
  if (!cards) return null;
  return (
    <section>
      <h1>After listening</h1>
      {cards.length === 0 && <p className="muted">All caught up. Listens will show up here once a sleeve comes off the stand.</p>}
      {cards.map((c) => (
        <ListenCard key={c.session.id} card={c} />
      ))}
    </section>
  );
}

function ListenCard({ card }: { card: InboxCard }) {
  const queryClient = useQueryClient();
  const { session, album } = card;
  const [rating, setRating] = useState<number | undefined>(session.rating);
  const [note, setNote] = useState(session.note ?? "");
  const refresh = () => queryClient.invalidateQueries({ queryKey: inboxKey });

  const resolve = useMutation({
    mutationFn: (resolution: string) => api(`/api/sessions/${session.id}/resolve`, { body: { resolution } }),
    onSuccess: refresh,
  });
  const review = useMutation({
    mutationFn: () => api(`/api/sessions/${session.id}/review`, { method: "PUT", body: { rating, note } }),
    onSuccess: refresh,
  });
  const dismiss = useMutation({
    mutationFn: () => api(`/api/sessions/${session.id}/dismiss`, { method: "POST" }),
    onSuccess: refresh,
  });
  const error = resolve.error ?? review.error ?? dismiss.error;

  if (!album) {
    return (
      <article className="card listen">
        <p>
          <strong>Unknown sleeve</strong> <span className="muted">· {formatDateTime(session.started_at)} · {formatLength(session.listened_sec || 0)}</span>
        </p>
        <p className="muted">
          Which album is <code>{card.uid}</code>? The listen will be counted once you pick it.
        </p>
        <AssignTag uid={card.uid} onDone={refresh} />
        <button className="link" onClick={() => dismiss.mutate()}>
          Not a real listen, delete it
        </button>
        {error && <p className="error">{error.message}</p>}
      </article>
    );
  }

  const pending = session.state === "pending_confirmation";
  return (
    <article className="card listen">
      <div className="row top">
        <Cover albumId={album.id} hasCover={album.has_cover} color={album.cover_color} className="thumb-lg" />
        <div className="grow">
          <p>
            <strong>{album.title}</strong> <span className="muted">· {album.artist}</span>
          </p>
          <p className="muted">
            {formatDateTime(session.started_at)} · {formatLength(session.listened_sec)}
            {!session.duration_confirmed && " (unconfirmed)"}
            {album.rating !== undefined && ` · your rating ${formatRating(album.rating)}`}
          </p>
        </div>
      </div>

      {pending && (
        <div className="confirm">
          <p>
            The sleeve stayed on the stand for {formatLength(session.listened_sec)}
            {album.length_sec ? `, the album is ${formatLength(album.length_sec)}` : ""}. What happened?
          </p>
          <div className="choices">
            <button className="secondary" onClick={() => resolve.mutate("forgot")} disabled={resolve.isPending}>
              I forgot to take it off
            </button>
            <button className="secondary" onClick={() => resolve.mutate("repeated")} disabled={resolve.isPending}>
              I played it several times
            </button>
            <button className="secondary" onClick={() => resolve.mutate("longer")} disabled={resolve.isPending}>
              I listened longer
            </button>
          </div>
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
        <button className="link" onClick={() => dismiss.mutate()} disabled={dismiss.isPending}>
          Skip
        </button>
        <button onClick={() => review.mutate()} disabled={review.isPending || (rating === undefined && note.trim() === "")}>
          Save
        </button>
      </div>
      {error && <p className="error">{error.message}</p>}
    </article>
  );
}
