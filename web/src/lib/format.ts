const compactDateTimeFormatter = new Intl.DateTimeFormat("en-US", {
  month: "numeric",
  day: "numeric",
  year: "2-digit",
  hour: "numeric",
  minute: "2-digit",
  hour12: true,
});

function parseTimestamp(ts: string): Date | null {
  if (!ts) return null;
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return null;
  return d;
}

export function formatDateTime(ts: string): string {
  if (!ts) return "-";
  const d = parseTimestamp(ts);
  if (!d) return ts;
  return d.toLocaleString();
}

export function formatCompactDateTime(ts: string): string {
  if (!ts) return "-";
  const d = parseTimestamp(ts);
  if (!d) return ts;

  const parts = compactDateTimeFormatter.formatToParts(d);
  const month = parts.find((part) => part.type === "month")?.value;
  const day = parts.find((part) => part.type === "day")?.value;
  const year = parts.find((part) => part.type === "year")?.value;
  const hour = parts.find((part) => part.type === "hour")?.value;
  const minute = parts.find((part) => part.type === "minute")?.value;
  const dayPeriod = parts.find((part) => part.type === "dayPeriod")?.value;

  if (!month || !day || !year || !hour || !minute || !dayPeriod) {
    return compactDateTimeFormatter.format(d).replace(", ", " ").replace(/ (\S+)$/, "\u00A0$1");
  }

  return `${month}/${day}/${year} ${hour}:${minute}\u00A0${dayPeriod}`;
}

/** Coarse "how long ago" label: "just now", "5m ago", "3d ago", "2mo ago". */
export function formatRelativeTime(ts: string): string {
  if (!ts) return "-";
  const d = parseTimestamp(ts);
  if (!d) return ts;

  const seconds = Math.floor((Date.now() - d.getTime()) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  if (days < 30) return `${Math.floor(days / 7)}w ago`;
  if (days < 365) return `${Math.floor(days / 30)}mo ago`;
  return `${Math.floor(days / 365)}y ago`;
}

export function formatDuration(seconds?: number | null): string {
  if (!seconds || seconds <= 0) return "-";
  const min = Math.floor(seconds / 60);
  const sec = seconds % 60;
  return `${min}m ${sec}s`;
}

export function formatGameFormat(format?: string | null): string {
  const raw = format?.trim();
  if (!raw) return "-";

  const label = raw
    .replace(/^Traditional(?=[A-Z_\s-]|$)/, "")
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/\s+/g, " ")
    .trim();
  return label || raw;
}

export function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

/** Abbreviate a macOS home-directory prefix: /Users/name/… → ~/… */
export function shortenHomePath(path: string): string {
  return path.replace(/^\/Users\/[^/]+(?=\/)/, "~");
}

/** Human-readable byte count: 1536 → "1.5 KB". */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 || value >= 10 ? Math.round(value).toString() : value.toFixed(1);
  return `${rounded} ${units[unit]}`;
}
