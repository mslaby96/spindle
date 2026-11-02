import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api, type SearchResult } from "../api/client";
import { useDebounced } from "../hooks/useDebounced";

// Only cataloguing, where the next step is always typing, takes focus; elsewhere it would pop the phone keyboard.
export function AlbumSearch({ onPick, autoFocus = false }: { onPick: (r: SearchResult) => void; autoFocus?: boolean }) {
  const [query, setQuery] = useState("");
  const q = useDebounced(query.trim(), 300);
  const results = useQuery({
    queryKey: ["discogs-search", q],
    queryFn: ({ signal }) => api<SearchResult[]>(`/api/discogs/search?q=${encodeURIComponent(q)}`, { signal }),
    enabled: q.length >= 2,
    staleTime: 5 * 60_000,
  });

  return (
    <div className="search">
      <input
        autoFocus={autoFocus}
        placeholder="Artist or album, e.g. Pink Floyd Dark Side"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        aria-label="Search albums"
      />
      {results.isFetching && <p className="muted">Searching Discogs…</p>}
      {results.error && <p className="error">{results.error.message}</p>}
      {results.data?.length === 0 && q.length >= 2 && !results.isFetching && <p className="muted">No matches.</p>}
      <ul className="results">
        {results.data?.map((r) => (
          <li key={`${r.kind}-${r.id}`}>
            <button className="result" onClick={() => onPick(r)}>
              {r.thumb ? <img src={r.thumb} alt="" /> : <span className="thumb" />}
              <span>
                <strong>{r.title}</strong>
                <span className="muted">
                  {[r.artist, r.year, r.kind === "release" ? [r.country, r.catno].filter(Boolean).join(" ") : null]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
