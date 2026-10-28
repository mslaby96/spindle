const locale = "en-GB";

export function formatLength(seconds: number) {
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
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
