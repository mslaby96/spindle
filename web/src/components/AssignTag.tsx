import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, ApiError, type SearchResult, type Tag } from "../api/client";
import { applyAssignedTag } from "../api/events";
import { AlbumSearch } from "./AlbumSearch";
import { ReleasePicker } from "./ReleasePicker";

type Choice = { master_id?: number; release_id?: number; listens?: "move" | "keep" };

// listensToMove reads the server's answer when the sleeve already has listens of the album it is being taken from.
function listensToMove(err: unknown) {
  return err instanceof ApiError && err.status === 409 ? Number(err.data?.listens) || 0 : 0;
}

export function AssignTag({ uid, current, onDone }: { uid: string; current?: string; onDone?: (tag: Tag) => void }) {
  const queryClient = useQueryClient();
  const [master, setMaster] = useState<SearchResult | null>(null);
  const [picked, setPicked] = useState<Choice | null>(null);

  const assign = useMutation({
    mutationFn: (body: Choice) => api<Tag>(`/api/tags/${uid}/assign`, { body }),
    onMutate: (body) => setPicked(body),
    onSuccess: (tag) => {
      applyAssignedTag(queryClient, tag);
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
        <AlbumSearch onPick={pick} />
      )}
      {assign.isPending && <p className="muted">Saving…</p>}
      {assign.error && !pending && <p className="error">{assign.error.message}</p>}
    </div>
  );
}
