import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import { api } from "../api/client";
import { Cover } from "../components/Cover";
import { tabStyle } from "../colors";
import { Divider } from "../components/Divider";
import { QueryState } from "../components/QueryState";
import { formatDayMonth, formatLength } from "../format";

type Ranked = { name: string; value: number };
type Top = {
  summary: { plays: number; listened_sec: number; unconfirmed_sec: number; albums: number };
  albums: { album_id: number; title: string; artist: string; has_cover: boolean; cover_color?: string; value: number }[];
  genres: Ranked[];
  styles: Ranked[];
};
type Week = { start: string; plays: number; listened_sec: number };
type Measure = "plays" | "time";

const periods = [
  ["30d", "30 days"],
  ["90d", "90 days"],
  ["year", "Year"],
  ["all", "All time"],
] as const;

function formatValue(measure: Measure, v: number) {
  return measure === "plays" ? `${v}×` : formatLength(v);
}

export function StatsPage() {
  const [period, setPeriod] = useState("30d");
  const [measure, setMeasure] = useState<Measure>("plays");
  const top = useQuery({
    queryKey: ["stats", "top", period, measure],
    queryFn: () => api<Top>(`/api/stats/top?period=${period}&measure=${measure}`),
  });
  const weeks = useQuery({ queryKey: ["stats", "timeline"], queryFn: () => api<Week[]>("/api/stats/timeline?weeks=26") });
  const s = top.data?.summary;

  return (
    <section>
      <h1>Stats</h1>
      <div className="row toolbar filters-bar">
        <div className="tabs" role="group" aria-label="Period">
          {periods.map(([value, label]) => (
            <button key={value} aria-pressed={period === value} onClick={() => setPeriod(value)}>
              {label}
            </button>
          ))}
        </div>
        <div className="tabs" role="group" aria-label="Measure">
          <button aria-pressed={measure === "plays"} onClick={() => setMeasure("plays")}>
            Listens
          </button>
          <button aria-pressed={measure === "time"} onClick={() => setMeasure("time")}>
            Time
          </button>
        </div>
      </div>

      {!top.data && <QueryState query={top} what="your stats" />}
      {s && (
        <div className="tiles">
          <div className="tile-stat">
            <span className="muted">Listens</span>
            <strong>{s.plays}</strong>
          </div>
          <div className="tile-stat">
            <span className="muted">Time listened</span>
            <strong>{formatLength(s.listened_sec)}</strong>
            {s.unconfirmed_sec > 0 && <span className="muted small">incl. {formatLength(s.unconfirmed_sec)} unconfirmed</span>}
          </div>
          <div className="tile-stat">
            <span className="muted">Different albums</span>
            <strong>{s.albums}</strong>
          </div>
        </div>
      )}

      {weeks.data && <WeeklyChart weeks={weeks.data} />}
      {weeks.error && <QueryState query={weeks} what="the weekly chart" />}

      {top.data && top.data.albums.length === 0 && <p className="muted">No listens in this period.</p>}
      {top.data && top.data.albums.length > 0 && (
        <div className="columns">
          <div>
            <Divider color="var(--tomato)">Top albums</Divider>
            <ol className="ranked">
              {top.data.albums.map((a) => (
                <li key={a.album_id}>
                  <Link to={`/albums/${a.album_id}`} className="ranked-album">
                    <Cover albumId={a.album_id} hasCover={a.has_cover} color={a.cover_color} className="thumb-sm" />
                    <span className="grow ellipsis">
                      {a.title} <span className="muted">· {a.artist}</span>
                    </span>
                    <span className="value">{formatValue(measure, a.value)}</span>
                  </Link>
                </li>
              ))}
            </ol>
          </div>
          <RankedBars title="Top genres" color="var(--teal)" items={top.data.genres} measure={measure} />
          <RankedBars title="Top styles" color="var(--cobalt)" items={top.data.styles} measure={measure} />
        </div>
      )}
    </section>
  );
}

function RankedBars({ title, color, items, measure }: { title: string; color: string; items: Ranked[]; measure: Measure }) {
  if (items.length === 0) return null;
  const maxValue = Math.max(...items.map((i) => i.value));
  return (
    <div style={tabStyle(color)}>
      <Divider>{title}</Divider>
      <ol className="ranked bars">
        {items.map((i) => (
          <li key={i.name}>
            <span className="label">
              <span className="ellipsis">{i.name}</span>
              <span className="value">{formatValue(measure, i.value)}</span>
            </span>
            <span className="track">
              <span className="fill" style={{ width: `${(i.value / maxValue) * 100}%` }} />
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function WeeklyChart({ weeks }: { weeks: Week[] }) {
  const [asTable, setAsTable] = useState(false);
  return (
    <div className="card chart">
      <div className="row">
        <Divider color="var(--mustard)">Listens per week</Divider>
        <button className="link" onClick={() => setAsTable(!asTable)}>
          {asTable ? "Show chart" : "Show as table"}
        </button>
      </div>
      {asTable ? (
        <table>
          <thead>
            <tr>
              <th>Week of</th>
              <th>Listens</th>
              <th>Time</th>
            </tr>
          </thead>
          <tbody>
            {weeks.map((w) => (
              <tr key={w.start}>
                <td>{formatDayMonth(w.start)}</td>
                <td>{w.plays}</td>
                <td>{formatLength(w.listened_sec)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={weeks} margin={{ top: 8, right: 4, bottom: 0, left: -24 }} barCategoryGap={2}>
            <CartesianGrid vertical={false} stroke="var(--grid)" />
            <XAxis
              dataKey="start"
              tickFormatter={formatDayMonth}
              tick={{ fill: "var(--ink-2)", fontSize: 12 }}
              axisLine={{ stroke: "var(--grid)" }}
              tickLine={false}
              interval="preserveStartEnd"
              minTickGap={24}
            />
            <YAxis allowDecimals={false} tick={{ fill: "var(--ink-2)", fontSize: 12 }} axisLine={false} tickLine={false} />
            <Tooltip
              cursor={{ fill: "var(--grid)", opacity: 0.5 }}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const w = payload[0].payload as Week;
                return (
                  <div className="chart-tip">
                    <strong>{w.plays} listens</strong>
                    <span className="muted">
                      {formatLength(w.listened_sec)} · week of {formatDayMonth(w.start)}
                    </span>
                  </div>
                );
              }}
            />
            <Bar dataKey="plays" fill="var(--mustard)" radius={[3, 3, 0, 0]} maxBarSize={24} isAnimationActive={false} />
          </BarChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}
