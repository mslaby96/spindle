import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api, type Version } from "../api/client";
import { useDebounced } from "./AlbumSearch";

type Props = {
  masterId: number;
  onPick: (releaseId: number) => void;
  onSkip: () => void;
  busy?: boolean;
};

export function ReleasePicker({ masterId, onPick, onSkip, busy }: Props) {
  const [country, setCountry] = useState("");
  const [year, setYear] = useState("");
  const [catno, setCatno] = useState("");
  const filters = useDebounced({ country: country.trim(), year: year.trim(), catno: catno.trim() }, 400);

  const versions = useQuery({
    queryKey: ["versions", masterId, filters],
    queryFn: ({ signal }) => {
      const q = new URLSearchParams();
      if (filters.country) q.set("country", filters.country);
      if (filters.year) q.set("year", filters.year);
      if (filters.catno) q.set("catno", filters.catno);
      return api<Version[]>(`/api/discogs/masters/${masterId}/versions?${q}`, { signal });
    },
    staleTime: 5 * 60_000,
  });

  return (
    <div className="picker">
      <p className="muted">Which pressing is it? This is optional, you can add it later.</p>
      <div className="filters">
        <input placeholder="Country" value={country} onChange={(e) => setCountry(e.target.value)} aria-label="Country" />
        <input placeholder="Year" value={year} onChange={(e) => setYear(e.target.value)} aria-label="Year" />
        <input
          placeholder="Catalogue no. (on the spine)"
          value={catno}
          onChange={(e) => setCatno(e.target.value)}
          aria-label="Catalogue number"
        />
      </div>
      <button className="secondary" onClick={onSkip} disabled={busy}>
        Skip, just save the album
      </button>
      {versions.isFetching && <p className="muted">Loading pressings…</p>}
      {versions.error && <p className="error">{versions.error.message}</p>}
      <ul className="results">
        {versions.data?.map((v) => (
          <li key={v.id}>
            <button className="result" onClick={() => onPick(v.id)} disabled={busy}>
              {v.thumb ? <img src={v.thumb} alt="" /> : <span className="thumb" />}
              <span>
                <strong>{[v.country, v.released].filter(Boolean).join(" ")}</strong>
                <span className="muted">{[v.label, v.catno, v.format].filter(Boolean).join(" · ")}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
