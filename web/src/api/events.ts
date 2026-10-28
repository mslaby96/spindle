import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { Album, CatalogMode, Tag } from "./client";

export type NowPlaying = { device_id: number; active: boolean; uid?: string; album?: Album; started_at?: string };
export type CatalogTag = { device_id: number; tag: Tag; new: boolean };

export const nowPlayingKey = ["now-playing"] as const;
export const catalogModeKey = ["catalog", "mode"] as const;
export const catalogCurrentKey = ["catalog", "current"] as const;
export const unassignedKey = ["tags", "unassigned"] as const;
export const inboxKey = ["inbox"] as const;

const RETRY_MS = 3000;

export function useLiveEvents(enabled: boolean) {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!enabled) return;
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    const on = <T,>(type: string, handle: (data: T) => void) =>
      source!.addEventListener(type, (e) => handle(JSON.parse((e as MessageEvent).data) as T));

    const connect = () => {
      source = new EventSource("/api/events");
      on<NowPlaying>("now_playing", (data) =>
        queryClient.setQueryData<NowPlaying[]>(nowPlayingKey, (prev = []) => [
          ...prev.filter((p) => p.device_id !== data.device_id),
          ...(data.active ? [data] : []),
        ]),
      );
      on<CatalogMode>("catalog_mode", (data) => queryClient.setQueryData(catalogModeKey, data));
      on<CatalogTag>("catalog_tag", (data) => {
        queryClient.setQueryData(catalogCurrentKey, data);
        queryClient.invalidateQueries({ queryKey: unassignedKey });
      });
      on("inbox_changed", () => queryClient.invalidateQueries({ queryKey: inboxKey }));
      on<Tag>("tag_assigned", (tag) => {
        queryClient.invalidateQueries({ queryKey: inboxKey });
        queryClient.setQueryData<NowPlaying[]>(nowPlayingKey, (prev) =>
          prev?.map((p) => (p.uid === tag.uid ? { ...p, album: tag.album } : p)),
        );
        queryClient.setQueryData<CatalogTag | null>(catalogCurrentKey, (prev) =>
          prev && prev.tag.uid === tag.uid ? { ...prev, tag } : prev,
        );
        queryClient.invalidateQueries({ queryKey: unassignedKey });
      });
      source.onerror = () => {
        if (source?.readyState === EventSource.CLOSED && !stopped) {
          source.close();
          retry = setTimeout(connect, RETRY_MS);
        }
      };
    };
    connect();

    return () => {
      stopped = true;
      clearTimeout(retry);
      source?.close();
    };
  }, [enabled, queryClient]);
}
