import { AlertCircle, Check, Clock, CloudOff, EyeOff, Film, Plus, RotateCcw, Search, Tv } from "lucide-react";
import { useId, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useCachedLibraryTitle, usePick, useTitleDetails } from "@/api/queries";
import type { Kind, Pick, TitleDetails } from "@/api/types";
import { appFor, appName, dateLabel, plural, runtimeLabel, shortDate } from "@/lib/format";
import { ratingScore } from "@/lib/ratings";
import { useTitleModal, type TitleTarget } from "@/lib/titleModal";
import { cn } from "@/lib/utils";
import { AcceptDialog } from "./AcceptDialog";
import { hasRatings, RatingChips, TitleLinks } from "./PickMeta";
import { Poster } from "./Poster";
import { CastSkeleton, TitleCast } from "./TitleCast";
import { TitleTrailer, TrailerSkeleton } from "./TitleTrailer";
import { Button } from "./ui/button";
import { Dialog, DialogDetailContent, DialogTitle } from "./ui/dialog";
import { useVerdictActions } from "./useVerdictActions";

const HEADING = "font-display text-[24px] leading-none font-bold tracking-tight";

/** The title detail modal. Mounted once in the shell; opened by ?title=kind:tmdb_id[&pick=id]. */
export function TitleModal() {
  const { target, close } = useTitleModal();
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && close()}>
      {target && <TitleDialog key={`${target.kind}:${target.tmdbId}:${target.pickId ?? ""}`} target={target} />}
    </Dialog>
  );
}

function TitleDialog({ target }: { target: TitleTarget }) {
  const { kind, tmdbId } = target;
  const details = useTitleDetails(kind, tmdbId);
  const pickState = usePick(target.pickId, kind);
  // A stale or hand-edited link can pair a pick with another title; then the pick is left out.
  const pick = pickState.pick?.tmdb_id === tmdbId && pickState.pick.kind === kind ? pickState.pick : undefined;
  const libraryTitle = useCachedLibraryTitle(kind, tmdbId);
  const [accepting, setAccepting] = useState<Pick | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const id = useId();

  const d = details.data;
  const loading = details.isPending;
  const failed = details.isError;
  const notFound = failed && details.error instanceof ApiError && details.error.status === 404;

  const title = d?.title ?? pick?.title ?? libraryTitle?.title;
  const year = d?.year ?? pick?.year ?? libraryTitle?.year;
  const posterUrl = d?.poster_url ?? pick?.poster_url ?? libraryTitle?.poster_url;
  const genres = d?.genres ?? pick?.genres ?? libraryTitle?.genres ?? [];
  const overview = d?.overview ?? pick?.overview;
  // Live providers win; the pick's list is what was known when it was found.
  const streaming = d ? (d.streaming ?? []) : pick?.streaming;
  const ratings = d?.ratings ?? pick?.ratings;
  const imdbId = d?.imdb_id ?? pick?.imdb_id;
  const inLibrary = d ? d.in_library : !!libraryTitle;
  const directors = d?.directors ?? [];
  const cast = d?.cast ?? [];
  const series = kind === "series";

  const autoFocus = (e: Event) => {
    // Focus the scrolling body rather than the first button, so arrow keys scroll and nothing looks pressed.
    e.preventDefault();
    scrollRef.current?.focus({ preventScroll: true });
  };

  if (!title && failed) {
    return (
      <DialogDetailContent aria-describedby={undefined} onOpenAutoFocus={autoFocus} className="sm:w-[min(94vw,34rem)]">
        <div ref={scrollRef} tabIndex={-1} className="flex flex-col items-center px-6 pt-20 pb-14 text-center focus:outline-none">
          <div className="grid size-16 place-items-center rounded-full border border-border bg-surface-raised text-accent">
            <Film className="size-7" strokeWidth={1.75} />
          </div>
          <DialogTitle className="mt-6 text-[34px] font-bold">{notFound ? "Title not found" : "Could not load this title"}</DialogTitle>
          <p className="mt-3 max-w-md text-[15px] break-words text-text-muted">
            {notFound ? "TMDB does not know this title. The link may be out of date." : details.error.message}
          </p>
          {!notFound && (
            <Button className="mt-7" onClick={() => void details.refetch()}>
              Try again
            </Button>
          )}
        </div>
      </DialogDetailContent>
    );
  }

  const facts = [year ? String(year) : "", runtimeText(d, series), d?.status ?? ""].filter(Boolean);
  const episodes = series ? seasonText(d) : "";
  const showScoreBlock = !!pick || pickState.isPending || hasRatings(ratings, d?.tmdb_rating) || failed;

  return (
    <DialogDetailContent aria-describedby={undefined} onOpenAutoFocus={autoFocus}>
      <div ref={scrollRef} tabIndex={-1} className="min-h-0 flex-1 overflow-y-auto overscroll-contain focus:outline-none">
        <Backdrop url={d?.backdrop_url} fallbackUrl={loading ? undefined : posterUrl} loading={loading} />

        <header className="relative -mt-24 flex items-end gap-4 px-5 sm:-mt-36 sm:gap-7 sm:px-8">
          <div className="w-[6.75rem] shrink-0 sm:w-44">
            {title ? (
              <Poster src={posterUrl} title={title} className="shadow-[0_24px_48px_-16px_rgb(0_0_0/0.8)]" />
            ) : (
              <div className="aspect-[2/3] animate-pulse rounded-[var(--radius-poster)] bg-surface-raised" />
            )}
          </div>
          <div className="min-w-0 flex-1 pb-0.5">
            {title ? (
              <DialogTitle className="text-[34px] leading-[0.92] font-bold text-balance break-words sm:text-[54px] lg:text-[62px]">{title}</DialogTitle>
            ) : (
              <>
                <DialogTitle className="sr-only">Loading title</DialogTitle>
                <div aria-hidden className="h-9 w-3/4 animate-pulse rounded bg-surface-raised sm:h-14" />
              </>
            )}
            {facts.length > 0 ? (
              <p className="nums mt-3 flex flex-wrap items-center gap-x-2 text-[15px] text-text">
                {facts.map((f, i) => (
                  <span key={f} className="flex items-center gap-2">
                    {i > 0 && <span aria-hidden className="size-1 rounded-full bg-text-muted/60" />}
                    {f}
                  </span>
                ))}
              </p>
            ) : (
              loading && <div aria-hidden className="mt-3 h-4 w-40 animate-pulse rounded bg-surface-raised" />
            )}
            {episodes && <p className="nums mt-1 text-sm text-text-muted">{episodes}</p>}
            {genres.length > 0 && <p className="mt-1 text-sm text-text-muted">{genres.join(", ")}</p>}
          </div>
        </header>

        {d?.tagline && <p className="mt-5 px-5 text-[17px] leading-snug text-text-muted italic sm:px-8">{d.tagline}</p>}

        <div className="grid gap-x-10 gap-y-10 px-5 pt-8 pb-12 sm:px-8 lg:grid-cols-[minmax(0,1fr)_17rem] lg:grid-rows-[auto_auto_1fr]">
          {showScoreBlock && (
            <div className="flex flex-col gap-4 lg:col-start-2">
              {pick ? <ScoreBlock pick={pick} /> : pickState.isPending && <div aria-hidden className="h-12 w-48 animate-pulse rounded-[9px] bg-surface-raised" />}
              <RatingChips detailed ratings={ratings} tmdb={{ value: d?.tmdb_rating, votes: d?.tmdb_votes }} />
              {failed && <DetailsNote message={details.error.message} onRetry={() => void details.refetch()} />}
            </div>
          )}

          <div className="flex min-w-0 flex-col gap-11 lg:col-start-1 lg:row-span-3 lg:row-start-1">
            {pick ? (
              <WhyPicked pick={pick} headingId={`${id}-why`} />
            ) : (
              pickState.isPending && <TextSkeleton lines={3} />
            )}

            {overview || directors.length > 0 ? (
              <section aria-labelledby={`${id}-overview`}>
                <h3 id={`${id}-overview`} className={HEADING}>
                  Overview
                </h3>
                {overview && <p className="mt-3 max-w-[68ch] text-[15px] leading-relaxed text-text-muted">{overview}</p>}
                {directors.length > 0 && (
                  <p className="mt-4 text-sm">
                    <span className="text-text-muted">{series ? "Created by" : "Directed by"}</span>{" "}
                    <span className="font-medium">{directors.join(", ")}</span>
                  </p>
                )}
              </section>
            ) : (
              loading && <TextSkeleton lines={4} />
            )}

            {loading ? (
              <CastSkeleton />
            ) : (
              cast.length > 0 && (
                <TitleCast
                  cast={cast}
                  labelledBy={`${id}-cast`}
                  heading={
                    <h3 id={`${id}-cast`} className={HEADING}>
                      Cast
                    </h3>
                  }
                />
              )
            )}

            {loading ? (
              <TrailerSkeleton />
            ) : (
              d?.trailer &&
              title && (
                <section aria-label="Trailer">
                  <TitleTrailer trailer={d.trailer} backdropUrl={d.backdrop_url} title={title} />
                </section>
              )
            )}
          </div>

          <div className="flex flex-col gap-8 lg:col-start-2">
            <section aria-labelledby={`${id}-streaming`}>
              <h3 id={`${id}-streaming`} className={HEADING}>
                Streaming
              </h3>
              {streaming === undefined ? (
                loading ? (
                  <div aria-hidden className="mt-3 h-8 w-32 animate-pulse rounded-full bg-surface-raised" />
                ) : (
                  <p className="mt-3 text-sm text-text-muted">Unknown</p>
                )
              ) : streaming.length > 0 ? (
                <ul className="mt-3 flex flex-wrap gap-1.5">
                  {streaming.map((s) => (
                    <li key={s} className="inline-flex h-8 items-center gap-1.5 rounded-full border border-border bg-surface-raised px-3 text-[13px] font-medium">
                      <Tv aria-hidden className="size-3.5 text-text-muted" />
                      {s}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="mt-3 text-sm text-text-muted">Not on a streaming service here.</p>
              )}
            </section>
            {title && (
              <section aria-labelledby={`${id}-links`}>
                <h3 id={`${id}-links`} className={HEADING}>
                  Links
                </h3>
                <TitleLinks className="mt-3" tone="panel" pick={{ title, year, imdb_id: imdbId }} tmdb={{ kind, id: tmdbId }} />
              </section>
            )}
          </div>
        </div>
      </div>

      <ActionBar kind={kind} pick={pick} inLibrary={inLibrary} known={!!d || !!pick || !!libraryTitle} onAdd={setAccepting} />
      <AcceptDialog pick={accepting} onClose={() => setAccepting(null)} />
    </DialogDetailContent>
  );
}

function runtimeText(d: TitleDetails | undefined, series: boolean): string {
  if (!d?.runtime) return "";
  return series ? `~${d.runtime} min episodes` : runtimeLabel(d.runtime);
}

function seasonText(d: TitleDetails | undefined): string {
  if (!d) return "";
  return [d.seasons ? plural(d.seasons, "season") : "", d.episodes ? plural(d.episodes, "episode") : ""].filter(Boolean).join(" · ");
}

function Backdrop({ url, fallbackUrl, loading }: { url?: string; fallbackUrl?: string; loading: boolean }) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const src = url && failedSrc !== url ? url : undefined;
  // Into the surface colour at the bottom, so the title below reads in both themes.
  const fade: CSSProperties = {
    background:
      "linear-gradient(to top, var(--surface) 0%, color-mix(in oklab, var(--surface) 82%, transparent) 30%, color-mix(in oklab, var(--surface) 30%, transparent) 64%, transparent 100%)",
  };
  return (
    <div aria-hidden className="relative h-60 overflow-hidden bg-surface-raised sm:h-[21rem] lg:h-[24rem]">
      {src ? (
        <img src={src} alt="" decoding="async" onError={() => setFailedSrc(src)} className="absolute inset-0 size-full object-cover object-[50%_25%]" />
      ) : fallbackUrl ? (
        <img src={fallbackUrl} alt="" decoding="async" className="absolute inset-0 size-full scale-125 object-cover opacity-50 blur-2xl" />
      ) : (
        <div className={cn("absolute inset-0 bg-surface-raised", loading && "animate-pulse")} />
      )}
      <div className="absolute inset-0" style={fade} />
      {/* Keeps the close button legible on bright artwork. */}
      <div className="absolute inset-x-0 top-0 h-24 bg-gradient-to-b from-black/40 to-transparent" />
    </div>
  );
}

function ScoreBlock({ pick }: { pick: Pick }) {
  const fromRatings = pick.run_use_taste === false && ratingScore(pick.ratings) !== undefined;
  const label = fromRatings ? "Score from IMDb and Rotten Tomatoes" : "Claude's match score";
  const high = pick.score >= 90;
  return (
    <div className="flex items-center gap-3">
      <span
        role="img"
        aria-label={`${label}: ${pick.score} of 100`}
        className={cn(
          "grid h-12 min-w-12 shrink-0 place-items-center rounded-[9px] px-2 font-display text-[30px] leading-none font-bold",
          high ? "bg-accent text-accent-contrast" : "border border-border bg-surface-raised text-text",
        )}
      >
        <span className="nums">{pick.score}</span>
      </span>
      <p aria-hidden className="text-sm leading-snug text-text-muted">
        {label}
      </p>
    </div>
  );
}

function WhyPicked({ pick, headingId }: { pick: Pick; headingId: string }) {
  const openSearch = pick.run_use_taste === false;
  const related = pick.related_to ?? [];
  const found = dateLabel(pick.found_at);
  return (
    <section aria-labelledby={headingId}>
      <h3 id={headingId} className={HEADING}>
        Why Claude picked it
      </h3>
      <p className="mt-4 max-w-[62ch] border-l-2 border-accent pl-4 text-[17px] leading-relaxed text-text sm:text-lg">{pick.reason}</p>
      {openSearch ? (
        <p className="mt-4 flex items-start gap-2 text-sm text-text-muted">
          <Search aria-hidden className="mt-0.5 size-4 shrink-0 text-accent" />
          <span>
            Found by your search <span className="text-text">“{pick.run_vibe}”</span>
            {found && (
              <>
                {" "}
                on <time dateTime={pick.found_at}>{found}</time>
              </>
            )}
          </span>
        </p>
      ) : (
        related.length > 0 && (
          <div className="mt-5">
            <p className="text-xs font-medium text-text-muted">Related to your library</p>
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {related.map((t) => {
                const m = /^(.*)\s\((\d{4})\)$/.exec(t);
                return (
                  <li key={t} className="inline-flex items-baseline gap-1.5 rounded-full border border-border bg-surface-raised px-3 py-1 text-[13px]">
                    {m ? m[1] : t}
                    {m && <span className="nums text-xs text-text-muted">{m[2]}</span>}
                  </li>
                );
              })}
            </ul>
          </div>
        )
      )}
      {pick.source === "free" && !openSearch && <p className="mt-3 text-xs text-text-muted">Claude picked this outside the TMDB candidate list.</p>}
    </section>
  );
}

function DetailsNote({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div role="status" className="flex items-start gap-2 text-[13px] leading-snug text-text-muted">
      <CloudOff aria-hidden className="mt-px size-4 shrink-0" />
      <p className="min-w-0">
        Could not load the full details from TMDB.{" "}
        <button type="button" onClick={onRetry} className="font-medium text-text underline decoration-border underline-offset-4 hover:decoration-text">
          Try again
        </button>
        <span className="mt-0.5 block text-xs break-words opacity-80">{message}</span>
      </p>
    </div>
  );
}

function TextSkeleton({ lines }: { lines: number }) {
  return (
    <div aria-hidden>
      <div className="h-6 w-44 animate-pulse rounded bg-surface-raised" />
      <div className="mt-4 flex flex-col gap-2">
        {Array.from({ length: lines }, (_, i) => (
          <div key={i} className={cn("h-3.5 animate-pulse rounded bg-surface-raised", i === lines - 1 ? "w-2/3" : "w-full")} />
        ))}
      </div>
    </div>
  );
}

function ActionBar({
  kind,
  pick,
  inLibrary,
  known,
  onAdd,
}: {
  kind: Kind;
  pick?: Pick;
  inLibrary: boolean;
  /** Whether in_library is known yet. */
  known: boolean;
  onAdd: (pick: Pick) => void;
}) {
  const { decide } = useVerdictActions();
  const app = appName(appFor(kind));
  const request = pick?.request;
  const added = request?.status === "added";
  if (!pick && !(known && inLibrary)) return null;

  let status: ReactNode = null;
  if (added && request) {
    status = (
      <>
        <Check aria-hidden className="mt-0.5 size-4 shrink-0 text-success" />
        <span className="min-w-0">
          <span className="font-semibold">In {appName(request.app)}</span>
          <span className="text-text-muted">
            {[request.quality_profile, request.root_folder, dateLabel(request.requested_at)].filter(Boolean).map((part) => (
              <span key={part}> · {part}</span>
            ))}
          </span>
        </span>
      </>
    );
  } else if (inLibrary) {
    status = (
      <>
        <Check aria-hidden className="mt-0.5 size-4 shrink-0 text-success" />
        <span className="font-semibold">Already in your library</span>
      </>
    );
  } else if (request?.status === "failed") {
    status = (
      <>
        <AlertCircle aria-hidden className="mt-0.5 size-4 shrink-0 text-danger" />
        <span className="min-w-0 break-words">
          Adding to {app} failed{request.error ? `: ${request.error}` : ""}
        </span>
      </>
    );
  } else if (pick?.verdict === "later") {
    const until = shortDate(pick.later_until);
    status = (
      <>
        <Clock aria-hidden className="mt-0.5 size-4 shrink-0 text-text-muted" />
        {until ? `Saved for later, back on ${until}` : "Saved for later"}
      </>
    );
  } else if (pick?.verdict === "ignored") {
    status = (
      <>
        <EyeOff aria-hidden className="mt-0.5 size-4 shrink-0 text-text-muted" />
        Ignored
      </>
    );
  } else if (pick?.verdict === "accepted") {
    status = (
      <>
        <Check aria-hidden className="mt-0.5 size-4 shrink-0 text-text-muted" />
        Accepted, not added yet
      </>
    );
  }

  return (
    <footer className="flex flex-col gap-3 border-t border-border bg-surface-raised px-5 pt-3.5 pb-[max(0.875rem,env(safe-area-inset-bottom))] sm:min-h-[4.5rem] sm:flex-row sm:items-center sm:gap-6 sm:px-8 sm:pb-3.5">
      {status && (
        <p className="flex min-w-0 items-start gap-2 text-sm" aria-live="polite">
          {status}
        </p>
      )}
      {pick && !added && (
        <div className="flex gap-2 sm:ml-auto">
          {(pick.verdict === "later" || pick.verdict === "ignored") && (
            <Button variant="ghost" onClick={() => decide(pick, "")} aria-label={`Move ${pick.title} back to undecided`}>
              <RotateCcw /> Move back
            </Button>
          )}
          {pick.verdict !== "later" && (
            <Button onClick={() => decide(pick, "later")} aria-label={`Save ${pick.title} for later`}>
              <Clock /> Later
            </Button>
          )}
          {pick.verdict !== "ignored" && (
            <Button onClick={() => decide(pick, "ignored")} aria-label={`Ignore ${pick.title}`}>
              <EyeOff /> Ignore
            </Button>
          )}
          {!inLibrary && (
            <Button variant="primary" className="min-w-0 flex-1 sm:flex-none" onClick={() => onAdd(pick)}>
              <Plus /> Add<span className="hidden sm:inline"> to {app}</span>
            </Button>
          )}
        </div>
      )}
      {/* Radarr grabbed nothing but another profile would: the accept dialog opens on that choice. */}
      {pick && added && request?.release?.status === "waiting" && request.release.alternatives.length > 0 && (
        <Button variant="primary" className="sm:ml-auto" onClick={() => onAdd(pick)}>
          Switch profile
        </Button>
      )}
    </footer>
  );
}
