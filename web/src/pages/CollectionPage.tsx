import * as Tabs from "@radix-ui/react-tabs";
import { ArrowRight, Check, Clock, EyeOff, Loader2, Plus, Search } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { usePickPages } from "@/api/queries";
import type { Kind, Pick } from "@/api/types";
import { EmptyState, ErrorNote } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { RatingChips } from "@/components/PickMeta";
import { Poster } from "@/components/Poster";
import { SelectCheck, type CardSelection } from "@/components/SelectCheck";
import { SelectionBar } from "@/components/SelectionBar";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { appFor, appName, dateLabel, plural, relativeTime } from "@/lib/format";
import { useTitleLink } from "@/lib/titleModal";
import { useSelection, type Selection } from "@/lib/useSelection";
import { cn } from "@/lib/utils";

type PagesQuery = ReturnType<typeof usePickPages>;

/** Stable empty list for the selection while the Added tab is open. */
const NO_PICKS: Pick[] = [];

const GRID = "grid grid-cols-2 gap-x-4 gap-y-9 sm:grid-cols-3 sm:gap-x-5 md:grid-cols-4 lg:grid-cols-5 2xl:grid-cols-7";

export function CollectionPage() {
  const [params, setParams] = useSearchParams();
  const kind: Kind = params.get("kind") === "series" ? "series" : "movies";
  const tab = params.get("tab") === "searches" ? "searches" : "added";
  const added = usePickPages({ kind, run: "all", added: true, distinct: true });
  const searches = usePickPages({ kind, run: "all", search: true });
  const app = appName(appFor(kind));
  const groups = useSearchGroups(searches);
  const searchPicks = useMemo(() => groups.flatMap((g) => g.picks), [groups]);
  // Only the Searches tab selects; another kind or tab starts empty.
  const selection = useSelection(tab === "searches" ? searchPicks : NO_PICKS, `${kind}:${tab}`);

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

  return (
    <>
      <PageHeader
        title="Collection"
        actions={
          <Segmented
            label="Library"
            value={kind}
            onChange={(k) => update({ kind: k === "movies" ? null : k })}
            options={[
              { value: "movies", label: "Movies" },
              { value: "series", label: "Series" },
            ]}
          />
        }
      >
        Everything you added to {app} through Proposarr, and every title your open searches turned up.
      </PageHeader>

      <Tabs.Root value={tab} onValueChange={(v) => update({ tab: v === "searches" ? "searches" : null })}>
        <Tabs.List aria-label="Collection" className="mb-8 flex gap-8 border-b border-border">
          <TabTrigger value="added" label="Added" count={added.data?.pages[0]?.total} />
          <TabTrigger value="searches" label="Searches" count={searches.data?.pages[0]?.total} />
        </Tabs.List>
        <Tabs.Content value="added" className="focus-visible:outline-none">
          <AddedPanel query={added} kind={kind} />
        </Tabs.Content>
        <Tabs.Content value="searches" className="focus-visible:outline-none">
          <SearchesPanel query={searches} groups={groups} kind={kind} selection={selection} />
        </Tabs.Content>
      </Tabs.Root>

      {tab === "searches" && <SelectionBar selection={selection} kind={kind} />}
    </>
  );
}

function TabTrigger({ value, label, count }: { value: string; label: string; count?: number }) {
  return (
    <Tabs.Trigger
      value={value}
      className={cn(
        "group -mb-px inline-flex h-12 items-center gap-2.5 border-b-2 border-transparent font-display text-[26px] leading-none font-bold tracking-tight text-text-muted transition-colors",
        "hover:text-text data-[state=active]:border-accent data-[state=active]:text-text",
      )}
    >
      {label}
      {count === undefined ? (
        <span aria-hidden className="h-5 w-7 animate-pulse rounded-full bg-surface-raised" />
      ) : (
        <span className="nums rounded-full bg-surface-raised px-2 py-0.5 font-sans text-xs font-semibold text-text-muted group-data-[state=active]:bg-accent-soft group-data-[state=active]:text-accent">
          {count}
        </span>
      )}
    </Tabs.Trigger>
  );
}

function AddedPanel({ query, kind }: { query: PagesQuery; kind: Kind }) {
  const app = appName(appFor(kind));
  const picks = useMemo(() => query.data?.pages.flatMap((p) => p.picks) ?? [], [query.data]);
  if (query.isPending) return <GridSkeleton />;
  if (query.isError) return <ErrorNote title="Could not load added titles" message={query.error.message} onRetry={() => void query.refetch()} />;
  if (picks.length === 0) return <AddedEmpty kind={kind} />;
  const total = query.data.pages[0]?.total ?? picks.length;
  return (
    <>
      <p className="mb-6 text-sm text-text-muted">
        {plural(total, kind === "series" ? "series" : "movie", kind === "series" ? "series" : "movies")} added to {app} through Proposarr, newest first.
      </p>
      <ul className={GRID}>
        {picks.map((p) => (
          <li key={p.id}>
            <CollectionCard
              pick={p}
              footer={
                p.request && (
                  <>
                    Added <time dateTime={p.request.requested_at} title={dateLabel(p.request.requested_at)}>{relativeTime(p.request.requested_at)}</time> ·{" "}
                    {p.request.quality_profile}
                  </>
                )
              }
            />
          </li>
        ))}
      </ul>
      <LoadMore query={query} loaded={picks.length} total={total} />
    </>
  );
}

interface SearchGroup {
  runId: number;
  vibe?: string;
  foundAt: string;
  picks: Pick[];
}

/** Open-search picks grouped by search, newest search first. */
function useSearchGroups(query: PagesQuery): SearchGroup[] {
  return useMemo(() => {
    const byRun = new Map<number, SearchGroup>();
    for (const p of query.data?.pages.flatMap((page) => page.picks) ?? []) {
      const group = byRun.get(p.run_id) ?? { runId: p.run_id, vibe: p.run_vibe, foundAt: p.found_at, picks: [] };
      group.picks.push(p);
      byRun.set(p.run_id, group);
    }
    return [...byRun.values()].sort((a, b) => (b.foundAt ?? "").localeCompare(a.foundAt ?? "") || b.runId - a.runId);
  }, [query.data]);
}

function SearchesPanel({ query, groups, kind, selection }: { query: PagesQuery; groups: SearchGroup[]; kind: Kind; selection: Selection<Pick> }) {
  if (query.isPending) return <GridSkeleton />;
  if (query.isError) return <ErrorNote title="Could not load searches" message={query.error.message} onRetry={() => void query.refetch()} />;
  if (groups.length === 0) {
    return (
      <EmptyState
        icon={Search}
        title="No searches yet"
        actions={
          <Button asChild variant="primary">
            <Link to={`/?${new URLSearchParams({ ...(kind === "series" ? { kind } : {}), mode: "search" })}`}>
              <Search /> Try an open search
            </Link>
          </Button>
        }
      >
        <p>
          Switch off “Use my taste” on the Picks page and describe what you are after, like “{kind === "series" ? "short Korean thrillers" : "90s heist movies with a twist ending"}”.
          Every title a search finds is kept here, grouped by search.
        </p>
      </EmptyState>
    );
  }
  const loaded = groups.reduce((n, g) => n + g.picks.length, 0);
  const total = query.data.pages[0]?.total ?? loaded;
  const more = query.hasNextPage;

  return (
    <>
      <p className="mb-2 text-sm text-text-muted">
        {plural(total, "title")} from {more ? "at least " : ""}
        {plural(groups.length, "search", "searches")}, newest search first.
      </p>
      <div className="flex flex-col">
        {groups.map((g) => (
          <section key={g.runId} aria-labelledby={`search-${g.runId}`} className="border-b border-border pt-8 pb-10 last:border-b-0">
            <header className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
              <div className="min-w-0">
                <h2 id={`search-${g.runId}`} className="font-display text-[30px] leading-[0.95] font-bold tracking-tight text-balance break-words sm:text-[38px]">
                  “{g.vibe || "Untitled search"}”
                </h2>
                <p className="nums mt-2 text-sm text-text-muted">
                  Searched <time dateTime={g.foundAt} title={relativeTime(g.foundAt)}>{dateLabel(g.foundAt) || "on an unknown date"}</time> ·{" "}
                  {plural(g.picks.length, "pick")}
                </p>
              </div>
              <Button asChild variant="secondary" size="sm" className="self-start sm:self-auto">
                <Link to={`/?kind=${kind}&run=${g.runId}`}>
                  Show in Picks <ArrowRight />
                </Link>
              </Button>
            </header>
            <ul className={GRID}>
              {g.picks.map((p) => (
                <li key={p.id}>
                  <CollectionCard
                    pick={p}
                    showState
                    selection={{ selected: selection.isSelected(p.id), active: selection.active, onToggle: (range) => selection.toggle(p.id, range) }}
                  />
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <LoadMore query={query} loaded={loaded} total={total} />
    </>
  );
}

function CollectionCard({
  pick,
  footer,
  showState = false,
  selection,
}: {
  pick: Pick;
  footer?: ReactNode;
  showState?: boolean;
  /** Multi-select (Searches tab): a checkbox on the corner, and clicks toggle while anything is selected. */
  selection?: CardSelection;
}) {
  const titleLink = useTitleLink();
  const link = titleLink({ kind: pick.kind, tmdbId: pick.tmdb_id, pickId: pick.id });
  const selecting = selection?.active ?? false;
  return (
    // The checkbox sits beside the link rather than in it: a button inside a link is not allowed.
    <div className="group/select relative">
      {selection && (
        <SelectCheck title={pick.title} checked={selection.selected} shown={selecting} onToggle={selection.onToggle} className="group-hover/select:-translate-y-1" />
      )}
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
        <div
          className={cn(
            "relative rounded-[var(--radius-poster)] ring-offset-[3px] ring-offset-background transition-[translate,box-shadow] duration-300 group-hover:-translate-y-1",
            selection?.selected && "ring-2 ring-accent",
          )}
        >
          <Poster
            src={pick.poster_url}
            title={pick.title}
            className={cn(
              "transition-[filter,opacity,box-shadow] duration-300 group-hover:shadow-[0_18px_40px_-18px_rgb(0_0_0/0.7)]",
              showState && pick.verdict === "ignored" && "opacity-55 grayscale",
            )}
          />
          {showState && <StatePill pick={pick} />}
        </div>
        <div className="mt-3 flex min-w-0 flex-col gap-1.5 px-0.5">
          <p className="text-[15px] leading-tight font-semibold">
            <span className="line-clamp-1 decoration-text-muted/60 underline-offset-4 group-hover:underline">{pick.title}</span>
            {pick.year && <span className="nums text-[13px] font-normal text-text-muted">{pick.year}</span>}
          </p>
          <RatingChips ratings={pick.ratings} />
          {footer && <p className="text-[13px] leading-snug text-text-muted">{footer}</p>}
        </div>
      </Link>
    </div>
  );
}

function StatePill({ pick }: { pick: Pick }) {
  const state =
    pick.request?.status === "added"
      ? { icon: Check, label: "Added", tone: "text-[#7fd6bc]" }
      : pick.verdict === "accepted"
        ? { icon: Check, label: "Accepted", tone: "text-white" }
        : pick.verdict === "later"
          ? { icon: Clock, label: "Later", tone: "text-white" }
          : pick.verdict === "ignored"
            ? { icon: EyeOff, label: "Ignored", tone: "text-white" }
            : null;
  if (!state) return null;
  const Icon = state.icon;
  return (
    <span className="absolute top-2.5 left-2.5 inline-flex items-center gap-1 rounded-full bg-black/60 py-0.5 pr-2 pl-1.5 text-[11px] font-medium text-white backdrop-blur-md">
      <Icon aria-hidden className={cn("size-3", state.tone)} strokeWidth={2.5} />
      {state.label}
    </span>
  );
}

function LoadMore({ query, loaded, total }: { query: PagesQuery; loaded: number; total: number }) {
  if (!query.hasNextPage) return null;
  return (
    <div className="mt-12 flex flex-col items-center gap-2">
      <Button onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
        {query.isFetchingNextPage ? (
          <>
            <Loader2 className="animate-spin" /> Loading…
          </>
        ) : (
          "Load more"
        )}
      </Button>
      <p className="nums text-xs text-text-muted">
        Showing {loaded} of {total}
      </p>
    </div>
  );
}

function AddedEmpty({ kind }: { kind: Kind }) {
  const app = appName(appFor(kind));
  const noun = kind === "series" ? "series" : "movies";
  return (
    <section className="mx-auto flex max-w-xl flex-col items-center px-4 py-14 text-center sm:py-20">
      {/* Three empty poster frames waiting for a title. */}
      <div aria-hidden className="relative h-40 w-60">
        <div className="absolute top-3 left-4 aspect-[2/3] w-[5.5rem] -rotate-[9deg] rounded-[var(--radius-poster)] border border-dashed border-border bg-surface" />
        <div className="absolute top-3 right-4 aspect-[2/3] w-[5.5rem] rotate-[9deg] rounded-[var(--radius-poster)] border border-dashed border-border bg-surface" />
        <div className="absolute top-0 left-1/2 grid aspect-[2/3] w-[6.25rem] -translate-x-1/2 place-items-center rounded-[var(--radius-poster)] border border-border bg-surface-raised shadow-[0_24px_48px_-24px_rgb(0_0_0/0.6)]">
          <span className="grid size-11 place-items-center rounded-full bg-accent text-accent-contrast">
            <Plus className="size-5" strokeWidth={2.5} />
          </span>
        </div>
      </div>
      <h2 className="mt-8 font-display text-[34px] leading-none font-bold tracking-tight">Nothing added yet</h2>
      <p className="mt-3 text-[15px] leading-relaxed text-text-muted">
        When you add {noun} from Picks they land here, with the quality profile you chose and when you added them. Proposarr keeps the list even
        after the picks are gone.
      </p>
      <ol className="mt-6 flex flex-wrap justify-center gap-x-5 gap-y-2 text-sm text-text-muted">
        {["Open Picks", "Press Add on a title", `Choose a ${app} quality profile`].map((step, i) => (
          <li key={step} className="flex items-center gap-2">
            <span className="nums grid size-5 place-items-center rounded-full border border-border text-[11px] font-semibold text-text">{i + 1}</span>
            {step}
          </li>
        ))}
      </ol>
      <Button asChild variant="primary" className="mt-8">
        <Link to={kind === "series" ? "/?kind=series" : "/"}>Go to Picks</Link>
      </Button>
    </section>
  );
}

function GridSkeleton() {
  return (
    <div className={GRID} aria-busy aria-label="Loading">
      {Array.from({ length: 10 }, (_, i) => (
        <div key={i}>
          <div className="aspect-[2/3] animate-pulse rounded-[var(--radius-poster)] bg-surface-raised" />
          <div className="mt-3 h-4 w-3/4 animate-pulse rounded bg-surface-raised" />
          <div className="mt-2 h-3 w-1/2 animate-pulse rounded bg-surface-raised" />
        </div>
      ))}
    </div>
  );
}
