import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type ImportProgress, type ImportSummary } from "../api/client";
import { discogsImportKey } from "../api/events";

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function DiscogsImport() {
  const queryClient = useQueryClient();
  const progress = useQuery({ queryKey: discogsImportKey, queryFn: () => api<ImportProgress>("/api/import/discogs") });
  const preview = useMutation({ mutationFn: () => api<ImportSummary>("/api/import/discogs/preview", { method: "POST" }) });
  const start = useMutation({
    mutationFn: () => api<ImportProgress>("/api/import/discogs", { method: "POST" }),
    onSuccess: (p) => {
      preview.reset();
      queryClient.setQueryData(discogsImportKey, p);
    },
  });
  const state = progress.data;

  if (state?.state === "running") {
    return (
      <div className="import">
        <p>
          Importing: {state.done} of {plural(state.total, "album", "albums")}. This carries on if you leave the page.
        </p>
        <progress value={state.done} max={state.total} aria-valuenow={state.done} aria-valuemax={state.total} />
      </div>
    );
  }

  return (
    <div className="import">
      {state?.state === "done" && state.summary && <Imported summary={state.summary} />}
      {state?.state === "failed" && <p className="error">The import stopped: {state.error}</p>}
      {preview.data ? (
        <Preview summary={preview.data} onStart={() => start.mutate()} onCancel={() => preview.reset()} busy={start.isPending} />
      ) : (
        <>
          <p className="muted">
            Adds the records in the Discogs collection of the account this server's token belongs to. It only adds:
            nothing already in Spindle changes, and you can run it again whenever your collection grows.
          </p>
          <div className="row">
            <button className="secondary" onClick={() => preview.mutate()} disabled={preview.isPending}>
              Check my Discogs collection
            </button>
          </div>
          {preview.isPending && <p className="muted">Asking Discogs…</p>}
        </>
      )}
      {(preview.error ?? start.error) && <p className="error">{(preview.error ?? start.error)!.message}</p>}
    </div>
  );
}

function Preview({ summary: s, onStart, onCancel, busy }: { summary: ImportSummary; onStart: () => void; onCancel: () => void; busy: boolean }) {
  const nothing = s.new_albums === 0 && s.new_pressings === 0;
  return (
    <div className="import-preview">
      <p>
        {plural(s.found, "record", "records")} on Discogs: {plural(s.new_albums, "new album", "new albums")},{" "}
        {plural(s.new_pressings, "new pressing", "new pressings")}, {s.already_in_library} already here.
      </p>
      {s.skipped_removed.length > 0 && (
        <p className="muted">
          {plural(s.skipped_removed.length, "record you removed stays", "records you removed stay")} removed:{" "}
          {s.skipped_removed.join(", ")}.
        </p>
      )}
      {s.skipped_removed_pressings > 0 && (
        <p className="muted">
          {plural(s.skipped_removed_pressings, "pressing you removed stays", "pressings you removed stay")} removed.
        </p>
      )}
      {nothing ? (
        <p className="muted">Nothing new to import.</p>
      ) : (
        <p className="muted">
          Spindle makes a backup first. Each new album takes about a second, so this one takes about{" "}
          {Math.max(1, Math.round(s.new_albums / 60))} min.
        </p>
      )}
      <div className="choices">
        {!nothing && (
          <button onClick={onStart} disabled={busy}>
            Start import
          </button>
        )}
        <button className="secondary" onClick={onCancel}>
          {nothing ? "OK" : "Not now"}
        </button>
      </div>
    </div>
  );
}

function Imported({ summary: s }: { summary: ImportSummary }) {
  return (
    <p className="success" role="status">
      Added {plural(s.new_albums, "album", "albums")} and {plural(s.new_pressings, "pressing", "pressings")}. Their covers
      and album lengths keep arriving over the next few minutes.
    </p>
  );
}
