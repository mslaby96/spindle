import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";

import { api, type LibraryItem } from "../api/client";
import { Cover } from "../components/Cover";
import { formatRating } from "../components/RatingStars";
import { formatDate, timeAgo } from "../format";

const sorts = [
  ["last_played", "Last played"],
  ["plays", "Most played"],
  ["artist", "Artist"],
  ["title", "Title"],
  ["added", "Recently added"],
] as const;

export function LibraryPage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "forgotten" ? "forgotten" : "all";
  return (
    <section>
      <div className="row heading">
        <h1>Library</h1>
        <div className="tabs" role="tablist">
          <button role="tab" aria-selected={tab === "all"} onClick={() => setParams({})}>
            All records
          </button>
          <button role="tab" aria-selected={tab === "forgotten"} onClick={() => setParams({ tab: "forgotten" })}>
            Forgotten shelf
          </button>
        </div>
      </div>
      {tab === "all" ? <AllRecords /> : <ForgottenShelf />}
    </section>
  );
}

function AllRecords() {
  const [sort, setSort] = useState<string>("last_played");
  const [filter, setFilter] = useState("");
  const { data } = useQuery({
    queryKey: ["library", sort],
    queryFn: () => api<LibraryItem[]>(`/api/library?sort=${sort}`),
  });
  const needle = filter.trim().toLowerCase();
  const items = data?.filter((i) => !needle || `${i.title} ${i.artist}`.toLowerCase().includes(needle));

  if (data?.length === 0) {
    return (
      <p className="muted">
        Your library is empty. <Link to="/catalog">Catalogue your records</Link> to get started.
      </p>
    );
  }
  return (
    <>
      <div className="row toolbar">
        <input placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter library" />
        <select value={sort} onChange={(e) => setSort(e.target.value)} aria-label="Sort by">
          {sorts.map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </div>
      {data && <p className="muted">{data.length} records</p>}
      <Grid items={items ?? []} />
    </>
  );
}

function ForgottenShelf() {
  const { data } = useQuery({
    queryKey: ["library", "forgotten"],
    queryFn: () => api<{ not_recently: LibraryItem[]; never: LibraryItem[] }>("/api/library/forgotten"),
  });
  if (!data) return null;
  return (
    <>
      <h2>Not played in a long time</h2>
      {data.not_recently.length === 0 ? (
        <p className="muted">Nothing gathering dust. Nice.</p>
      ) : (
        <Grid items={data.not_recently} />
      )}
      <h2>Never played since you catalogued it</h2>
      {data.never.length === 0 ? <p className="muted">You have played every record at least once.</p> : <Grid items={data.never} />}
    </>
  );
}

function Grid({ items }: { items: LibraryItem[] }) {
  return (
    <ul className="grid">
      {items.map((i) => (
        <li key={i.album_id}>
          <Link to={`/albums/${i.album_id}`} className="tile">
            <Cover albumId={i.album_id} hasCover={i.has_cover} color={i.cover_color} className="tile-cover" />
            <strong>{i.title}</strong>
            <span className="muted">{i.artist}</span>
            <span className="muted small">
              {i.last_played_at ? `${timeAgo(i.last_played_at)} · ${i.plays}×` : `added ${formatDate(i.added_at)}`}
              {i.rating !== undefined && ` · ${formatRating(i.rating)}`}
            </span>
          </Link>
        </li>
      ))}
    </ul>
  );
}
