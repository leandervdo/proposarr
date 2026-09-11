import type { App, Kind, Run } from "@/api/types";

/** A run driven only by its description, not the library or history. Older servers omit use_taste. */
export function isOpenSearch(run?: Pick<Run, "use_taste">): boolean {
  return run?.use_taste === false;
}

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });

/** 2275363 → "2.3M" */
export function compactNumber(n: number): string {
  return compact.format(n);
}

const ZERO_TIME = "0001-01-01";

/** Go zero times mean unknown. */
export function knownDate(iso?: string): Date | null {
  if (!iso || iso.startsWith(ZERO_TIME)) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

export function relativeTime(iso?: string, now = Date.now()): string {
  const d = knownDate(iso);
  if (!d) return "unknown";
  const diff = (d.getTime() - now) / 1000;
  const abs = Math.abs(diff);
  if (abs < 45) return "just now";
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), "day");
  if (abs < 86400 * 365) return rtf.format(Math.round(diff / (86400 * 30)), "month");
  return rtf.format(Math.round(diff / (86400 * 365)), "year");
}

export function shortDate(iso?: string): string {
  const d = knownDate(iso);
  if (!d) return "";
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

export function duration(startIso?: string, endIso?: string, now = Date.now()): string {
  const start = knownDate(startIso);
  if (!start) return "";
  const end = knownDate(endIso)?.getTime() ?? now;
  const s = Math.max(0, Math.round((end - start.getTime()) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
}

export function cost(usd: number): string {
  if (!usd) return "$0";
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}

export function tokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

export function gigabytes(bytes: number): string {
  if (!bytes) return "";
  const gb = bytes / 1024 ** 3;
  return gb >= 1024 ? `${(gb / 1024).toFixed(1)} TB free` : `${Math.round(gb)} GB free`;
}

export function appFor(kind: Kind): App {
  return kind === "series" ? "sonarr" : "radarr";
}

export function appName(app: App): string {
  return app === "sonarr" ? "Sonarr" : "Radarr";
}

export function kindLabel(kind: Kind, plural = true): string {
  if (kind === "series") return plural ? "Series" : "Series";
  return plural ? "Movies" : "Movie";
}

export function initials(title: string): string {
  const words = title
    .replace(/^(the|a|an)\s+/i, "")
    .split(/[\s:–-]+/)
    .filter(Boolean);
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join("");
}
