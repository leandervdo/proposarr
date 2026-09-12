import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import type { Kind, Ratings } from "@/api/types";
import { compactNumber } from "@/lib/format";
import { imdbUrl, rottenTomatoesUrl, tmdbUrl } from "@/lib/links";
import { cn } from "@/lib/utils";

interface LinkableTitle {
  title: string;
  year?: number;
  imdb_id?: string;
}

const LINK_LOOK = {
  poster: "h-6 bg-white/12 text-white/85 hover:bg-white/22 hover:text-white",
  surface: "h-7 border border-border bg-surface-raised text-text-muted hover:border-text-muted/50 hover:text-text",
  panel: "h-9 gap-1.5 border border-border bg-surface-raised pr-2.5 pl-3 text-[13px] text-text hover:border-text-muted/60",
};

/**
 * IMDb and Rotten Tomatoes links, plus TMDB when `tmdb` is given. `poster` sits on the dark details overlay,
 * `surface` on a panel, `panel` is the larger size used in the title modal.
 */
export function TitleLinks({
  pick,
  tone,
  tmdb,
  className,
}: {
  pick: LinkableTitle;
  tone: keyof typeof LINK_LOOK;
  tmdb?: { kind: Kind; id: number };
  className?: string;
}) {
  const look = LINK_LOOK[tone];
  return (
    <p className={cn("flex flex-wrap gap-1.5", className)}>
      <ExternalLink href={imdbUrl(pick)} label={`Open ${pick.title} on IMDb`} className={look}>
        IMDb
      </ExternalLink>
      <ExternalLink href={rottenTomatoesUrl(pick)} label={`Search ${pick.title} on Rotten Tomatoes`} className={look}>
        Rotten Tomatoes
      </ExternalLink>
      {tmdb && (
        <ExternalLink href={tmdbUrl(tmdb.kind, tmdb.id)} label={`Open ${pick.title} on TMDB`} className={look}>
          TMDB
        </ExternalLink>
      )}
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

/** True when RatingChips would show anything. */
export function hasRatings(ratings?: Ratings, tmdbRating?: number): boolean {
  return (
    (!!ratings?.imdb && ratings.imdb.value > 0) ||
    typeof ratings?.rotten_tomatoes === "number" ||
    typeof ratings?.metacritic === "number" ||
    (tmdbRating ?? 0) > 0
  );
}

/**
 * IMDb / RT / MC chips, and TMDB when given. Compact by default; `detailed` spells out the names and shows
 * vote counts. Renders nothing when no rating is known.
 */
export function RatingChips({
  ratings,
  tmdb,
  detailed = false,
  className,
}: {
  ratings?: Ratings;
  tmdb?: { value?: number; votes?: number };
  detailed?: boolean;
  className?: string;
}) {
  const imdb = ratings?.imdb && ratings.imdb.value > 0 ? ratings.imdb : undefined;
  const rt = typeof ratings?.rotten_tomatoes === "number" ? ratings.rotten_tomatoes : undefined;
  const mc = typeof ratings?.metacritic === "number" ? ratings.metacritic : undefined;
  const tmdbValue = tmdb?.value && tmdb.value > 0 ? tmdb.value : undefined;
  if (!imdb && rt === undefined && mc === undefined && tmdbValue === undefined) return null;
  const votes = (n?: number) => (n && n > 0 ? `${compactNumber(n)} votes` : undefined);
  return (
    <ul className={cn("flex flex-wrap", detailed ? "gap-1.5" : "gap-1", className)} aria-label="Ratings">
      {imdb && <Chip detailed={detailed} label="IMDb" name="IMDb" value={imdb.value.toFixed(1)} detail={votes(imdb.votes)} />}
      {rt !== undefined && (
        <Chip detailed={detailed} label={detailed ? "Rotten Tomatoes" : "RT"} name="Rotten Tomatoes critic score" value={`${rt}%`} />
      )}
      {mc !== undefined && <Chip detailed={detailed} label={detailed ? "Metacritic" : "MC"} name="Metacritic" value={String(mc)} />}
      {tmdbValue !== undefined && (
        <Chip detailed={detailed} label="TMDB" name="TMDB user score" value={tmdbValue.toFixed(1)} detail={votes(tmdb?.votes)} />
      )}
    </ul>
  );
}

function Chip({ label, name, value, detail, detailed }: { label: string; name: string; value: string; detail?: string; detailed: boolean }) {
  return (
    <li
      title={`${name} ${value}${detail ? `, ${detail}` : ""}`}
      className={cn(
        "nums inline-flex items-baseline rounded-full border border-border leading-tight text-text-muted",
        detailed ? "gap-1.5 bg-surface-raised px-3 py-1.5 text-[13px]" : "gap-1 px-2 py-0.5 text-[11px]",
      )}
    >
      <span aria-hidden>{label}</span>
      <span className="sr-only">{name}</span>
      <span className="font-semibold text-text">{value}</span>
      {detail &&
        (detailed ? (
          <span>
            <span aria-hidden>· </span>
            <span className="sr-only">, </span>
            {detail}
          </span>
        ) : (
          <span className="sr-only">, {detail}</span>
        ))}
    </li>
  );
}
