import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

export type TagSeen = { device_id: number; uid: string };

export const nowPlayingKey = ["now-playing"] as const;

const RETRY_MS = 3000;

export function useLiveEvents(enabled: boolean) {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!enabled) return;
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    const connect = () => {
      source = new EventSource("/api/events");
      source.addEventListener("tag_seen", (e) => {
        const data = JSON.parse((e as MessageEvent).data) as TagSeen;
        queryClient.setQueryData<Record<number, TagSeen>>(nowPlayingKey, (prev) => ({
          ...prev,
          [data.device_id]: data,
        }));
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
