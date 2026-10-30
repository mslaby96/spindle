import type { Release } from "./api/client";

const locale = "en-GB";

export function formatPressing(r: Release) {
  return [r.country, r.year, r.label, r.catno, r.format].filter(Boolean).join(" · ");
}

export function formatLength(seconds: number) {
  const minutes = Math.round(seconds / 60);
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return h ? `${h} h ${m} min` : `${m} min`;
}

export function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(locale, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}

export function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" });
}

export function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString(locale, { timeStyle: "short" });
}

export function timeAgo(iso: string, now = Date.now()) {
  const days = Math.floor((now - new Date(iso).getTime()) / 86_400_000);
  if (days < 1) return "today";
  if (days === 1) return "yesterday";
  if (days < 60) return `${days} days ago`;
  const months = Math.floor(days / 30);
  if (months < 24) return `${months} months ago`;
  return `${Math.floor(months / 12)} years ago`;
}

export function formatMonth(iso: string) {
  return new Date(iso).toLocaleDateString(locale, { month: "long", year: "numeric" });
}

export function formatWeekday(iso: string) {
  return new Date(iso).toLocaleDateString(locale, { weekday: "short" });
}

export function formatDayMonth(iso: string) {
  return new Date(iso).toLocaleDateString(locale, { day: "numeric", month: "short" });
}

export function formatFull(iso: string) {
  return new Date(iso).toLocaleString(locale);
}

export function formatCountdown(ms: number) {
  const total = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

export function formatElapsed(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}
