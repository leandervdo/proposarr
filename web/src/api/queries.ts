import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, type PicksQuery } from "./client";
import type { App, Kind, Pick, Service, SettingValues, Verdict } from "./types";

export const keys = {
  status: ["status"] as const,
  config: ["config"] as const,
  settings: ["settings"] as const,
  runs: ["runs"] as const,
  run: (id: number) => ["runs", id] as const,
  picks: (q: PicksQuery) => ["picks", q] as const,
  library: (kind: Kind) => ["library", kind] as const,
  options: (app: App) => ["options", app] as const,
};

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
  qc.setQueriesData<Pick[]>({ queryKey: ["picks"] }, (old) =>
    old?.map((p) =>
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
    mutationFn: ({ pick, qualityProfileId, rootFolder }: { pick: Pick; qualityProfileId: number; rootFolder?: string }) =>
      api.requestPick(pick.id, qualityProfileId, rootFolder),
    onSuccess: (pick) => {
      applyPick(qc, pick);
      void qc.invalidateQueries({ queryKey: ["library"] });
    },
  });
}
