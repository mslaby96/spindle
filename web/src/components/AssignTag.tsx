import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, type SearchResult, type Tag } from "../api/client";
import { applyAssignedTag } from "../api/events";
import { AlbumSearch } from "./AlbumSearch";
import { ReleasePicker } from "./ReleasePicker";

export function AssignTag({ uid, onDone }: { uid: string; onDone?: (tag: Tag) => void }) {
  const queryClient = useQueryClient();
  const [master, setMaster] = useState<SearchResult | null>(null);

  const assign = useMutation({
    mutationFn: (body: { master_id?: number; release_id?: number }) =>
      api<Tag>(`/api/tags/${uid}/assign`, { body }),
    onSuccess: (tag) => {
      applyAssignedTag(queryClient, tag);
      onDone?.(tag);
    },
  });

  function pick(r: SearchResult) {
    if (r.kind === "release") assign.mutate({ release_id: r.id });
    else setMaster(r);
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
      {assign.error && <p className="error">{assign.error.message}</p>}
    </div>
  );
}
