import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { api, type DiaryEntry } from "../api/client";
import { Cover } from "../components/Cover";
import { dye } from "../colors";
import { Divider } from "../components/Divider";
import { QueryState } from "../components/QueryState";
import { Rating } from "../components/RatingStars";
import { formatLength, formatMonth, formatWeekday } from "../format";

const pageSize = 50;

export function DiaryPage() {
  const diary = useInfiniteQuery({
    queryKey: ["diary"],
    initialPageParam: "",
    queryFn: ({ pageParam }) => api<DiaryEntry[]>(`/api/diary?limit=${pageSize}${pageParam}`),
    getNextPageParam: (last) => {
      if (last.length < pageSize) return undefined;
      const e = last[last.length - 1];
      return `&before=${encodeURIComponent(e.started_at)}&before_id=${e.session_id}`;
    },
  });
  const entries = diary.data?.pages.flat() ?? [];

  let month = "";
  return (
    <section>
      <h1>Diary</h1>
      {!diary.data && <QueryState query={diary} what="your diary" />}
      {diary.isSuccess && entries.length === 0 && <p className="muted">No listens yet.</p>}
      <ul className="list diary">
        {entries.map((e) => {
          const m = formatMonth(e.started_at);
          const header = m !== month ? (month = m) : null;
          const d = new Date(e.started_at);
          return (
            <li key={e.session_id}>
              {header && <Divider color={dye(header)}>{header}</Divider>}
              <Link to={`/albums/${e.album_id}`} className="entry">
                <span className="day">
                  <strong>{d.getDate()}</strong>
                  <span className="muted">{formatWeekday(e.started_at)}</span>
                </span>
                <Cover albumId={e.album_id} hasCover={e.has_cover} color={e.cover_color} className="thumb-lg" />
                <span className="grow">
                  <strong>{e.title}</strong> <span className="muted">· {e.artist}</span>
                  <span className="muted block">
                    {formatLength(e.listened_sec)}
                    {e.plays > 1 && ` · ${e.plays}×`}
                    {e.rating !== undefined && <> · <Rating value={e.rating} /></>}
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
