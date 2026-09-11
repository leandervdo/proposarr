import * as Collapsible from "@radix-ui/react-collapsible";
import { Check, ChevronDown, Gauge, History, Search, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { useLive } from "@/api/live";
import { useRuns } from "@/api/queries";
import type { Run } from "@/api/types";
import { EmptyState, ErrorNote } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { cost, duration, isOpenSearch, kindLabel, relativeTime, tokens } from "@/lib/format";
import { cn } from "@/lib/utils";

export function RunsPage() {
  const runs = useRuns();
  const { running } = useLive();
  const anyRunning = (runs.data ?? []).some((r) => r.status === "running");
  useTick(anyRunning);

  return (
    <>
      <PageHeader title="Runs">Every recommendation run, newest first, with what it found and what it cost.</PageHeader>
      {runs.isPending ? (
        <div className="flex flex-col gap-4" aria-busy>
          {[0, 1, 2].map((i) => (
            <div key={i} className="ml-12 h-36 animate-pulse rounded-[var(--radius-panel)] bg-surface" />
          ))}
        </div>
      ) : runs.isError ? (
        <ErrorNote title="Could not load runs" message={runs.error.message} onRetry={() => void runs.refetch()} />
      ) : runs.data.length === 0 ? (
        <EmptyState
          icon={History}
          title="No runs yet"
          actions={
            <Button asChild variant="primary">
              <Link to="/">Go to picks</Link>
            </Button>
          }
        >
          <p>Start a run from the Picks page. Each one shows up here with its status, duration, tokens and cost.</p>
        </EmptyState>
      ) : (
        <ol className="relative max-w-4xl">
          <span aria-hidden className="absolute top-2 bottom-2 left-[15px] w-px bg-border" />
          {runs.data.map((run) => (
            <RunRow key={run.id} run={run} message={running[run.id]?.message} />
          ))}
        </ol>
      )}
    </>
  );
}

function useTick(active: boolean) {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!active) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [active]);
}

const STATUS_LABEL: Record<Run["status"], string> = {
  running: "Running",
  succeeded: "Finished",
  failed: "Failed",
  rate_limited: "Session limit",
};

function RunRow({ run, message }: { run: Run; message?: string }) {
  const warnings = run.warnings ?? [];
  const rejected = run.rejected ?? [];
  const extra = warnings.length + rejected.length;
  const search = isOpenSearch(run);

  return (
    <li className="relative pb-5 pl-12 last:pb-0">
      <StatusNode status={run.status} />
      <article className="rounded-[var(--radius-panel)] border border-border bg-surface p-4 sm:p-5">
        <header className="flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
          <h2 className="min-w-0 text-[17px] leading-snug font-semibold">
            {kindLabel(run.kind)}
            {search ? (
              <>
                <span className="mx-2 inline-flex -translate-y-px items-center gap-1 rounded-full border border-border px-2 py-0.5 align-middle text-[11px] leading-tight font-medium text-text-muted">
                  <Search aria-hidden className="size-3" />
                  Search
                </span>
                <span className="font-normal text-text-muted">“{run.vibe}”</span>
              </>
            ) : (
              <span className="font-normal text-text-muted">{run.vibe ? <> for “{run.vibe}”</> : " by taste"}</span>
            )}
          </h2>
          <p className="shrink-0 text-sm text-text-muted">
            <span className={cn("font-medium", statusTone(run.status))}>{STATUS_LABEL[run.status]}</span>{" "}
            <time dateTime={run.started_at} title={new Date(run.started_at).toLocaleString()}>
              {relativeTime(run.started_at)}
            </time>
          </p>
        </header>

        {run.status === "running" && (
          <p className="mt-2 flex items-center gap-2 text-sm" aria-live="polite">
            <span className="size-2 animate-pulse-dot rounded-full bg-accent" />
            {message ?? "Working…"}
          </p>
        )}
        {run.status === "rate_limited" && (
          <div className="mt-3 rounded-[var(--radius-control)] border border-warning/35 bg-warning/8 p-3 text-sm">
            <p className="font-semibold text-warning">Claude session limit reached</p>
            <p className="mt-0.5 text-text-muted">Your subscription window was used up before the run could finish. Start it again after the reset.</p>
            {run.error && <p className="mt-1 break-words text-text-muted">{run.error}</p>}
          </div>
        )}
        {run.status === "failed" && run.error && (
          <p className="mt-3 rounded-[var(--radius-control)] border border-danger/35 bg-danger/8 p-3 text-sm break-words">{run.error}</p>
        )}

        <dl className="mt-4 grid grid-cols-3 gap-x-4 gap-y-3 sm:grid-cols-6">
          <Stat label="Duration" value={duration(run.started_at, run.finished_at)} />
          <Stat label="Cost" value={run.status === "running" ? "…" : cost(run.cost_usd)} />
          <Stat label="Tokens in" value={tokens(run.input_tokens)} />
          <Stat label="Tokens out" value={tokens(run.output_tokens)} />
          <Stat label="Turns" value={String(run.num_turns)} />
          <Stat
            label="Picks"
            value={run.status !== "succeeded" ? "–" : search && run.candidate_count === 0 ? String(run.pick_count) : `${run.pick_count} of ${run.candidate_count}`}
          />
        </dl>

        {(extra > 0 || run.status === "succeeded") && (
          <Collapsible.Root>
            <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-border pt-3">
              {extra > 0 && (
                <Collapsible.Trigger className="group inline-flex h-8 items-center gap-1.5 rounded-[var(--radius-control)] px-2 text-sm text-text-muted hover:bg-surface-raised hover:text-text">
                  <ChevronDown className="size-4 transition-transform group-data-[state=open]:rotate-180" />
                  {[warnings.length && `${warnings.length} ${warnings.length === 1 ? "warning" : "warnings"}`, rejected.length && `${rejected.length} rejected ${rejected.length === 1 ? "pick" : "picks"}`]
                    .filter(Boolean)
                    .join(" and ")}
                </Collapsible.Trigger>
              )}
              {run.status === "succeeded" && run.pick_count > 0 && (
                <Button asChild variant="ghost" size="sm" className="ml-auto">
                  <Link to={`/?kind=${run.kind}&run=${run.id}&verdict=all`}>Show picks</Link>
                </Button>
              )}
            </div>
            <Collapsible.Content className="mt-2 flex flex-col gap-3 text-sm">
              {warnings.length > 0 && (
                <ul className="flex flex-col gap-1.5">
                  {warnings.map((w, i) => (
                    <li key={i} className="flex gap-2 break-words text-text-muted">
                      <span className="mt-2 size-1.5 shrink-0 rounded-full bg-warning" />
                      {w}
                    </li>
                  ))}
                </ul>
              )}
              {rejected.length > 0 && (
                <ul className="flex flex-col gap-1.5">
                  {rejected.map((r, i) => (
                    <li key={i} className="flex gap-2 text-text-muted">
                      <X className="mt-0.5 size-4 shrink-0 text-danger" />
                      <span>
                        <span className="text-text">{r.title}</span> was left out: {r.reason}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </Collapsible.Content>
          </Collapsible.Root>
        )}
      </article>
    </li>
  );
}

function statusTone(s: Run["status"]) {
  return s === "succeeded" ? "text-success" : s === "failed" ? "text-danger" : s === "rate_limited" ? "text-warning" : "text-accent";
}

function StatusNode({ status }: { status: Run["status"] }) {
  return (
    <span
      aria-hidden
      className={cn(
        "absolute top-4 left-0 grid size-8 place-items-center rounded-full border-2 bg-background",
        status === "succeeded" && "border-success/60 text-success",
        status === "failed" && "border-danger/60 text-danger",
        status === "rate_limited" && "border-warning/60 text-warning",
        status === "running" && "border-accent text-accent",
      )}
    >
      {status === "succeeded" && <Check className="size-4" strokeWidth={2.5} />}
      {status === "failed" && <X className="size-4" strokeWidth={2.5} />}
      {status === "rate_limited" && <Gauge className="size-4" />}
      {status === "running" && <span className="size-2.5 animate-pulse-dot rounded-full bg-accent" />}
    </span>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-text-muted">{label}</dt>
      <dd className="nums mt-0.5 truncate text-[15px] font-medium">{value || "–"}</dd>
    </div>
  );
}
