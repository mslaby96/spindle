import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { api, type DiaryEntry } from "../api/client";
import { Cover } from "../components/Cover";
import { formatRating } from "../components/RatingStars";
import { formatLength } from "../format";

const pageSize = 50;

function monthLabel(iso: string) {
  return new Date(iso).toLocaleDateString("en-GB", { month: "long", year: "numeric" });
}

export function DiaryPage() {
  const diary = useInfiniteQuery({
    queryKey: ["diary"],
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      api<DiaryEntry[]>(`/api/diary?limit=${pageSize}${pageParam ? `&before=${encodeURIComponent(pageParam)}` : ""}`),
    getNextPageParam: (last) => (last.length === pageSize ? last[last.length - 1].started_at : undefined),
  });
  const entries = diary.data?.pages.flat() ?? [];

  let month = "";
  return (
    <section>
      <h1>Diary</h1>
      {diary.isSuccess && entries.length === 0 && <p className="muted">No listens yet.</p>}
      <ul className="list diary">
        {entries.map((e) => {
          const m = monthLabel(e.started_at);
          const header = m !== month ? (month = m) : null;
          const d = new Date(e.started_at);
          return (
            <li key={e.session_id}>
              {header && <h2>{header}</h2>}
              <Link to={`/albums/${e.album_id}`} className="entry">
                <span className="day">
                  <strong>{d.getDate()}</strong>
                  <span className="muted">{d.toLocaleDateString("en-GB", { weekday: "short" })}</span>
                </span>
                <Cover albumId={e.album_id} hasCover={e.has_cover} color={e.cover_color} className="thumb-lg" />
                <span className="grow">
                  <strong>{e.title}</strong> <span className="muted">· {e.artist}</span>
                  <span className="muted block">
                    {formatLength(e.listened_sec)}
                    {e.plays > 1 && ` · ${e.plays}×`}
                    {e.rating !== undefined && ` · ${formatRating(e.rating)}`}
                  </span>
                  {e.note && <span className="block note">{e.note}</span>}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
      {diary.hasNextPage && (
        <button className="secondary" onClick={() => diary.fetchNextPage()} disabled={diary.isFetchingNextPage}>
          Older listens
        </button>
      )}
    </section>
  );
}
