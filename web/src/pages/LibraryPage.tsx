import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";

import { api, type LibraryItem } from "../api/client";
import { Cover } from "../components/Cover";
import { dye } from "../colors";
import { Divider } from "../components/Divider";
import { QueryState } from "../components/QueryState";
import { Rating } from "../components/RatingStars";
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
  const records = useQuery({
    queryKey: ["library", sort],
    queryFn: () => api<LibraryItem[]>(`/api/library?sort=${sort}`),
  });
  const { data } = records;
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
      {!data && <QueryState query={records} what="your library" />}
      {items && (
        <p className="muted">
          {needle ? `${items.length} of ${data?.length} records` : `${items.length} records`}
        </p>
      )}
      {sort === "artist" || sort === "title" ? (
        <Filed items={items ?? []} by={sort} />
      ) : (
        <Grid items={items ?? []} />
      )}
    </>
  );
}

function ForgottenShelf() {
  const shelf = useQuery({
    queryKey: ["library", "forgotten"],
    queryFn: () => api<{ not_recently: LibraryItem[]; never: LibraryItem[] }>("/api/library/forgotten"),
  });
  const { data } = shelf;
  if (!data) return <QueryState query={shelf} what="the forgotten shelf" />;
  return (
    <>
      <Divider color="var(--tomato)">Not played in a long time</Divider>
      {data.not_recently.length === 0 ? (
        <p className="muted">Nothing gathering dust. Nice.</p>
      ) : (
        <Grid items={data.not_recently} />
      )}
      <Divider color="var(--plum)">Never played since you catalogued it</Divider>
      {data.never.length === 0 ? <p className="muted">You have played every record at least once.</p> : <Grid items={data.never} stickers={false} />}
    </>
  );
}

// The server owns how records file (it drops a leading "The"), so the letter comes from its sort key.
function letterOf(item: LibraryItem, by: "artist" | "title") {
  const c = item[`${by}_key`].charAt(0).toUpperCase();
  return /[A-Z]/.test(c) ? c : "#";
}

function Filed({ items, by }: { items: LibraryItem[]; by: "artist" | "title" }) {
  const groups: { letter: string; items: LibraryItem[] }[] = [];
  for (const i of items) {
    const letter = letterOf(i, by);
    const last = groups[groups.length - 1];
    if (last?.letter === letter) last.items.push(i);
    else groups.push({ letter, items: [i] });
  }
  return (
    <>
      {groups.map((g) => (
        <div key={g.letter}>
          <Divider color={dye(g.letter)} at={g.letter === "#" ? 0 : (g.letter.charCodeAt(0) - 65) / 25}>
            {g.letter}
          </Divider>
          <Grid items={g.items} />
        </div>
      ))}
    </>
  );
}

function Grid({ items, stickers = true }: { items: LibraryItem[]; stickers?: boolean }) {
  return (
    <ul className="grid">
      {items.map((i) => (
        <li key={i.album_id}>
          <Link to={`/albums/${i.album_id}`} className="tile">
            <Cover albumId={i.album_id} hasCover={i.has_cover} color={i.cover_color} className="tile-cover" />
            {stickers && !i.last_played_at && <span className="sticker">Unplayed</span>}
            <strong>{i.title}</strong>
            <span className="muted">{i.artist}</span>
            <span className="muted small">
              {i.last_played_at ? `${timeAgo(i.last_played_at)} · ${i.plays}×` : `added ${formatDate(i.added_at)}`}
            </span>
            {i.rating !== undefined && <Rating value={i.rating} />}
          </Link>
        </li>
      ))}
    </ul>
  );
}
