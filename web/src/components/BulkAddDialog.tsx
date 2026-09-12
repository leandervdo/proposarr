import * as SelectPrimitive from "@radix-ui/react-select";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircle, CalendarClock, Check, CircleAlert, Clock, HardDrive, Info, Loader2, Search } from "lucide-react";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useAppOptions, useCachedPicks, useConfig, useRequestPick, useSwitchProfile } from "@/api/queries";
import type { AppOptions, IfNothingFits, Kind, Pick, ReleaseCheck } from "@/api/types";
import {
  bulkSummary,
  effectiveFallback,
  fallbackHint,
  fallbackTargets,
  IF_NOTHING_FITS_LEGEND,
  IF_NOTHING_FITS_OPTIONS,
  releaseLabel,
  UNRANKED_HINT,
  type RowOutcome,
  type StatusLabel,
} from "@/lib/bulkAdd";
import { mapLimit } from "@/lib/concurrency";
import { appFor, appName, fileSize, gigabytes, plural } from "@/lib/format";
import { awaitRelease, takeAwaitedRelease, useReleaseShown } from "@/lib/releases";
import { cn } from "@/lib/utils";
import { RatingChips } from "./PickMeta";
import { Poster } from "./Poster";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";
import { Select, SelectItem } from "./ui/select";

const CONCURRENCY = 3;

/** The rows' two controls and the batch fields above them share these columns. */
const CONTROL_COLUMNS = "grid gap-2 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]";

export interface BulkAddRequest {
  kind: Kind;
  /** The titles to add, in the order they appear on the page. */
  picks: Pick[];
  /** Selected titles left out because they were already added. */
  skipped: number;
}

/**
 * Adds several titles to Radarr/Sonarr at once. Every title needs its own quality profile; "Set all to…" fills
 * them in one go. After submitting, each row follows its pick live.
 */
export function BulkAddDialog({
  request,
  onSubmitted,
  onClose,
}: {
  request: BulkAddRequest | null;
  /** The titles were sent; closing from then on should clear the selection. */
  onSubmitted: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={request !== null} onOpenChange={(open) => !open && onClose()}>
      {/* Mounted per opening, so every title starts without a profile. */}
      {request && <BulkAddFlow {...request} onSubmitted={onSubmitted} onClose={onClose} />}
    </Dialog>
  );
}

interface RowChoice {
  profileId: string;
  /** Unset: "switch" when a fallback is available, else "wait". */
  ifNothingFits?: IfNothingFits;
}

type RowPhase =
  | { phase: "queued" }
  | { phase: "adding" }
  | { phase: "added"; pick: Pick }
  | { phase: "exists" }
  | { phase: "error"; message: string };

function BulkAddFlow({ kind, picks, skipped, onSubmitted, onClose }: BulkAddRequest & { onSubmitted: () => void; onClose: () => void }) {
  const app = appFor(kind);
  const name = appName(app);
  const movie = app === "radarr";
  const options = useAppOptions(app, true);
  const config = useConfig();
  const requestPick = useRequestPick();
  const mounted = useMounted();
  const descriptionId = useId();
  const live = useCachedPicks(picks.map((p) => p.id));
  const [choices, setChoices] = useState<Record<number, RowChoice>>({});
  const [rootFolder, setRootFolder] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [phases, setPhases] = useState<Record<number, RowPhase>>({});

  const profiles = options.data?.quality_profiles ?? [];
  const folders = options.data?.root_folders ?? [];
  const needsFolder = folders.length > 1 && !options.data?.default_root_folder;
  // Movies wait for the config too: the profile order decides whether a row can fall back.
  const loading = options.isPending || (movie && config.isPending);
  const profileOrder = config.data?.radarr?.profile_order;

  const choiceFor = (id: number): RowChoice => choices[id] ?? { profileId: "" };
  const targetsOf = (choice: RowChoice) => fallbackTargets(profileOrder, Number(choice.profileId) || 0, profiles);
  const fitsFor = (id: number) => effectiveFallback(choiceFor(id).ifNothingFits, targetsOf(choiceFor(id)));

  const missing = picks.filter((p) => choiceFor(p.id).profileId === "").length;
  const canSubmit = !submitted && !loading && profiles.length > 0 && missing === 0 && (!needsFolder || rootFolder !== "");

  // The batch controls show a value only while the rows they fill agree on it.
  const rowProfiles = picks.map((p) => choiceFor(p.id).profileId);
  const allProfile = rowProfiles[0] && rowProfiles.every((v) => v === rowProfiles[0]) ? rowProfiles[0] : "";
  const fallbackRows = picks.filter((p) => targetsOf(choiceFor(p.id)).available);
  const fallbackFits = fallbackRows.map((p) => fitsFor(p.id));
  const allFits: IfNothingFits | "" =
    fallbackRows.length === 0 ? "wait" : fallbackFits.every((v) => v === fallbackFits[0]) ? fallbackFits[0]! : "";
  // One hint above the rows when they all have the same one, otherwise one per row.
  const hints = picks.map((p) => fallbackHint(targetsOf(choiceFor(p.id))));
  const sharedHint = hints.every((h) => h === hints[0]) ? hints[0] : undefined;

  const setRow = (id: number, patch: Partial<RowChoice>) =>
    setChoices((prev) => ({ ...prev, [id]: { ...(prev[id] ?? { profileId: "" }), ...patch } }));
  const setAllProfiles = (profileId: string) =>
    setChoices((prev) => Object.fromEntries(picks.map((p) => [p.id, { ...prev[p.id], profileId }])));
  // Only rows that can fall back take the batch choice; the rest keep waiting.
  const setAllFits = (ifNothingFits: IfNothingFits) =>
    setChoices((prev) =>
      Object.fromEntries(
        picks.map((p) => {
          const row: RowChoice = prev[p.id] ?? { profileId: "" };
          return [p.id, targetsOf(row).available ? { ...row, ifNothingFits } : row];
        }),
      ),
    );
  const setPhase = (id: number, phase: RowPhase) => setPhases((prev) => ({ ...prev, [id]: phase }));

  const submit = async () => {
    if (!canSubmit) return;
    const rows = picks.map((pick) => ({ pick, profileId: Number(choiceFor(pick.id).profileId), fits: movie ? fitsFor(pick.id) : undefined }));
    const folder = needsFolder ? rootFolder : undefined;
    setSubmitted(true);
    setPhases(Object.fromEntries(picks.map((p) => [p.id, { phase: "queued" }])));
    onSubmitted();

    const results = await mapLimit(rows, CONCURRENCY, async ({ pick, profileId, fits }) => {
      setPhase(pick.id, { phase: "adding" });
      try {
        const updated = await requestPick.mutateAsync({ pick, qualityProfileId: profileId, rootFolder: folder, ifNothingFits: fits });
        // The row reports the result while the dialog is open; otherwise a toast does when pick.updated arrives.
        if (updated.request?.release?.status === "checking") awaitRelease(pick.id, `Added to ${name}`);
        setPhase(pick.id, { phase: "added", pick: updated });
      } catch (err) {
        const exists = err instanceof ApiError && err.status === 409;
        setPhase(pick.id, exists ? { phase: "exists" } : { phase: "error", message: (err as Error).message });
        throw err;
      }
    });

    // Closed before every request came back: say how it went, as the single add dialog does with toasts.
    if (mounted.current) return;
    const added = results.filter((r) => r.status === "fulfilled").length;
    const exists = results.filter(isConflict).length;
    const errors = results
      .map((r, i) => (r.status === "rejected" && !isConflict(r) ? `${picks[i]!.title}: ${(r.reason as Error).message}` : ""))
      .filter(Boolean);
    const details = [exists > 0 ? `${exists} already in your library` : "", ...errors].filter(Boolean).join(" · ");
    if (errors.length > 0) toast.error(`Added ${added} of ${plural(picks.length, "title")} to ${name}`, { description: details });
    else toast.success(`Added ${plural(added, "title")} to ${name}`, { description: details || undefined });
  };

  const outcomes = picks.map((p, i) => outcomeOf(phases[p.id], live[i], movie)).filter((o): o is RowOutcome => o !== undefined);
  const footer = submitted
    ? bulkSummary(outcomes)
    : loading
      ? `Loading ${name} quality profiles…`
      : profiles.length === 0
        ? ""
        : missing > 0
          ? `${plural(missing, "title")} still ${missing === 1 ? "needs" : "need"} a quality profile`
          : needsFolder && rootFolder === ""
            ? "Choose a root folder"
            : "Every title has a quality profile";

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
          <PosterStack picks={picks} />
          <div className="min-w-0">
            <DialogTitle className="text-balance">
              Add {plural(picks.length, "title")} to {name}
            </DialogTitle>
            <DialogDescription id={descriptionId} className="mt-2">
              {submitted
                ? movie
                  ? "Added to Radarr one by one. You can close this; Proposarr tells you what Radarr finds."
                  : "Added to Sonarr one by one. Sonarr searches for each series itself."
                : `Choose the quality profile ${name} uses for each title. It is asked for every title you add.`}
            </DialogDescription>
            {skipped > 0 && (
              <p className="mt-2 flex items-start gap-1.5 text-[13px] text-text-muted">
                <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                {skipped === 1
                  ? `1 selected title is already in ${name} and was left out.`
                  : `${skipped} selected titles are already in ${name} and were left out.`}
              </p>
            )}
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {!submitted && (
            <BatchControls
              name={name}
              movie={movie}
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
              needsFolder={needsFolder}
              rootFolder={rootFolder}
              onRootFolder={setRootFolder}
            />
          )}

          <ul aria-label="Titles to add" className="divide-y divide-border">
            {picks.map((pick, i) => {
              const phase = phases[pick.id];
              const current = live[i] ?? (phase?.phase === "added" ? phase.pick : pick);
              const choice = choiceFor(pick.id);
              return (
                <li key={pick.id} className="grid grid-cols-[3rem_minmax(0,1fr)] gap-x-4 gap-y-3 px-5 py-4 sm:grid-cols-[3.5rem_minmax(0,1fr)]">
                  <Poster src={pick.poster_url} title={pick.title} className="row-span-2 w-12 self-start rounded-md sm:w-14" />
                  <div className="min-w-0">
                    <p className="flex min-w-0 items-baseline gap-2">
                      <span className="truncate text-[15px] leading-tight font-semibold">{pick.title}</span>
                      {pick.year && <span className="nums shrink-0 text-[13px] text-text-muted">{pick.year}</span>}
                    </p>
                    <RatingChips ratings={pick.ratings} className="mt-1.5" />
                  </div>
                  <div className="col-start-2 min-w-0">
                    {phase ? (
                      <RowStatus pick={current} phase={phase} movie={movie} />
                    ) : (
                      <RowControls
                        title={pick.title}
                        movie={movie}
                        options={options.data}
                        loading={loading}
                        choice={choice}
                        fits={fitsFor(pick.id)}
                        canFallBack={targetsOf(choice).available}
                        hint={sharedHint === undefined ? hints[i] : undefined}
                        onChange={(patch) => setRow(pick.id, patch)}
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
                  Add {plural(picks.length, "title")}
                </Button>
              </>
            )}
          </div>
        </footer>
      </form>
    </DialogContent>
  );
}

function isConflict(result: PromiseSettledResult<unknown>): boolean {
  return result.status === "rejected" && result.reason instanceof ApiError && result.reason.status === 409;
}

/** Where a row stands, for the summary. Undefined before submitting. */
function outcomeOf(phase: RowPhase | undefined, livePick: Pick | undefined, movie: boolean): RowOutcome | undefined {
  if (!phase) return undefined;
  if (phase.phase !== "added") return phase.phase;
  const release = livePick?.request?.release ?? phase.pick.request?.release;
  return movie && release ? release.status : "added";
}

/** A few posters fanned out beside the title. */
function PosterStack({ picks }: { picks: Pick[] }) {
  const shown = picks.slice(0, 3);
  const tilt = shown.length === 1 ? [""] : shown.length === 2 ? ["-rotate-6", "rotate-3"] : ["-rotate-8", "rotate-0", "rotate-8"];
  return (
    <div aria-hidden className="hidden shrink-0 items-end pl-1 sm:flex">
      {shown.map((p, i) => (
        <Poster
          key={p.id}
          src={p.poster_url}
          title={p.title}
          className={cn("w-12 origin-bottom rounded-md shadow-[0_10px_24px_-10px_rgb(0_0_0/0.7)] ring-2 ring-surface", i > 0 && "-ml-6", tilt[i])}
        />
      ))}
    </div>
  );
}

const FIELD_LABEL = "mb-1.5 block text-xs font-semibold text-text-muted";

function BatchControls({
  name,
  movie,
  options,
  loading,
  error,
  onRetry,
  allProfile,
  onAllProfile,
  allFits,
  canFallBack,
  hint,
  onAllFits,
  needsFolder,
  rootFolder,
  onRootFolder,
}: {
  name: string;
  movie: boolean;
  options: AppOptions | undefined;
  loading: boolean;
  error: string | null;
  onRetry: () => void;
  allProfile: string;
  onAllProfile: (profileId: string) => void;
  allFits: IfNothingFits | "";
  /** At least one row can fall back to a lower-ranked profile. */
  canFallBack: boolean;
  /** The fallback hint every row shares, if they do. */
  hint?: string;
  onAllFits: (value: IfNothingFits) => void;
  needsFolder: boolean;
  rootFolder: string;
  onRootFolder: (path: string) => void;
}) {
  const profiles = options?.quality_profiles ?? [];
  const folders = options?.root_folders ?? [];

  if (error) {
    return (
      <div className="border-b border-border px-5 py-4">
        <div role="alert" className="flex items-start gap-2 rounded-[var(--radius-control)] border border-danger/35 bg-danger/8 p-3 text-sm">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-danger" />
          <div className="min-w-0 flex-1">
            <p className="font-medium">Could not load {name} quality profiles</p>
            <p className="mt-0.5 break-words text-text-muted">{error}</p>
          </div>
          <Button size="sm" onClick={onRetry}>
            Retry
          </Button>
        </div>
      </div>
    );
  }
  if (!loading && profiles.length === 0) {
    return <p className="border-b border-border px-5 py-4 text-sm text-text-muted">{name} has no quality profiles. Create one in {name} first.</p>;
  }

  return (
    // Lined up with the rows' controls, so the batch fields read as column headings. Pinned while scrolling from sm up.
    <section aria-label="Every title" className="z-10 border-b border-border bg-surface-raised/95 px-5 py-4 backdrop-blur-md sm:sticky sm:top-0 sm:pl-[calc(1.25rem+4.5rem)]">
      <div className={cn(CONTROL_COLUMNS, "gap-3")}>
        <div className="min-w-0">
          <span className={FIELD_LABEL}>Quality profile</span>
          {loading ? (
            <ControlSkeleton />
          ) : (
            <Select value={allProfile} onValueChange={onAllProfile} label="Set every title's quality profile" placeholder="Set all to…" className="w-full">
              {profiles.map((p) => (
                <SelectItem key={p.id} value={String(p.id)}>
                  {p.name}
                </SelectItem>
              ))}
            </Select>
          )}
        </div>
        {movie && (
          <div className="min-w-0">
            <span className={FIELD_LABEL}>{IF_NOTHING_FITS_LEGEND}</span>
            {loading ? (
              <ControlSkeleton />
            ) : (
              <FallbackSelect
                value={allFits}
                onChange={onAllFits}
                canFallBack={canFallBack}
                label={`${IF_NOTHING_FITS_LEGEND}, for every title that can fall back`}
                placeholder="Mixed"
              />
            )}
            {hint && !loading && <FallbackHint text={hint} className="mt-1.5" />}
          </div>
        )}
        {needsFolder && (
          <div className="min-w-0 sm:col-span-2">
            <span className={FIELD_LABEL}>Root folder</span>
            <Select value={rootFolder} onValueChange={onRootFolder} label="Root folder" placeholder="Choose where they go" className={cn("w-full", rootFolder === "" && "border-dashed")}>
              {folders.map((f) => (
                <SelectItem key={f.id} value={f.path}>
                  <span className="flex items-center gap-2">
                    <HardDrive className="size-4 text-text-muted" />
                    {f.path}
                    {f.free_space > 0 && <span className="text-text-muted">{gigabytes(f.free_space)}</span>}
                  </span>
                </SelectItem>
              ))}
            </Select>
          </div>
        )}
      </div>
    </section>
  );
}

function ControlSkeleton() {
  return <div aria-hidden className="h-9 animate-pulse rounded-[var(--radius-control)] bg-surface" />;
}

function RowControls({
  title,
  movie,
  options,
  loading,
  choice,
  fits,
  canFallBack,
  hint,
  onChange,
}: {
  title: string;
  movie: boolean;
  options: AppOptions | undefined;
  loading: boolean;
  choice: RowChoice;
  fits: IfNothingFits;
  canFallBack: boolean;
  /** Shown under the fallback choice when the rows' hints differ. */
  hint?: string;
  onChange: (patch: Partial<RowChoice>) => void;
}) {
  const profiles = options?.quality_profiles ?? [];
  if (loading) {
    return (
      <div className={CONTROL_COLUMNS}>
        <ControlSkeleton />
        {movie && <ControlSkeleton />}
      </div>
    );
  }
  if (profiles.length === 0) return null;
  return (
    <div className={CONTROL_COLUMNS}>
      <Select
        value={choice.profileId}
        onValueChange={(profileId) => onChange({ profileId })}
        label={`Quality profile for ${title}`}
        placeholder="Choose a quality profile"
        // Dashed until chosen: every title needs one.
        className={cn("w-full", choice.profileId === "" && "border-dashed border-text-muted/55")}
      >
        {profiles.map((p) => (
          <SelectItem key={p.id} value={String(p.id)}>
            {p.name}
          </SelectItem>
        ))}
      </Select>
      {movie && (
        <div className="min-w-0">
          <FallbackSelect
            value={fits}
            onChange={(ifNothingFits) => onChange({ ifNothingFits })}
            canFallBack={canFallBack}
            label={`${IF_NOTHING_FITS_LEGEND}, for ${title}`}
          />
          {hint && <FallbackHint text={hint} className="mt-1.5" />}
        </div>
      )}
    </div>
  );
}

/** "If nothing fits this profile": switching is disabled unless a lower-ranked profile exists. */
function FallbackSelect({
  value,
  onChange,
  canFallBack,
  label,
  placeholder,
}: {
  value: IfNothingFits | "";
  onChange: (value: IfNothingFits) => void;
  canFallBack: boolean;
  label: string;
  placeholder?: string;
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as IfNothingFits)} label={label} placeholder={placeholder} className="w-full text-[13px]">
      {IF_NOTHING_FITS_OPTIONS.map((o) => (
        <OptionItem key={o.value} value={o.value} disabled={o.value === "switch" && !canFallBack}>
          {o.label}
        </OptionItem>
      ))}
    </Select>
  );
}

/** ui/select's SelectItem look, plus a disabled state. */
function OptionItem({ value, disabled, children }: { value: string; disabled?: boolean; children: ReactNode }) {
  return (
    <SelectPrimitive.Item
      value={value}
      disabled={disabled}
      className="relative flex cursor-pointer items-center rounded-[6px] py-2 pr-8 pl-2.5 outline-none select-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-45 data-[highlighted]:bg-surface data-[state=checked]:text-accent"
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2.5">
        <Check className="size-4" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}

function FallbackHint({ text, className }: { text: string; className?: string }) {
  const look = cn("text-xs leading-snug text-text-muted", className);
  if (text !== UNRANKED_HINT) return <p className={look}>{text}</p>;
  const [before, after] = UNRANKED_HINT.split("Connections");
  return (
    <p className={look}>
      {before}
      <Link to="/connections" className="font-medium text-text underline decoration-border underline-offset-4 hover:decoration-text">
        Connections
      </Link>
      {after}
    </p>
  );
}

/** A submitted row, following its pick from the cache as pick.updated events come in. */
function RowStatus({ pick, phase, movie }: { pick: Pick; phase: RowPhase; movie: boolean }) {
  const release = phase.phase === "added" ? (pick.request?.release ?? phase.pick.request?.release) : undefined;
  const settled = !!release && release.status !== "checking";
  // While the row shows the check, it reports the result instead of a toast.
  useReleaseShown(pick.id);
  useEffect(() => {
    if (settled) takeAwaitedRelease(pick.id);
  }, [settled, pick.id]);

  const label = statusLabel(phase, pick, movie ? release : undefined);
  const canSwitch = release?.status === "waiting" && release.alternatives.length > 0;

  return (
    <div className="flex items-start gap-2.5 text-sm">
      <StatusIcon phase={phase} release={release} tone={label.tone} />
      <div className="min-w-0 flex-1">
        <p className="font-medium break-words">{label.title}</p>
        {label.description && (
          <p className="mt-0.5 truncate text-[13px] text-text-muted" title={label.description}>
            {label.description}
          </p>
        )}
        {release?.status === "waiting" && release.qualities.length > 0 && (
          <p className="nums mt-1 truncate text-xs text-text-muted">{release.qualities.map((q) => `${q.count} × ${q.quality}`).join(" · ")}</p>
        )}
        {release && canSwitch && <SwitchControl pick={pick} check={release} />}
      </div>
    </div>
  );
}

function statusLabel(phase: RowPhase, pick: Pick, release: ReleaseCheck | undefined): StatusLabel {
  switch (phase.phase) {
    case "queued":
      return { tone: "neutral", title: "Queued" };
    case "adding":
      return { tone: "pending", title: "Adding…" };
    case "exists":
      return { tone: "neutral", title: "Already in your library" };
    case "error":
      return { tone: "danger", title: phase.message };
    case "added":
      if (release) return releaseLabel("Added", release);
      return { tone: "success", title: "Added", description: pick.request?.quality_profile ?? phase.pick.request?.quality_profile };
  }
}

function StatusIcon({ phase, release, tone }: { phase: RowPhase; release?: ReleaseCheck; tone: StatusLabel["tone"] }) {
  const base = "mt-0.5 size-4 shrink-0";
  if (tone === "pending") return <Loader2 aria-hidden className={cn(base, "animate-spin text-accent")} />;
  if (tone === "success") return <Check aria-hidden className={cn(base, "text-success")} strokeWidth={2.5} />;
  if (tone === "danger") return <AlertCircle aria-hidden className={cn(base, "text-danger")} />;
  if (phase.phase === "exists") return <Check aria-hidden className={cn(base, "text-text-muted")} />;
  switch (release?.status) {
    case "waiting":
      return <Clock aria-hidden className={cn(base, "text-warning")} />;
    case "failed":
      return <CircleAlert aria-hidden className={cn(base, "text-warning")} />;
    case "searching":
      return <Search aria-hidden className={cn(base, "text-text-muted")} />;
    case "unavailable":
      return <CalendarClock aria-hidden className={cn(base, "text-text-muted")} />;
    default:
      return <Clock aria-hidden className={cn(base, "text-text-muted")} />;
  }
}

/** Radarr grabbed nothing, but other profiles would grab a release now: switch this title to one of them. */
function SwitchControl({ pick, check }: { pick: Pick; check: ReleaseCheck }) {
  const qc = useQueryClient();
  const switchProfile = useSwitchProfile();
  const mounted = useMounted();
  const [profileId, setProfileId] = useState("");
  const [error, setError] = useState<string | null>(null);
  const chosen = check.alternatives.find((a) => String(a.id) === profileId);

  const submit = async () => {
    if (!chosen || switchProfile.isPending) return;
    setError(null);
    try {
      const updated = await switchProfile.mutateAsync({ pick, qualityProfileId: chosen.id });
      if (updated.request?.release?.status === "checking") awaitRelease(pick.id, `Switched to ${chosen.name}`);
    } catch (err) {
      const message = (err as Error).message;
      const conflict = err instanceof ApiError && err.status === 409;
      // Usually a check that is still running (started elsewhere): refresh, so the row shows it.
      if (conflict) void qc.invalidateQueries({ queryKey: ["picks"] });
      if (mounted.current) setError(conflict ? `Can't switch right now: ${message}` : message);
      else toast.error(`Could not switch ${pick.title} to ${chosen.name}`, { description: message });
    }
  };

  return (
    <div className="mt-2.5">
      <div className="flex flex-col gap-2 sm:flex-row">
        <Select
          value={profileId}
          onValueChange={setProfileId}
          label={`Switch ${pick.title} to another quality profile`}
          placeholder="Available now with another profile…"
          className="w-full sm:flex-1"
        >
          {check.alternatives.map((a) => (
            <SelectItem key={a.id} value={String(a.id)}>
              <span className="flex items-baseline gap-2">
                <span className="font-medium">{a.name}</span>
                <span className="nums text-xs text-text-muted">
                  {[a.count === 1 ? "1 release" : `${a.count} releases`, a.best.quality, fileSize(a.best.size)].filter(Boolean).join(" · ")}
                </span>
              </span>
            </SelectItem>
          ))}
        </Select>
        <Button size="sm" className="h-9" variant={chosen ? "primary" : "secondary"} disabled={!chosen || switchProfile.isPending} onClick={() => void submit()}>
          {switchProfile.isPending ? (
            <>
              <Loader2 className="animate-spin" /> Switching…
            </>
          ) : (
            "Switch and search"
          )}
        </Button>
      </div>
      {error && (
        <p role="alert" className="mt-2 flex items-start gap-1.5 text-[13px] text-danger">
          <AlertCircle aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0 break-words">{error}</span>
        </p>
      )}
    </div>
  );
}

/** False once the component has unmounted (the dialog closed). */
function useMounted() {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return mounted;
}
