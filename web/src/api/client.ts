export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public data?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export async function api<T = void>(
  path: string,
  init: { method?: string; body?: unknown; signal?: AbortSignal } = {},
): Promise<T> {
  const res = await fetch(path, {
    signal: init.signal,
    method: init.method ?? (init.body === undefined ? "GET" : "POST"),
    headers: {
      "X-Spindle": "1",
      ...(init.body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
    credentials: "same-origin",
  });
  if (!res.ok) {
    let message = res.statusText;
    let data: Record<string, unknown> | undefined;
    try {
      data = await res.json();
      message = (data?.error as string) ?? message;
    } catch {
      // body was not JSON
    }
    throw new ApiError(res.status, message, data);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export type Me = { id: number; username: string };

export type Device = {
  id: number;
  name: string;
  fw_version?: string;
  last_seen_at?: string;
  created_at: string;
};

export const meKey = ["me"] as const;
export const setupKey = ["setup"] as const;

export type Album = {
  id: number;
  discogs_master_id?: number;
  discogs_release_id?: number;
  title: string;
  artist: string;
  year?: number;
  genres: string[];
  styles: string[];
  cover_color?: string;
  has_cover: boolean;
  duration_sec?: number;
  duration_status: string;
  removed_at?: string;
};

export type Release = {
  id: number;
  album_id: number;
  discogs_release_id: number;
  title: string;
  country?: string;
  year?: number;
  label?: string;
  catno?: string;
  format?: string;
};

export type Tag = { id: number; uid: string; album?: Album; release?: Release; created_at: number };

export type SearchResult = {
  kind: "master" | "release";
  id: number;
  artist: string;
  title: string;
  year?: string;
  country?: string;
  format?: string[];
  thumb?: string;
  catno?: string;
};

export type Version = {
  id: number;
  title: string;
  label: string;
  catno: string;
  country: string;
  released: string;
  format: string;
  thumb: string;
};

export type CatalogMode = { active: boolean; until?: string };

export type Session = {
  id: number;
  device_id: number;
  tag_id: number;
  album_id?: number;
  state: "active" | "unclassified" | "discarded" | "counted" | "pending_confirmation";
  started_at: string;
  ended_at?: string;
  play_count: number;
  listened_sec: number;
  duration_confirmed: boolean;
  resolution?: string;
  rating?: number;
  note?: string;
  inbox_state: string;
};

export type InboxCard = {
  session: Session;
  uid: string;
  album?: { id: number; title: string; artist: string; has_cover: boolean; cover_color?: string; rating?: number; length_sec?: number };
};

export type LibraryItem = {
  album_id: number;
  title: string;
  artist: string;
  year?: number;
  has_cover: boolean;
  cover_color?: string;
  plays: number;
  listened_sec: number;
  last_played_at?: string;
  rating?: number;
  added_at: string;
  artist_key?: string;
  title_key?: string;
};

export type DiaryEntry = {
  session_id: number;
  album_id: number;
  title: string;
  artist: string;
  has_cover: boolean;
  cover_color?: string;
  started_at: string;
  plays: number;
  listened_sec: number;
  duration_confirmed: boolean;
  rating?: number;
  note?: string;
};

export type AlbumDetail = { album: Album; stats: LibraryItem; releases: Release[]; history: DiaryEntry[]; sleeves: string[] };
