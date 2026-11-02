import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError, type Album, type SearchResult, type Tag } from "../api/client";
import { applyAssignedTag, untaggedKey } from "../api/events";
import { Cover } from "./Cover";
import { AlbumSearch } from "./AlbumSearch";
import { ReleasePicker } from "./ReleasePicker";

type Choice = { master_id?: number; release_id?: number; listens?: "move" | "keep" };

// listensToMove reads the server's answer when the sleeve already has listens of the album it is being taken from.
function listensToMove(err: unknown) {
  return err instanceof ApiError && err.status === 409 ? Number(err.data?.listens) || 0 : 0;
}

type Props = { uid: string; current?: string; onDone?: (tag: Tag) => void; autoFocus?: boolean };

export function AssignTag({ uid, current, onDone, autoFocus }: Props) {
  const queryClient = useQueryClient();
  const [master, setMaster] = useState<SearchResult | null>(null);
  const [picked, setPicked] = useState<Choice | null>(null);

  const assign = useMutation({
    mutationFn: (body: Choice) => api<Tag>(`/api/tags/${uid}/assign`, { body }),
    onMutate: (body) => setPicked(body),
    onSuccess: (tag) => {
      applyAssignedTag(queryClient, tag);
      queryClient.invalidateQueries({ queryKey: untaggedKey });
      onDone?.(tag);
    },
  });
  const pending = listensToMove(assign.error);

  function pick(r: SearchResult) {
    if (r.kind === "release") assign.mutate({ release_id: r.id });
    else setMaster(r);
  }

  if (pending > 0 && picked) {
    return (
      <div className="assign">
        <p>
          This sleeve has {pending} {pending === 1 ? "listen" : "listens"} of {current ?? "its current album"}. Were they
          really of the new album?
        </p>
        <div className="row">
          <button onClick={() => assign.mutate({ ...picked, listens: "move" })} disabled={assign.isPending}>
            Move them to the new album
          </button>
          <button className="secondary" onClick={() => assign.mutate({ ...picked, listens: "keep" })} disabled={assign.isPending}>
            Keep them with {current ?? "the old album"}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="assign">
      {master ? (
        <>
          <p>
            <strong>{master.title}</strong> <span className="muted">· {master.artist}</span>{" "}
            <button className="link" onClick={() => setMaster(null)}>
              change
            </button>
          </p>
          <ReleasePicker
            masterId={master.id}
            busy={assign.isPending}
            onSkip={() => assign.mutate({ master_id: master.id })}
            onPick={(id) => assign.mutate({ release_id: id })}
          />
        </>
      ) : (
        <CollectionPick
          onPick={(a) => assign.mutate(a.discogs_master_id ? { master_id: a.discogs_master_id } : { release_id: a.discogs_release_id })}
          busy={assign.isPending}
        >
          <AlbumSearch onPick={pick} autoFocus={autoFocus} />
        </CollectionPick>
      )}
      {assign.isPending && <p className="muted">Saving…</p>}
      {assign.error && !pending && <p className="error">{assign.error.message}</p>}
    </div>
  );
}

const firstFew = 8;

// Records already catalogued without a sleeve come first, so a sleeve for one of them needs no Discogs search.
function CollectionPick({ onPick, busy, children }: { onPick: (a: Album) => void; busy: boolean; children: ReactNode }) {
  const untagged = useQuery({ queryKey: untaggedKey, queryFn: () => api<Album[]>("/api/library/untagged") });
  const [filter, setFilter] = useState("");
  const [all, setAll] = useState(false);
  const albums = untagged.data ?? [];
  if (albums.length === 0) return <>{children}</>;
  const needle = filter.trim().toLowerCase();
  const matching = albums.filter((a) => !needle || `${a.artist} ${a.title}`.toLowerCase().includes(needle));
  const shown = needle || all ? matching : matching.slice(0, firstFew);
  return (
    <>
      <p className="assign-label">From your collection</p>
      <input placeholder="Artist or album" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter your collection" />
      <ul className="results collection" aria-label="From your collection">
        {shown.map((a) => (
          <li key={a.id}>
            <button className="result" onClick={() => onPick(a)} disabled={busy}>
              <Cover albumId={a.id} hasCover={a.has_cover} color={a.cover_color} className="thumb" />
              <span>
                <strong>{a.title}</strong>
                <span className="muted">{a.artist}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {shown.length < matching.length && (
        <button className="link" onClick={() => setAll(true)}>
          Show all {matching.length}
        </button>
      )}
      {shown.length === 0 && <p className="muted">None of your records match.</p>}
      <p className="assign-label">Or search Discogs</p>
      {children}
    </>
  );
}
