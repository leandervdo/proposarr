import {
  ArrowDownToLine,
  CalendarClock,
  Check,
  CircleAlert,
  CircleDashed,
  Clock,
  CloudOff,
  Download,
  EyeOff,
  Loader2,
  Search,
  type LucideIcon,
} from "lucide-react";
import { useId, useMemo, useState } from "react";
import { Link } from "react-router";
import { useOwned } from "@/api/queries";
import type { OwnedStatus, OwnedTitle, ReleaseCheck, Run } from "@/api/types";
import type { StatusTone } from "@/lib/bulkAdd";
import { appFor, appName, plural } from "@/lib/format";
import { canGet, isOnHold, ownedStatusLabel, shownSearch } from "@/lib/owned";
import { useTitleLink } from "@/lib/titleModal";
import { useSelection } from "@/lib/useSelection";
import { cn } from "@/lib/utils";
import { GetOwnedDialog, type GetOwnedRequest } from "./GetOwnedDialog";
import { Poster } from "./Poster";
import { SelectCheck, type CardSelection } from "./SelectCheck";
import { Button } from "./ui/button";

/** Columns per breakpoint. Collapsed, only the first row shows (pastFirstRow). */
const GRID = "grid grid-cols-3 gap-x-3 gap-y-6 sm:grid-cols-5 md:grid-cols-6 xl:grid-cols-8 2xl:grid-cols-10";

/** Hides the card at `index` where it falls past GRID's first row. */
function pastFirstRow(index: number): string {
  return cn(
    index >= 3 && "max-sm:hidden",
    index >= 5 && "sm:max-md:hidden",
    index >= 6 && "md:max-xl:hidden",
    index >= 8 && "xl:max-2xl:hidden",
    index >= 10 && "2xl:hidden",
  );
}

/** Hides "Show all" where `count` cards fit in GRID's first row. */
function fitsFirstRow(count: number): string {
  return cn(
    count <= 3 && "max-sm:hidden",
    count <= 5 && "sm:max-md:hidden",
    count <= 6 && "md:max-xl:hidden",
    count <= 8 && "xl:max-2xl:hidden",
    count <= 10 && "2xl:hidden",
  );
}

const TONE: Record<StatusTone, string> = {
  pending: "text-accent",
  success: "text-success",
  neutral: "text-text-muted",
  warning: "text-warning",
  danger: "text-danger",
};

const STATUS_ICON: Record<OwnedStatus, LucideIcon> = {
  downloaded: Check,
  downloading: ArrowDownToLine,
  missing: CircleDashed,
  unmonitored: EyeOff,
  unreleased: CalendarClock,
  unknown: CircleAlert,
  in_library: Check,
};

const SEARCH_ICON: Record<ReleaseCheck["status"], LucideIcon> = {
  checking: Loader2,
  grabbed: Check,
  pending: Clock,
  waiting: Clock,
  searching: Search,
  unavailable: CalendarClock,
  failed: CircleAlert,
};

/**
 * The library titles an open search matched, with their live state in Radarr/Sonarr. The search leaves them out of
 * its picks, so this says you already have them, and gets the movies that aren't on disk: all of them, or the ones
 * you select.
 */
export function OwnedPanel({ run, hasPicks }: { run: Run; hasPicks: boolean }) {
  const matches = run.owned ?? [];
  const owned = useOwned(run.id, matches.length > 0);
  const [expanded, setExpanded] = useState(false);
  const [getting, setGetting] = useState<GetOwnedRequest | null>(null);
  const id = useId();
  // Only movies Get can fetch take part in the selection, in the order they show.
  const selectable = useMemo(() => (owned.data ?? []).filter(canGet).map((t) => ({ id: t.tmdb_id, title: t })), [owned.data]);
  const selection = useSelection(selectable, String(run.id));
  const series = run.kind === "series";
  const count = owned.data?.length ?? matches.length;
  const gettable = selectable.map((s) => s.title);

  if (count === 0) return null;

  return (
    <section aria-labelledby={`${id}-heading`} className="mb-8 rounded-[var(--radius-panel)] border border-border bg-surface px-4 pt-4 pb-5 sm:px-5 sm:pt-5">
      <header className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 id={`${id}-heading`} className="font-display text-[26px] leading-none font-bold tracking-tight text-balance">
            You already have {series ? `${count} matching series` : plural(count, "matching movie")}
          </h2>
          <p className="mt-1.5 text-sm text-text-muted">
            {count === 1 ? "It's" : "They're"} left out of the picks{hasPicks ? " below" : ""}.
            {gettable.length > 1 && " Select the ones you want to get only those."}
          </p>
        </div>
        {selection.active ? (
          <div role="toolbar" aria-label="Selected movies" className="flex flex-wrap items-center gap-1.5 self-start">
            <p aria-live="polite" aria-atomic="true" className="flex items-baseline gap-1.5 pr-1.5 whitespace-nowrap">
              <span className="nums font-display text-[22px] leading-none font-bold text-accent">{selection.count}</span>
              <span className="text-sm font-medium">selected</span>
            </p>
            <Button
              variant="ghost"
              size="sm"
              disabled={selection.allSelected}
              onClick={() => {
                // Every selected movie should be visible.
                selection.selectAll();
                setExpanded(true);
              }}
            >
              Select all
              <span className="nums text-xs text-text-muted">{selection.total}</span>
            </Button>
            <Button variant="ghost" size="sm" onClick={selection.clear}>
              Clear
            </Button>
            <Button variant="primary" size="sm" onClick={() => setGetting({ titles: selection.selected.map((s) => s.title) })}>
              <Download /> Get {selection.count}
            </Button>
          </div>
        ) : (
          gettable.length > 0 && (
            <Button variant="primary" size="sm" className="self-start" onClick={() => setGetting({ titles: gettable })}>
              <Download /> Get {gettable.length} missing
            </Button>
          )
        )}
      </header>

      {owned.isPending ? (
        <div aria-busy aria-label={`Loading their state in ${appName(appFor(run.kind))}`} className={GRID}>
          {Array.from({ length: Math.min(count, 10) }, (_, i) => (
            <div key={i} className={pastFirstRow(i)}>
              <div className="aspect-[2/3] animate-pulse rounded-[var(--radius-poster)] bg-surface-raised" />
              <div className="mt-2 h-3.5 w-3/4 animate-pulse rounded bg-surface-raised" />
              <div className="mt-1.5 h-3 w-1/2 animate-pulse rounded bg-surface-raised" />
            </div>
          ))}
        </div>
      ) : owned.isError ? (
        <div role="status" className="flex items-start gap-2 text-[13px] leading-snug text-text-muted">
          <CloudOff aria-hidden className="mt-px size-4 shrink-0" />
          <p className="min-w-0">
            Couldn't read them from {appName(appFor(run.kind))}.{" "}
            <button type="button" onClick={() => void owned.refetch()} className="font-medium text-text underline decoration-border underline-offset-4 hover:decoration-text">
              Retry
            </button>
            <span className="mt-0.5 block text-xs break-words opacity-80">{owned.error.message}</span>
          </p>
        </div>
      ) : (
        <>
          <ul id={`${id}-list`} className={GRID}>
            {owned.data.map((t, i) => {
              const gettableCard = canGet(t);
              return (
                <li key={`${t.kind}:${t.tmdb_id}`} className={cn(!expanded && pastFirstRow(i))}>
                  <OwnedCard
                    title={t}
                    onGet={gettableCard && !selection.active ? () => setGetting({ titles: [t] }) : undefined}
                    selection={
                      gettableCard
                        ? { selected: selection.isSelected(t.tmdb_id), active: selection.active, onToggle: (range) => selection.toggle(t.tmdb_id, range) }
                        : undefined
                    }
                  />
                </li>
              );
            })}
          </ul>
          <Button
            variant="ghost"
            size="sm"
            aria-expanded={expanded}
            aria-controls={`${id}-list`}
            onClick={() => setExpanded((v) => !v)}
            className={cn("mt-4 -ml-3", fitsFirstRow(owned.data.length))}
          >
            {expanded ? "Show fewer" : `Show all ${owned.data.length}`}
          </Button>
        </>
      )}

      <GetOwnedDialog request={getting} onSubmitted={selection.clear} onClose={() => setGetting(null)} />
    </section>
  );
}

function OwnedCard({
  title,
  onGet,
  selection,
}: {
  title: OwnedTitle;
  onGet?: () => void;
  /** Movies Get can fetch: a checkbox on the corner, and clicks toggle while anything is selected. */
  selection?: CardSelection;
}) {
  const titleLink = useTitleLink();
  const link = titleLink({ kind: title.kind, tmdbId: title.tmdb_id });
  const label = ownedStatusLabel(title);
  const check = shownSearch(title);
  const onHold = isOnHold(title);
  const Icon = check ? SEARCH_ICON[check.status] : onHold ? Clock : STATUS_ICON[title.status];
  const progress = title.status === "downloading" && !onHold ? title.radarr?.queue?.progress : undefined;
  const selecting = selection?.active ?? false;

  return (
    // The checkbox and Get button sit beside the link rather than in it: a button inside a link is not allowed.
    <div className="group/select relative">
      {selection && <SelectCheck title={title.title} checked={selection.selected} shown={selecting} onToggle={selection.onToggle} className="ring-surface" />}
      <Link
        to={link.to}
        state={link.state}
        onMouseDown={(e) => {
          if (selection && e.shiftKey) e.preventDefault();
        }}
        onClick={(e) => {
          // While anything is selected a click toggles instead of opening; shift+click selects a range.
          if (!selection || e.metaKey || e.ctrlKey || e.altKey) return;
          if (!selecting && !e.shiftKey) return;
          e.preventDefault();
          selection.onToggle(e.shiftKey);
        }}
        onKeyDown={(e) => {
          if (selection && e.key === " ") {
            e.preventDefault();
            selection.onToggle(e.shiftKey);
          }
        }}
        className="group block rounded-[var(--radius-poster)] focus-visible:outline-offset-4"
      >
        <div className={cn("relative rounded-[var(--radius-poster)] ring-offset-[3px] ring-offset-surface", selection?.selected && "ring-2 ring-accent")}>
          <Poster src={title.poster_url} title={title.title} className="transition-shadow duration-300 group-hover:shadow-[0_14px_32px_-16px_rgb(0_0_0/0.7)]" />
          {progress !== undefined && (
            <div aria-hidden className="absolute inset-x-2 bottom-2 h-1 overflow-hidden rounded-full bg-black/50">
              <div className="h-full rounded-full bg-accent" style={{ width: `${Math.min(100, Math.max(0, progress))}%` }} />
            </div>
          )}
        </div>
        <p className="mt-2 px-0.5 text-[13px] leading-tight font-semibold">
          <span className="line-clamp-2 break-words decoration-text-muted/60 underline-offset-4 group-hover:underline">{title.title}</span>
          {title.year && <span className="nums text-xs font-normal text-text-muted">{title.year}</span>}
        </p>
        <p className="mt-1 flex items-start gap-1 px-0.5 text-xs leading-snug text-text-muted" title={label.description}>
          <Icon aria-hidden className={cn("mt-px size-3.5 shrink-0", TONE[label.tone], check?.status === "checking" && "animate-spin")} strokeWidth={2.25} />
          <span className="line-clamp-3 min-w-0 break-words">{label.title}</span>
        </p>
      </Link>
      {onGet && (
        <Button variant="onPoster" size="sm" onClick={onGet} aria-label={`Get ${title.title}`} className="absolute top-1.5 right-1.5 h-7 gap-1 px-2 text-xs">
          <Download /> Get
        </Button>
      )}
    </div>
  );
}
