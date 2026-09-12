import { CalendarClock, Eye } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useAppOptions, useCachedOwnedMovies, useConfig, useSearchOwnedMovie } from "@/api/queries";
import type { IfNothingFits, OwnedTitle, ProfileOption, QualityProfile, ReleaseCheck } from "@/api/types";
import { bulkSummary, effectiveFallback, fallbackHint, fallbackTargets, type RowOutcome, type StatusLabel } from "@/lib/bulkAdd";
import { mapLimit } from "@/lib/concurrency";
import { plural } from "@/lib/format";
import { searchLabel } from "@/lib/owned";
import { awaitRelease, ownedReleaseKey, takeAwaitedRelease, useReleaseShown } from "@/lib/releases";
import { useMounted } from "@/lib/useMounted";
import { BatchControls, PosterStack, RowControls, StatusIcon, SwitchControl, type RowChoice } from "./BulkAddDialog";
import { Poster } from "./Poster";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

const CONCURRENCY = 3;

/** The bulk add's summary, in words for searches. */
const SUMMARY_WORDS: Partial<Record<RowOutcome, string>> = { added: "started", adding: "starting", exists: "skipped" };

export interface GetOwnedRequest {
  /** Movies in Radarr that aren't on disk, in the order the panel shows them. */
  titles: OwnedTitle[];
}

/**
 * Gets movies you own but don't have on disk, like the bulk add: every movie has its own quality profile, starting
 * on the one it has in Radarr, and "if nothing fits" choice. Proposarr monitors each when needed, sets the profile and
 * starts Radarr's search; after submitting, each row follows its movie live.
 */
export function GetOwnedDialog({ request, onClose }: { request: GetOwnedRequest | null; onClose: () => void }) {
  return (
    <Dialog open={request !== null} onOpenChange={(open) => !open && onClose()}>
      {/* Mounted per opening, so every movie starts on its current profile. */}
      {request && <GetOwnedFlow titles={request.titles} onClose={onClose} />}
    </Dialog>
  );
}

type RowPhase =
  | { phase: "queued" }
  | { phase: "starting" }
  | { phase: "started"; title: OwnedTitle }
  | { phase: "conflict"; message: string }
  | { phase: "gone"; message: string }
  | { phase: "error"; message: string };

function GetOwnedFlow({ titles, onClose }: GetOwnedRequest & { onClose: () => void }) {
  const options = useAppOptions("radarr", true);
  const config = useConfig();
  const searchMovie = useSearchOwnedMovie();
  const mounted = useMounted();
  const descriptionId = useId();
  const live = useCachedOwnedMovies(titles.map((t) => t.tmdb_id));
  const [choices, setChoices] = useState<Record<number, RowChoice>>({});
  const [submitted, setSubmitted] = useState(false);
  const [phases, setPhases] = useState<Record<number, RowPhase>>({});

  const profiles = options.data?.quality_profiles ?? [];
  // The config too: the profile order decides whether a row can fall back.
  const loading = options.isPending || config.isPending;
  const profileOrder = config.data?.radarr?.profile_order;

  // A row starts on the movie's profile in Radarr, when Radarr still has it. It shows in the row and can be changed.
  const initialChoice = (t: OwnedTitle): RowChoice => {
    const id = t.radarr?.quality_profile_id;
    return { profileId: profiles.some((p) => p.id === id) ? String(id) : "" };
  };
  const choiceFor = (t: OwnedTitle): RowChoice => choices[t.tmdb_id] ?? initialChoice(t);
  const targetsOf = (choice: RowChoice) => fallbackTargets(profileOrder, Number(choice.profileId) || 0, profiles);
  const fitsFor = (t: OwnedTitle) => effectiveFallback(choiceFor(t).ifNothingFits, targetsOf(choiceFor(t)));

  const missing = titles.filter((t) => choiceFor(t).profileId === "").length;
  const canSubmit = !submitted && !loading && profiles.length > 0 && missing === 0;

  // The batch controls show a value only while the rows they fill agree on it.
  const rowProfiles = titles.map((t) => choiceFor(t).profileId);
  const allProfile = rowProfiles[0] && rowProfiles.every((v) => v === rowProfiles[0]) ? rowProfiles[0] : "";
  const fallbackRows = titles.filter((t) => targetsOf(choiceFor(t)).available);
  const fallbackFits = fallbackRows.map((t) => fitsFor(t));
  const allFits: IfNothingFits | "" =
    fallbackRows.length === 0 ? "wait" : fallbackFits.every((v) => v === fallbackFits[0]) ? fallbackFits[0]! : "";
  // One hint above the rows when they all have the same one, otherwise one per row.
  const hints = titles.map((t) => fallbackHint(targetsOf(choiceFor(t))));
  const sharedHint = hints.every((h) => h === hints[0]) ? hints[0] : undefined;

  const setRow = (t: OwnedTitle, patch: Partial<RowChoice>) =>
    setChoices((prev) => ({ ...prev, [t.tmdb_id]: { ...(prev[t.tmdb_id] ?? initialChoice(t)), ...patch } }));
  const setAllProfiles = (profileId: string) =>
    setChoices((prev) => Object.fromEntries(titles.map((t) => [t.tmdb_id, { ...prev[t.tmdb_id], profileId }])));
  // Only rows that can fall back take the batch choice; the rest keep waiting.
  const setAllFits = (ifNothingFits: IfNothingFits) =>
    setChoices((prev) =>
      Object.fromEntries(
        titles.map((t) => {
          const row = prev[t.tmdb_id] ?? initialChoice(t);
          return [t.tmdb_id, targetsOf(row).available ? { ...row, ifNothingFits } : row];
        }),
      ),
    );
  const setPhase = (id: number, phase: RowPhase) => setPhases((prev) => ({ ...prev, [id]: phase }));

  const submit = async () => {
    if (!canSubmit) return;
    const rows = titles.map((t) => ({ t, profileId: Number(choiceFor(t).profileId), fits: fitsFor(t) }));
    setSubmitted(true);
    setPhases(Object.fromEntries(titles.map((t) => [t.tmdb_id, { phase: "queued" }])));

    const results = await mapLimit(rows, CONCURRENCY, async ({ t, profileId, fits }) => {
      setPhase(t.tmdb_id, { phase: "starting" });
      try {
        const updated = await searchMovie.mutateAsync({ tmdbId: t.tmdb_id, qualityProfileId: profileId, ifNothingFits: fits });
        // The row reports the result while the dialog is open; otherwise a toast does when owned.updated arrives.
        if (updated.search?.release.status === "checking") awaitRelease(ownedReleaseKey(t.tmdb_id), t.title);
        setPhase(t.tmdb_id, { phase: "started", title: updated });
      } catch (err) {
        setPhase(t.tmdb_id, phaseForError(err));
        throw err;
      }
    });

    // Closed before every search had started: say how it went, as the bulk add does.
    if (mounted.current) return;
    const started = results.filter((r) => r.status === "fulfilled").length;
    const skipped = results.filter((r) => rejectedWith(r, 409)).length;
    const errors = results
      .map((r, i) => (r.status === "rejected" && !rejectedWith(r, 409) ? `${titles[i]!.title}: ${(r.reason as Error).message}` : ""))
      .filter(Boolean);
    const details = [skipped > 0 ? `${skipped} already on disk or being searched` : "", ...errors].filter(Boolean).join(" · ");
    if (errors.length > 0) toast.error(`Radarr is searching for ${started} of ${plural(titles.length, "movie")}`, { description: details });
    else toast.success(`Radarr is searching for ${plural(started, "movie")}`, { description: details || undefined });
  };

  const outcomes = titles.map((t, i) => outcomeOf(phases[t.tmdb_id], live[i])).filter((o): o is RowOutcome => o !== undefined);
  const footer = submitted
    ? bulkSummary(outcomes, SUMMARY_WORDS)
    : loading
      ? "Loading Radarr quality profiles…"
      : profiles.length === 0
        ? ""
        : missing > 0
          ? `${plural(missing, "movie")} still ${missing === 1 ? "needs" : "need"} a quality profile`
          : "Every movie has a quality profile";
  const one = titles.length === 1;

  return (
    <DialogContent
      aria-describedby={descriptionId}
      onOpenAutoFocus={(e) => e.preventDefault()}
      // Full screen on phones, a wide panel from sm up.
      className="h-dvh max-h-dvh rounded-none border-0 sm:h-auto sm:max-h-[min(92dvh,60rem)] sm:w-[min(94vw,52rem)] sm:rounded-[var(--radius-panel)] sm:border"
    >
      <form
        className="flex min-h-0 flex-1 flex-col"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <header className="flex items-end gap-4 border-b border-border px-5 pt-[max(1.25rem,env(safe-area-inset-top))] pr-14 pb-4 sm:pt-5">
          <PosterStack titles={titles} />
          <div className="min-w-0">
            <DialogTitle className="text-balance">{one ? `Get ${titles[0]!.title}` : `Get ${titles.length} movies`}</DialogTitle>
            <DialogDescription id={descriptionId} className="mt-2">
              {submitted
                ? `${one ? "Radarr is searching." : "Sent to Radarr one by one."} You can close this; Proposarr tells you what Radarr finds.`
                : one
                  ? "It's in Radarr but not on disk. Choose the quality profile Radarr searches with; it starts on the one the movie has now."
                  : "They're in Radarr but not on disk. Choose the quality profile Radarr searches with for each; every movie starts on the one it has now."}
            </DialogDescription>
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {!submitted && (
            <BatchControls
              name="Radarr"
              movie
              options={options.data}
              loading={loading}
              error={options.isError ? options.error.message : null}
              onRetry={() => void options.refetch()}
              allProfile={allProfile}
              onAllProfile={setAllProfiles}
              allFits={allFits}
              canFallBack={fallbackRows.length > 0}
              hint={sharedHint}
              onAllFits={setAllFits}
              needsFolder={false}
              rootFolder=""
              onRootFolder={() => {}}
            />
          )}

          <ul aria-label="Movies to get" className="divide-y divide-border">
            {titles.map((t, i) => {
              const phase = phases[t.tmdb_id];
              const current = live[i] ?? (phase?.phase === "started" ? phase.title : t);
              const choice = choiceFor(t);
              return (
                <li key={t.tmdb_id} className="grid grid-cols-[3rem_minmax(0,1fr)] gap-x-4 gap-y-3 px-5 py-4 sm:grid-cols-[3.5rem_minmax(0,1fr)]">
                  <Poster src={t.poster_url} title={t.title} className="row-span-2 w-12 self-start rounded-md sm:w-14" />
                  <div className="min-w-0">
                    <p className="flex min-w-0 items-baseline gap-2">
                      <span className="truncate text-[15px] leading-tight font-semibold">{t.title}</span>
                      {t.year && <span className="nums shrink-0 text-[13px] text-text-muted">{t.year}</span>}
                    </p>
                    {!phase && <RowNote title={t} />}
                  </div>
                  <div className="col-start-2 min-w-0">
                    {phase ? (
                      <RowStatus title={current} phase={phase} />
                    ) : (
                      <RowControls
                        title={t.title}
                        movie
                        options={options.data}
                        loading={loading}
                        choice={choice}
                        fits={fitsFor(t)}
                        canFallBack={targetsOf(choice).available}
                        hint={sharedHint === undefined ? hints[i] : undefined}
                        profileHint={profileHint(t, choice, profiles)}
                        onChange={(patch) => setRow(t, patch)}
                      />
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>

        <footer className="flex flex-wrap items-center gap-x-4 gap-y-3 border-t border-border bg-surface-raised px-5 pt-3.5 pb-[max(0.875rem,env(safe-area-inset-bottom))] sm:flex-nowrap sm:pb-3.5">
          <p aria-live="polite" className="nums min-w-0 basis-full text-sm text-text-muted sm:flex-1 sm:basis-auto">
            {footer}
          </p>
          <div className="flex w-full gap-2 sm:w-auto">
            {submitted ? (
              <Button variant="primary" className="flex-1 sm:flex-none" onClick={onClose}>
                Done
              </Button>
            ) : (
              <>
                <Button variant="ghost" className="flex-1 sm:flex-none" onClick={onClose}>
                  Cancel
                </Button>
                <Button type="submit" variant={canSubmit ? "primary" : "secondary"} disabled={!canSubmit} className="flex-[2] sm:flex-none">
                  {one ? "Search in Radarr" : `Search ${titles.length} in Radarr`}
                </Button>
              </>
            )}
          </div>
        </footer>
      </form>
    </DialogContent>
  );
}

function rejectedWith(result: PromiseSettledResult<unknown>, status: number): boolean {
  return result.status === "rejected" && result.reason instanceof ApiError && result.reason.status === status;
}

/** 409: on disk already, or a search Proposarr started is still running. 404: no longer in Radarr. */
function phaseForError(err: unknown): RowPhase {
  const message = (err as Error).message;
  if (err instanceof ApiError && err.status === 409) return { phase: "conflict", message };
  if (err instanceof ApiError && err.status === 404) return { phase: "gone", message };
  return { phase: "error", message };
}

/** Where a row stands, for the summary. Undefined before submitting. */
function outcomeOf(phase: RowPhase | undefined, liveTitle: OwnedTitle | undefined): RowOutcome | undefined {
  switch (phase?.phase) {
    case undefined:
      return undefined;
    case "queued":
      return "queued";
    case "starting":
      return "adding";
    case "conflict":
      return "exists";
    case "gone":
    case "error":
      return "error";
    case "started":
      return (liveTitle?.search ?? phase.title.search)?.release.status ?? "added";
  }
}

/** "Its profile in Radarr now", or what choosing another one changes. */
function profileHint(t: OwnedTitle, choice: RowChoice, profiles: QualityProfile[]): string | undefined {
  // The name is empty when the server couldn't read Radarr's profiles.
  const current = t.radarr?.quality_profile || profiles.find((p) => p.id === t.radarr?.quality_profile_id)?.name;
  if (!current) return undefined;
  if (choice.profileId === "") return `${current} in Radarr now`;
  return Number(choice.profileId) === t.radarr?.quality_profile_id ? "Its profile in Radarr now" : `Changes it from ${current}`;
}

/** What Proposarr does besides searching, before submitting. */
function RowNote({ title }: { title: OwnedTitle }) {
  const note =
    title.status === "unmonitored"
      ? { icon: Eye, text: "Radarr will monitor it" }
      : title.status === "unreleased"
        ? { icon: CalendarClock, text: "Not released yet: Radarr grabs it once it's available" }
        : null;
  if (!note) return <p className="mt-1 text-[13px] text-text-muted">Missing</p>;
  const Icon = note.icon;
  return (
    <p className="mt-1 flex items-start gap-1.5 text-[13px] leading-snug text-text-muted">
      <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      {note.text}
    </p>
  );
}

/** A submitted row, following its movie from the cache as owned.updated events come in. */
function RowStatus({ title, phase }: { title: OwnedTitle; phase: RowPhase }) {
  const key = ownedReleaseKey(title.tmdb_id);
  const check = phase.phase === "started" ? (title.search ?? phase.title.search)?.release : undefined;
  const settled = !!check && check.status !== "checking";
  const searchMovie = useSearchOwnedMovie();
  // While the row shows the search, it reports the result instead of a toast.
  useReleaseShown(key);
  useEffect(() => {
    if (settled) takeAwaitedRelease(key);
  }, [settled, key]);

  const label = statusLabel(phase, check);
  const canSwitch = check?.status === "waiting" && check.alternatives.length > 0;

  // Moves the movie to that profile and searches again, once: it doesn't switch on its own after this.
  const switchTo = async (profile: ProfileOption) => {
    const updated = await searchMovie.mutateAsync({ tmdbId: title.tmdb_id, qualityProfileId: profile.id, ifNothingFits: "wait" });
    if (updated.search?.release.status === "checking") awaitRelease(key, title.title);
  };

  return (
    <div className="flex items-start gap-2.5 text-sm">
      <StatusIcon muted={phase.phase === "conflict"} release={check} tone={label.tone} />
      <div className="min-w-0 flex-1">
        <p className="font-medium break-words">{label.title}</p>
        {label.description && (
          <p className="mt-0.5 truncate text-[13px] text-text-muted" title={label.description}>
            {label.description}
          </p>
        )}
        {check?.status === "waiting" && check.qualities.length > 0 && (
          <p className="nums mt-1 truncate text-xs text-text-muted">{check.qualities.map((q) => `${q.count} × ${q.quality}`).join(" · ")}</p>
        )}
        {check && canSwitch && <SwitchControl title={title.title} check={check} onSwitch={switchTo} />}
      </div>
    </div>
  );
}

function statusLabel(phase: RowPhase, check: ReleaseCheck | undefined): StatusLabel {
  switch (phase.phase) {
    case "queued":
      return { tone: "neutral", title: "Queued" };
    case "starting":
      return { tone: "pending", title: "Starting Radarr's search…" };
    case "conflict":
      return { tone: "neutral", title: "Already on disk or being searched", description: phase.message };
    case "gone":
      return { tone: "danger", title: "No longer in Radarr", description: phase.message };
    case "error":
      return { tone: "danger", title: phase.message };
    case "started":
      return check ? searchLabel(check) : { tone: "success", title: "Search started" };
  }
}
