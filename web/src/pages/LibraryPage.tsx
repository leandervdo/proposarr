import { Library as LibraryIcon, Search } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useLibrary } from "@/api/queries";
import type { Kind, Profile, Signal } from "@/api/types";
import { EmptyState, ErrorNote } from "@/components/EmptyState";
import { PageHeader, SectionTitle } from "@/components/PageHeader";
import { Poster } from "@/components/Poster";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { appFor, appName, relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

export function LibraryPage() {
  const [params, setParams] = useSearchParams();
  const kind: Kind = params.get("kind") === "series" ? "series" : "movies";
  const library = useLibrary(kind);
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query);
  const app = appName(appFor(kind));

  const titles = useMemo(() => {
    const list = [...(library.data?.titles ?? [])].sort((a, b) => a.title.localeCompare(b.title));
    const q = deferred.trim().toLowerCase();
    return q ? list.filter((t) => t.title.toLowerCase().includes(q)) : list;
  }, [library.data, deferred]);

  return (
    <>
      <PageHeader
        title="Library"
        actions={
          <Segmented
            label="Library"
            value={kind}
            onChange={(k) => setParams(k === "movies" ? {} : { kind: k }, { replace: true })}
            options={[
              { value: "movies", label: "Movies" },
              { value: "series", label: "Series" },
            ]}
          />
        }
      >
        What Proposarr knows about your taste: the profile from your most recent run and the {app} titles behind it.
      </PageHeader>

      {library.isPending ? (
        <div className="grid gap-8 xl:grid-cols-[22rem_1fr]" aria-busy>
          <div className="h-96 animate-pulse rounded-[var(--radius-panel)] bg-surface" />
          <div className="h-96 animate-pulse rounded-[var(--radius-panel)] bg-surface" />
        </div>
      ) : library.isError ? (
        <ErrorNote title={`Could not read the ${app} library`} message={library.error.message} onRetry={() => void library.refetch()} />
      ) : (library.data.titles ?? []).length === 0 && !library.data.profile ? (
        <EmptyState
          icon={LibraryIcon}
          title={`Nothing from ${app} yet`}
          actions={
            <Button asChild variant="primary">
              <Link to="/connections">Check connections</Link>
            </Button>
          }
        >
          <p>
            Once {app} is connected and has titles, they show up here together with the taste profile Proposarr builds from your watch history.
          </p>
        </EmptyState>
      ) : (
        <div className="grid items-start gap-10 xl:grid-cols-[23rem_1fr] xl:gap-12">
          <ProfilePanel profile={library.data.profile} app={app} />
          <section aria-labelledby="titles-heading">
            <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <h2 id="titles-heading" className="font-display text-[26px] leading-none font-bold tracking-tight">
                In {app} <span className="nums text-text-muted">{library.data.titles?.length ?? 0}</span>
              </h2>
              <label className="relative block sm:w-72">
                <span className="sr-only">Search titles</span>
                <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-text-muted" />
                <input
                  type="search"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search titles"
                  className="h-10 w-full rounded-[var(--radius-control)] border border-border bg-surface pr-3 pl-9 text-[15px] placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
              </label>
            </div>
            {titles.length === 0 ? (
              <p className="py-12 text-center text-text-muted">No titles match “{query}”.</p>
            ) : (
              <ul className="grid grid-cols-3 gap-x-3 gap-y-5 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-5 2xl:grid-cols-7">
                {titles.map((t) => (
                  <li key={t.tmdb_id ?? t.tvdb_id ?? t.title}>
                    <Poster src={t.poster_url} title={t.title} className="rounded-md" />
                    <p className="mt-2 line-clamp-1 text-[13px] font-medium" title={t.title}>
                      {t.title}
                    </p>
                    <p className="nums text-xs text-text-muted">{t.year ?? ""}</p>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </>
  );
}

const SIGNAL: Record<Signal, { label: string; className: string }> = {
  rewatched: { label: "Rewatched", className: "border-accent/50 text-accent" },
  watched: { label: "Watched", className: "border-success/45 text-success" },
  partial: { label: "Partly watched", className: "border-warning/45 text-warning" },
  owned: { label: "Owned", className: "border-border text-text-muted" },
};

function ProfilePanel({ profile, app }: { profile: Profile | null; app: string }) {
  if (!profile) {
    return (
      <aside className="rounded-[var(--radius-panel)] border border-dashed border-border p-6 text-[15px] text-text-muted">
        <SectionTitle>Taste profile</SectionTitle>
        <p>Run recommendations once and the profile that fed Claude appears here: your most-watched titles and the genres they lean towards.</p>
      </aside>
    );
  }
  const genres = profile.genres ?? [];
  const top = profile.top ?? [];
  const maxShare = Math.max(...genres.map((g) => g.share), 0.01);

  return (
    <aside className="rounded-[var(--radius-panel)] border border-border bg-surface p-5 sm:p-6 xl:sticky xl:top-10">
      <SectionTitle>Taste profile</SectionTitle>
      <p className="-mt-2 text-sm text-text-muted">
        {profile.library_count} titles in {app}, {profile.history_count} watched in the history window.
      </p>

      {genres.length > 0 && (
        <div className="mt-6">
          <h3 className="text-sm font-semibold">Genres you lean towards</h3>
          <ul className="mt-3 flex flex-col gap-2.5">
            {genres.map((g) => (
              <li key={g.name} className="grid grid-cols-[7.5rem_1fr_2.5rem] items-center gap-3 text-sm">
                <span className="truncate text-text-muted">{g.name}</span>
                <span className="h-2 overflow-hidden rounded-full bg-surface-raised">
                  <span className="block h-full rounded-full bg-accent" style={{ width: `${(g.share / maxShare) * 100}%`, opacity: 0.45 + 0.55 * (g.share / maxShare) }} />
                </span>
                <span className="nums text-right text-text-muted">{Math.round(g.share * 100)}%</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {top.length > 0 && (
        <div className="mt-7">
          <h3 className="text-sm font-semibold">Titles that weigh most</h3>
          <ul className="mt-2 divide-y divide-border">
            {top.map((e) => (
              <li key={`${e.title}-${e.year}`} className="flex items-center gap-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[15px]">
                    {e.title} {e.year ? <span className="nums text-text-muted">{e.year}</span> : null}
                  </p>
                  {e.signal !== "owned" && <p className="text-xs text-text-muted">Last watched {relativeTime(e.last_watched)}</p>}
                </div>
                <span className={cn("shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium", SIGNAL[e.signal].className)}>
                  {SIGNAL[e.signal].label}
                </span>
                <span className="nums w-7 shrink-0 text-right text-sm text-text-muted" title="Weight in the profile">
                  {e.weight}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </aside>
  );
}
