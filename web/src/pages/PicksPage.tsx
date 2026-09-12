import { AlertCircle, Clapperboard, Filter, Gauge, Loader2, Search, Settings2 } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, Navigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useLive } from "@/api/live";
import { usePicks, useRuns, useStartRun, useStatus } from "@/api/queries";
import type { Kind, Pick, Run, VerdictFilter } from "@/api/types";
import { AcceptDialog } from "@/components/AcceptDialog";
import { EmptyState, ErrorNote } from "@/components/EmptyState";
import { PickCard } from "@/components/PickCard";
import { SelectionBar } from "@/components/SelectionBar";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { Select, SelectItem } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { appFor, appName, duration, isOpenSearch, relativeTime } from "@/lib/format";
import { byRating } from "@/lib/ratings";
import { useSelection } from "@/lib/useSelection";
import { useTick } from "@/lib/useTick";
import { cn } from "@/lib/utils";

const EXAMPLE_VIBE: Record<Kind, string> = {
  movies: "slow-burn sci-fi with a big idea",
  series: "something I can finish in a month",
};

const EXAMPLE_SEARCH: Record<Kind, string> = {
  movies: "90s heist movies with a twist ending",
  series: "short Korean thrillers",
};

const MIN_SCORES = ["0", "60", "70", "80", "90"];

const USE_TASTE_KEY = "proposarr.useTaste";

function matchesVerdict(p: Pick, v: VerdictFilter) {
  if (v === "all") return true;
  if (v === "none") return !p.verdict;
  return p.verdict === v;
}

/** "Use my taste", remembered across visits when storage is available. `startWithSearch` turns it off. */
function useTastePreference(startWithSearch: boolean): [boolean, (value: boolean) => void] {
  const [value, setValue] = useState(() => {
    try {
      if (startWithSearch) {
        localStorage.setItem(USE_TASTE_KEY, "false");
        return false;
      }
      return localStorage.getItem(USE_TASTE_KEY) !== "false";
    } catch {
      return !startWithSearch;
    }
  });
  const set = useCallback((next: boolean) => {
    setValue(next);
    try {
      localStorage.setItem(USE_TASTE_KEY, String(next));
    } catch {
      // Private mode; the choice lasts for this page only.
    }
  }, []);
  return [value, set];
}

export function PicksPage() {
  const [params, setParams] = useSearchParams();
  const kind: Kind = params.get("kind") === "series" ? "series" : "movies";
  const verdict = (params.get("verdict") ?? "none") as VerdictFilter;
  const minScore = params.get("min") ?? "0";
  const runParam = params.get("run") ?? "latest";

  const update = (next: Record<string, string | null>) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        for (const [k, v] of Object.entries(next)) {
          if (v === null) n.delete(k);
          else n.set(k, v);
        }
        return n;
      },
      { replace: true },
    );

  const status = useStatus();
  const runs = useRuns();
  const { running } = useLive();
  const picks = usePicks({ kind, run: runParam === "latest" ? "latest" : Number(runParam), verdict: "all" });
  const [accepting, setAccepting] = useState<Pick | null>(null);
  const [useTaste, setUseTaste] = useTastePreference(params.get("mode") === "search");

  // ?mode=search (the Collection's "Try an open search") switches taste off once, then leaves the URL.
  useEffect(() => {
    if (params.get("mode") !== "search") return;
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.delete("mode");
        return n;
      },
      { replace: true },
    );
    document.getElementById("vibe")?.focus();
  }, [params, setParams]);

  const app = appFor(kind);
  const configured = status.data ? status.data.connections[app] && status.data.connections.tmdb : true;
  const liveRun = Object.values(running).find((r) => r.kind === kind);
  const runsById = useMemo(() => new Map((runs.data ?? []).map((r) => [r.id, r])), [runs.data]);
  const liveSearch = liveRun ? (liveRun.useTaste !== undefined ? !liveRun.useTaste : isOpenSearch(runsById.get(liveRun.runId))) : false;
  const kindRuns = useMemo(() => (runs.data ?? []).filter((r) => r.kind === kind), [runs.data, kind]);
  const succeeded = kindRuns.filter((r) => r.status === "succeeded");
  const shownRun: Run | undefined = runParam === "latest" ? succeeded[0] : kindRuns.find((r) => r.id === Number(runParam));
  const shownSearch = isOpenSearch(shownRun);
  // A failed or rate-limited run newer than the one on screen.
  const newerProblem = runParam === "latest" ? kindRuns.find((r) => (r.status === "failed" || r.status === "rate_limited") && (!shownRun || r.id > shownRun.id)) : undefined;

  const all = picks.data ?? [];
  const counts: Record<VerdictFilter, number> = {
    none: all.filter((p) => !p.verdict).length,
    accepted: all.filter((p) => p.verdict === "accepted").length,
    later: all.filter((p) => p.verdict === "later").length,
    ignored: all.filter((p) => p.verdict === "ignored").length,
    all: all.length,
  };
  const filtered = all.filter((p) => matchesVerdict(p, verdict) && p.score >= Number(minScore));
  // Open-search results are ranked by real ratings, as the server does; everything else keeps the API order.
  const visible = shownSearch ? [...filtered].sort(byRating) : filtered;
  // Starts empty again for another kind, run or filter.
  const selection = useSelection(visible, `${kind}:${runParam}:${verdict}:${minScore}`);

  if (status.data?.setup_required) return <Navigate to="/setup" replace />;

  return (
    <>
      <RunForm
        kind={kind}
        onKind={(k) => update({ kind: k, run: null })}
        configured={configured}
        liveRun={liveRun}
        liveSearch={liveSearch}
        useTaste={useTaste}
        onUseTaste={setUseTaste}
      />

      {!configured ? (
        <EmptyState
          icon={Settings2}
          title={`Connect ${appName(app)} first`}
          actions={
            <Button asChild variant="primary">
              <Link to="/connections">Open connections</Link>
            </Button>
          }
        >
          <p>
            Proposarr needs {appName(app)} and a TMDB key to find {kind === "series" ? "series" : "movies"} for you.
          </p>
        </EmptyState>
      ) : (
        <>
          {newerProblem && <RunProblem run={newerProblem} />}

          {(all.length > 0 || runParam !== "latest") && (
            <div className="mb-6 flex flex-col gap-3 border-b border-border pb-4 xl:flex-row xl:items-center xl:justify-between">
              <div className="no-scrollbar -mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
                <Segmented
                  label="Filter by decision"
                  value={verdict}
                  onChange={(v) => update({ verdict: v === "none" ? null : v })}
                  options={[
                    { value: "none", label: "Undecided", count: counts.none },
                    { value: "accepted", label: "Accepted", count: counts.accepted },
                    { value: "later", label: "Later", count: counts.later },
                    { value: "ignored", label: "Ignored", count: counts.ignored },
                    { value: "all", label: "All", count: counts.all },
                  ]}
                />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Select value={minScore} onValueChange={(v) => update({ min: v === "0" ? null : v })} label="Minimum score" className="w-[9.5rem]">
                  {MIN_SCORES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {s === "0" ? "Any score" : `Score ${s} or more`}
                    </SelectItem>
                  ))}
                </Select>
                <Select value={runParam} onValueChange={(v) => update({ run: v === "latest" ? null : v })} label="Run" className="w-[13rem]">
                  <SelectItem value="latest">Latest run</SelectItem>
                  {succeeded.map((r) => (
                    <SelectItem key={r.id} value={String(r.id)}>
                      {isOpenSearch(r) ? `Search “${r.vibe ?? ""}”` : r.vibe ? `“${r.vibe}”` : "By taste"}, {relativeTime(r.started_at)}
                    </SelectItem>
                  ))}
                </Select>
              </div>
            </div>
          )}

          {shownRun && all.length > 0 && (
            <p className="mb-5 text-sm text-text-muted">
              {shownSearch ? (
                <>
                  {shownRun.pick_count} picks for <span className="text-text">“{shownRun.vibe}”</span>, searched {relativeTime(shownRun.finished_at ?? shownRun.started_at)}.
                </>
              ) : (
                <>
                  {shownRun.pick_count} picks {relativeTime(shownRun.finished_at ?? shownRun.started_at)}
                  {shownRun.vibe ? <> for <span className="text-text">“{shownRun.vibe}”</span></> : " by taste alone"}, chosen from {shownRun.candidate_count} candidates.
                </>
              )}
            </p>
          )}

          {picks.isPending ? (
            <PickGridSkeleton />
          ) : picks.isError ? (
            <ErrorNote title="Could not load picks" message={picks.error.message} onRetry={() => void picks.refetch()} />
          ) : all.length === 0 ? (
            <EmptyState icon={Clapperboard} title="No picks yet">
              {useTaste ? (
                <p>
                  Describe a mood above, or leave it empty, and press Run now. Proposarr reads your {appName(app)} library and watch history and
                  asks Claude for {kind === "series" ? "series" : "movies"} you don't have yet.
                </p>
              ) : (
                <p>
                  Describe what you are looking for above and press Search. Claude suggests {kind === "series" ? "series" : "movies"} from the
                  description alone and leaves out titles you already have.
                </p>
              )}
            </EmptyState>
          ) : visible.length === 0 ? (
            <EmptyState
              icon={Filter}
              title={verdict === "none" ? "All decided" : "Nothing here"}
              actions={
                <Button onClick={() => update({ verdict: "all", min: null })}>Show all picks</Button>
              }
            >
              <p>
                {verdict === "none"
                  ? "You went through every pick from this run. Run again for fresh ones, or look at what you saved for later."
                  : "No picks match these filters."}
              </p>
            </EmptyState>
          ) : (
            <ul className="grid grid-cols-2 gap-x-4 gap-y-9 sm:grid-cols-3 sm:gap-x-5 md:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
              <AnimatePresence mode="popLayout" initial={false}>
                {visible.map((p, i) => (
                  <motion.li
                    key={p.id}
                    layout
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0, transition: { delay: Math.min(i, 12) * 0.025, duration: 0.3 } }}
                    exit={{ opacity: 0, scale: 0.94, transition: { duration: 0.18 } }}
                  >
                    <PickCard
                      pick={p}
                      onAccept={setAccepting}
                      openSearch={isOpenSearch(runsById.get(p.run_id)) || p.run_use_taste === false}
                      selection={{ selected: selection.isSelected(p.id), active: selection.active, onToggle: (range) => selection.toggle(p.id, range) }}
                    />
                  </motion.li>
                ))}
              </AnimatePresence>
            </ul>
          )}
        </>
      )}

      <SelectionBar selection={selection} kind={kind} />
      <AcceptDialog pick={accepting} onClose={() => setAccepting(null)} />
    </>
  );
}

function RunForm({
  kind,
  onKind,
  configured,
  liveRun,
  liveSearch,
  useTaste,
  onUseTaste,
}: {
  kind: Kind;
  onKind: (k: Kind) => void;
  configured: boolean;
  liveRun?: { runId: number; message: string; startedAt?: string };
  /** The live run is an open search. */
  liveSearch: boolean;
  useTaste: boolean;
  onUseTaste: (value: boolean) => void;
}) {
  const [vibe, setVibe] = useState("");
  const [error, setError] = useState<string | null>(null);
  const start = useStartRun();
  const busy = !!liveRun || start.isPending;
  const needsVibe = !useTaste && vibe.trim() === "";
  const searching = liveRun ? liveSearch : !useTaste;
  const noun = kind === "series" ? "series" : "movies";
  useTick(!!liveRun);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy || !configured || needsVibe) return;
    setError(null);
    const description = vibe.trim();
    start.mutate(
      { kind, vibe, useTaste },
      {
        onSuccess: () =>
          useTaste
            ? toast(`Finding ${noun}`, { description: description ? `“${description}”` : "Going by your taste alone." })
            : toast(`Searching ${noun}`, { description: `“${description}”` }),
        onError: (err) => {
          if (!useTaste && err instanceof ApiError && err.status === 400) {
            setError(err.message);
            return;
          }
          toast.error(err instanceof ApiError && err.status === 409 ? `A ${kind} run is already going` : "Could not start the run", {
            description: err.message,
          });
        },
      },
    );
  };

  return (
    <section className="mb-10 lg:mb-12" aria-labelledby="vibe-label">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Segmented
          label="Library"
          value={kind}
          onChange={(k) => {
            setError(null);
            onKind(k);
          }}
          options={[
            { value: "movies", label: "Movies" },
            { value: "series", label: "Series" },
          ]}
        />
        <Switch
          checked={useTaste}
          onCheckedChange={(v) => {
            setError(null);
            onUseTaste(v);
          }}
          aria-describedby="vibe-help"
          className="-mr-2"
        >
          Use my taste
        </Switch>
      </div>
      <form onSubmit={submit} className="mt-7">
        <label id="vibe-label" htmlFor="vibe" className="text-[15px] text-text-muted">
          {useTaste ? "What are you in the mood for?" : "What are you looking for?"}
        </label>
        <div className="mt-2 flex flex-col gap-4 md:flex-row md:items-end">
          <input
            id="vibe"
            value={vibe}
            onChange={(e) => {
              setVibe(e.target.value);
              setError(null);
            }}
            maxLength={200}
            autoComplete="off"
            placeholder={useTaste ? EXAMPLE_VIBE[kind] : EXAMPLE_SEARCH[kind]}
            disabled={!configured}
            required={!useTaste}
            aria-invalid={error ? true : undefined}
            aria-describedby="vibe-help"
            className={cn(
              "min-w-0 flex-1 border-b-2 border-border bg-transparent pb-2 font-display text-[40px] leading-[1.05] font-bold tracking-tight text-text transition-colors",
              "placeholder:text-text-muted/25 hover:border-text-muted/50 focus:border-accent focus:outline-none disabled:opacity-50 sm:text-[56px] xl:text-[68px]",
              "aria-[invalid=true]:border-danger",
            )}
          />
          <Button type="submit" variant="primary" size="lg" disabled={busy || !configured || needsVibe} className="md:mb-2">
            {busy ? <Loader2 className="animate-spin" /> : searching ? <Search /> : null}
            {busy ? (searching ? "Searching" : "Running") : useTaste ? "Run now" : "Search"}
          </Button>
        </div>
        <div id="vibe-help" className="mt-3 min-h-6 text-sm text-text-muted" aria-live="polite">
          {liveRun ? (
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="size-2 animate-pulse-dot rounded-full bg-accent" />
              <span className="text-text">{liveRun.message}</span>
              {liveRun.startedAt && <span className="nums">{duration(liveRun.startedAt)}</span>}
              <Link to="/runs" className="underline decoration-border underline-offset-4 hover:text-text">
                See run
              </Link>
            </span>
          ) : error ? (
            <p role="alert" className="flex items-start gap-2 text-danger">
              <AlertCircle className="mt-0.5 size-4 shrink-0" />
              <span className="break-words">{error}</span>
            </p>
          ) : useTaste ? (
            <span>Leave it empty to go by your taste alone. A run takes a minute or two.</span>
          ) : (
            <span className="flex flex-col gap-0.5">
              <span>Claude searches by your description only, not your library or history. Titles you already have are left out.</span>
              {/* Same height either way, so typing the first letter does not move the page. */}
              <span className={needsVibe ? "text-text" : undefined}>
                {needsVibe ? "Describe what you want to find to start a search." : "A search takes a minute or two."}
              </span>
            </span>
          )}
        </div>
      </form>
    </section>
  );
}

function RunProblem({ run }: { run: Run }) {
  const limited = run.status === "rate_limited";
  return (
    <div
      role="alert"
      className={cn(
        "mb-6 flex flex-col gap-3 rounded-[var(--radius-panel)] border p-4 sm:flex-row sm:items-center",
        limited ? "border-warning/40 bg-warning/8" : "border-danger/35 bg-danger/8",
      )}
    >
      <Gauge className={cn("size-5 shrink-0", limited ? "text-warning" : "text-danger")} />
      <div className="min-w-0 flex-1">
        <p className="font-semibold">{limited ? "Claude session limit reached" : "The last run failed"}</p>
        <p className="mt-0.5 text-sm break-words text-text-muted">
          {limited ? "Your Claude subscription window is used up. Run again after it resets; the picks below are from an earlier run." : run.error}
        </p>
      </div>
      <Button asChild size="sm">
        <Link to="/runs">View runs</Link>
      </Button>
    </div>
  );
}

function PickGridSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-9 sm:grid-cols-3 sm:gap-x-5 md:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6" aria-busy aria-label="Loading picks">
      {Array.from({ length: 10 }, (_, i) => (
        <div key={i}>
          <div className="aspect-[2/3] animate-pulse rounded-[var(--radius-poster)] bg-surface-raised" />
          <div className="mt-3 h-4 w-3/4 animate-pulse rounded bg-surface-raised" />
          <div className="mt-2 h-3 w-full animate-pulse rounded bg-surface-raised" />
        </div>
      ))}
    </div>
  );
}
