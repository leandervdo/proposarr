import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";
import { useCallback, useSyncExternalStore } from "react";
import { ApiError, api, type PicksPage, type PicksQuery } from "./client";
import type { App, IfNothingFits, Kind, Library, LibraryTitle, Pick, Service, SettingValues, Verdict } from "./types";

export const keys = {
  status: ["status"] as const,
  config: ["config"] as const,
  settings: ["settings"] as const,
  runs: ["runs"] as const,
  run: (id: number) => ["runs", id] as const,
  picks: (q: PicksQuery) => ["picks", q] as const,
  /** Paged pick lists (Collection). Under "picks" so run and verdict updates reach them. */
  pickPages: (q: Omit<PicksQuery, "offset">) => ["picks", "pages", q] as const,
  library: (kind: Kind) => ["library", kind] as const,
  options: (app: App) => ["options", app] as const,
  title: (kind: Kind, tmdbId: number) => ["title", kind, tmdbId] as const,
};

/** What can sit under the "picks" key: a plain list or paged lists. */
type PicksData = Pick[] | InfiniteData<PicksPage, number>;

function mapPicksData(old: PicksData | undefined, fn: (p: Pick) => Pick): PicksData | undefined {
  if (!old) return old;
  if (Array.isArray(old)) return old.map(fn);
  return { ...old, pages: old.pages.map((page) => ({ ...page, picks: page.picks.map(fn) })) };
}

export const useStatus = () => useQuery({ queryKey: keys.status, queryFn: api.status, refetchInterval: 60_000 });
export const useConfig = () => useQuery({ queryKey: keys.config, queryFn: api.config });
export const useRuns = () => useQuery({ queryKey: keys.runs, queryFn: () => api.runs(50) });
export const useRun = (id: number | undefined) =>
  useQuery({ queryKey: keys.run(id ?? 0), queryFn: () => api.run(id!), enabled: id !== undefined });
export const usePicks = (q: PicksQuery) => useQuery({ queryKey: keys.picks(q), queryFn: () => api.picks(q) });
export const useLibrary = (kind: Kind) => useQuery({ queryKey: keys.library(kind), queryFn: () => api.library(kind) });
export const useAppOptions = (app: App, enabled: boolean) =>
  useQuery({ queryKey: keys.options(app), queryFn: () => api.appOptions(app), enabled, staleTime: 5 * 60_000 });

export const useChecks = () => useMutation({ mutationFn: api.checks });

export const useSettings = () => useQuery({ queryKey: keys.settings, queryFn: api.settings });

export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (values: SettingValues) => api.saveSettings(values),
    onSuccess: (settings) => {
      qc.setQueryData(keys.settings, settings);
      void qc.invalidateQueries({ queryKey: keys.status });
      void qc.invalidateQueries({ queryKey: keys.config });
      void qc.invalidateQueries({ queryKey: ["options"] });
    },
  });
}

export const useTestService = () =>
  useMutation({ mutationFn: ({ service, values }: { service: Service; values: SettingValues }) => api.testService(service, values) });

export function useStartRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ kind, vibe, useTaste = true }: { kind: Kind; vibe: string; useTaste?: boolean }) => api.startRun(kind, vibe, useTaste),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.runs });
      void qc.invalidateQueries({ queryKey: keys.status });
    },
  });
}

/** Replaces a pick (and its verdict on other runs' copies of the same title) in every cached list. */
export function applyPick(qc: QueryClient, pick: Pick) {
  qc.setQueriesData<PicksData>({ queryKey: ["picks"] }, (old) =>
    mapPicksData(old, (p) =>
      p.id === pick.id
        ? pick
        : p.tmdb_id === pick.tmdb_id && p.kind === pick.kind
          ? { ...p, verdict: pick.verdict, verdict_at: pick.verdict_at, later_until: pick.later_until }
          : p,
    ),
  );
  qc.setQueriesData<{ run: unknown; picks: Pick[] }>({ queryKey: ["runs"] }, (old) =>
    old && "picks" in old ? { ...old, picks: old.picks.map((p) => (p.id === pick.id ? pick : p)) } : old,
  );
}

export function useSetVerdict() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ pick, verdict }: { pick: Pick; verdict: Verdict | "" }) =>
      api.setVerdict(pick.id, verdict, verdict === "later" ? 30 : undefined),
    onMutate: ({ pick, verdict }) => {
      applyPick(qc, { ...pick, verdict: verdict || undefined, verdict_at: verdict ? new Date().toISOString() : undefined });
    },
    onSuccess: (pick) => applyPick(qc, pick),
    onError: (_err, { pick }) => applyPick(qc, pick),
  });
}

export function useRequestPick() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ pick, qualityProfileId, rootFolder, ifNothingFits }: { pick: Pick; qualityProfileId: number; rootFolder?: string; ifNothingFits?: IfNothingFits }) =>
      api.requestPick(pick.id, qualityProfileId, rootFolder, ifNothingFits),
    onSuccess: (updated, { pick }) => {
      applyPick(qc, updated);
      void qc.invalidateQueries({ queryKey: ["library"] });
      // A new request joins the Collection's Added list; the title is now in the library.
      void qc.invalidateQueries({ queryKey: ["picks", "pages"] });
      void qc.invalidateQueries({ queryKey: keys.title(pick.kind, pick.tmdb_id) });
    },
  });
}

const PAGE_SIZE = 200;

/** Picks in pages of 200, for "Load more". */
export function usePickPages(q: Omit<PicksQuery, "offset" | "limit">) {
  return useInfiniteQuery({
    queryKey: keys.pickPages({ ...q, limit: PAGE_SIZE }),
    queryFn: ({ pageParam }) => api.picksPage({ ...q, limit: PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last) => {
      const next = last.offset + last.picks.length;
      return last.picks.length > 0 && next < last.total ? next : undefined;
    },
  });
}

/** Details for the title modal. The server caches TMDB for an hour, so this can stay fresh for a while. */
export const useTitleDetails = (kind: Kind | undefined, tmdbId: number | undefined) =>
  useQuery({
    queryKey: keys.title(kind ?? "movies", tmdbId ?? 0),
    queryFn: () => api.title(kind!, tmdbId!),
    enabled: kind !== undefined && tmdbId !== undefined,
    staleTime: 10 * 60_000,
    retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 1,
  });

function findCachedPick(qc: QueryClient, id: number): Pick | undefined {
  for (const [, data] of qc.getQueriesData<PicksData>({ queryKey: ["picks"] })) {
    if (!data) continue;
    const lists = Array.isArray(data) ? [data] : data.pages.map((page) => page.picks);
    for (const list of lists) {
      const found = list.find((p) => p.id === id);
      if (found) return found;
    }
  }
  for (const [, data] of qc.getQueriesData<unknown>({ queryKey: ["runs"] })) {
    if (data && typeof data === "object" && "picks" in data && Array.isArray(data.picks)) {
      const found = (data.picks as Pick[]).find((p) => p.id === id);
      if (found) return found;
    }
  }
  return undefined;
}

/**
 * A pick by id, kept live from whatever list already holds it. A shared link can land before any list is
 * loaded; then every pick of that kind is fetched once to find it.
 */
export function usePick(id: number | undefined, kind: Kind | undefined) {
  const qc = useQueryClient();
  const subscribe = useCallback((onChange: () => void) => qc.getQueryCache().subscribe(onChange), [qc]);
  const snapshot = useCallback(() => (id === undefined ? undefined : findCachedPick(qc, id)), [qc, id]);
  const cached = useSyncExternalStore(subscribe, snapshot);
  const fallback = useQuery({
    queryKey: keys.picks({ kind, run: "all", limit: 1000 }),
    queryFn: () => api.picks({ kind, run: "all", limit: 1000 }),
    enabled: id !== undefined && kind !== undefined && !cached,
  });
  return { pick: cached, isPending: id !== undefined && !cached && fallback.isPending && fallback.fetchStatus !== "idle" };
}

/** A library title already in the cache, for the modal's fallback when details fail. */
export function useCachedLibraryTitle(kind: Kind | undefined, tmdbId: number | undefined): LibraryTitle | undefined {
  const qc = useQueryClient();
  if (!kind || tmdbId === undefined) return undefined;
  return qc.getQueryData<Library>(keys.library(kind))?.titles?.find((t) => t.tmdb_id === tmdbId);
}

/** Moves an added movie to another quality profile; Radarr searches again and the check restarts. */
export function useSwitchProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ pick, qualityProfileId }: { pick: Pick; qualityProfileId: number }) => api.switchProfile(pick.id, qualityProfileId),
    onSuccess: (updated) => applyPick(qc, updated),
  });
}
