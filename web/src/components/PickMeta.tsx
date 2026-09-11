import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import type { Pick, Ratings } from "@/api/types";
import { compactNumber } from "@/lib/format";
import { imdbUrl, rottenTomatoesUrl } from "@/lib/links";
import { cn } from "@/lib/utils";

/** IMDb and Rotten Tomatoes links. `poster` sits on the dark details overlay, `surface` on a panel. */
export function TitleLinks({ pick, tone, className }: { pick: Pick; tone: "poster" | "surface"; className?: string }) {
  const look =
    tone === "poster"
      ? "h-6 bg-white/12 text-white/85 hover:bg-white/22 hover:text-white"
      : "h-7 border border-border bg-surface-raised text-text-muted hover:border-text-muted/50 hover:text-text";
  return (
    <p className={cn("flex flex-wrap gap-1.5", className)}>
      <ExternalLink href={imdbUrl(pick)} label={`Open ${pick.title} on IMDb`} className={look}>
        IMDb
      </ExternalLink>
      <ExternalLink href={rottenTomatoesUrl(pick)} label={`Search ${pick.title} on Rotten Tomatoes`} className={look}>
        Rotten Tomatoes
      </ExternalLink>
    </p>
  );
}

function ExternalLink({ href, label, className, children }: { href: string; label: string; className: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={label}
      title={label}
      className={cn(
        "relative inline-flex items-center gap-1 rounded-[6px] pr-1.5 pl-2 text-[11px] font-medium whitespace-nowrap transition-colors",
        // A taller invisible hit area for fingers, without changing the layout.
        "after:absolute after:inset-x-0 after:-inset-y-2 after:content-['']",
        className,
      )}
    >
      {children}
      <ArrowUpRight aria-hidden className="size-3 shrink-0 opacity-70" />
    </a>
  );
}

/** Compact IMDb / RT / MC chips. Renders nothing when no rating is known. */
export function RatingChips({ ratings, className }: { ratings?: Ratings; className?: string }) {
  const imdb = ratings?.imdb && ratings.imdb.value > 0 ? ratings.imdb : undefined;
  const rt = typeof ratings?.rotten_tomatoes === "number" ? ratings.rotten_tomatoes : undefined;
  const mc = typeof ratings?.metacritic === "number" ? ratings.metacritic : undefined;
  if (!imdb && rt === undefined && mc === undefined) return null;
  return (
    <ul className={cn("flex flex-wrap gap-1", className)} aria-label="Ratings">
      {imdb && (
        <Chip
          label="IMDb"
          name="IMDb"
          value={imdb.value.toFixed(1)}
          detail={imdb.votes > 0 ? `${compactNumber(imdb.votes)} votes` : undefined}
        />
      )}
      {rt !== undefined && <Chip label="RT" name="Rotten Tomatoes critic score" value={`${rt}%`} />}
      {mc !== undefined && <Chip label="MC" name="Metacritic" value={String(mc)} />}
    </ul>
  );
}

function Chip({ label, name, value, detail }: { label: string; name: string; value: string; detail?: string }) {
  return (
    <li
      title={`${name} ${value}${detail ? `, ${detail}` : ""}`}
      className="nums inline-flex items-baseline gap-1 rounded-full border border-border px-2 py-0.5 text-[11px] leading-tight text-text-muted"
    >
      <span aria-hidden>{label}</span>
      <span className="sr-only">{name}</span>
      <span className="font-semibold text-text">{value}</span>
      {detail && <span className="sr-only">, {detail}</span>}
    </li>
  );
}
