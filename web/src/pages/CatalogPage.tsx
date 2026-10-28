import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type CatalogMode, type Tag } from "../api/client";
import { catalogCurrentKey, catalogModeKey, unassignedKey, type CatalogTag } from "../api/events";
import { AssignTag } from "../components/AssignTag";
import { formatPressing } from "../format";

export function CatalogPage() {
  const queryClient = useQueryClient();
  const mode = useQuery({ queryKey: catalogModeKey, queryFn: () => api<CatalogMode>("/api/catalog/mode") });
  const current = useQuery<CatalogTag | null>({ queryKey: catalogCurrentKey, queryFn: () => null, staleTime: Infinity });
  const unassigned = useQuery({ queryKey: unassignedKey, queryFn: () => api<Tag[]>("/api/tags/unassigned") });
  const [editing, setEditing] = useState<string | null>(null);
  const active = mode.data?.active ?? false;
  const until = mode.data?.until;

  // The server lets cataloguing lapse quietly, so check again once it is due to end.
  useEffect(() => {
    if (!until) return;
    const t = setTimeout(() => queryClient.invalidateQueries({ queryKey: catalogModeKey }), Date.parse(until) - Date.now() + 500);
    return () => clearTimeout(t);
  }, [until, queryClient]);
  useEffect(() => {
    if (mode.data && !mode.data.active) queryClient.setQueryData(catalogCurrentKey, null);
  }, [mode.data, queryClient]);

  const toggle = useMutation({
    mutationFn: (active: boolean) => api<CatalogMode>("/api/catalog/mode", { method: "PUT", body: { active } }),
    onSuccess: (state) => queryClient.setQueryData(catalogModeKey, state),
  });

  const saved = () => setEditing(null);

  const tag = current.data?.tag;
  const others = unassigned.data?.filter((t) => t.uid !== tag?.uid) ?? [];

  return (
    <section>
      <div className="row heading">
        <h1>Catalogue</h1>
        <button className={active ? "secondary" : ""} onClick={() => toggle.mutate(!active)} disabled={toggle.isPending}>
          {active ? "Stop cataloguing" : "Start cataloguing"}
        </button>
      </div>

      {active ? (
        <div className="card">
          {!tag && <p>Put the next sleeve on the stand.</p>}
          {tag && tag.album && editing !== tag.uid && (
            <div className="saved">
              <p>
                {current.data?.new ? "Saved" : "Already in your library"}: <strong>{tag.album.title}</strong>{" "}
                <span className="muted">· {tag.album.artist}</span>
              </p>
              {tag.release && (
                <p className="muted">
                  {formatPressing(tag.release)}
                </p>
              )}
              <p className="muted">Put the next sleeve on the stand.</p>
              <button className="link" onClick={() => setEditing(tag.uid)}>
                Change album
              </button>
            </div>
          )}
          {tag && (!tag.album || editing === tag.uid) && (
            <>
              <p className="muted">
                New sleeve <code>{tag.uid}</code>. What is it?
              </p>
              <AssignTag key={tag.uid} uid={tag.uid} onDone={saved} />
            </>
          )}
        </div>
      ) : (
        <p className="muted">
          Turn on cataloguing, then put your records on the stand one by one and pick each album. Nothing counts as a
          listen while cataloguing is on, and it switches itself off when no new sleeve shows up for a while.
        </p>
      )}

      {others.length > 0 && (
        <>
          <h2>Sleeves waiting for an album</h2>
          <ul className="list">
            {others.map((t) => (
              <li key={t.uid} className="card">
                <div className="row">
                  <code>{t.uid}</code>
                  {editing !== t.uid && (
                    <button className="secondary" onClick={() => setEditing(t.uid)}>
                      Assign
                    </button>
                  )}
                </div>
                {editing === t.uid && <AssignTag uid={t.uid} onDone={saved} />}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
