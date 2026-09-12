import type {
  AppConfig,
  App,
  AppOptions,
  CheckResult,
  IfNothingFits,
  Kind,
  Library,
  OwnedTitle,
  Pick,
  Run,
  Service,
  SettingValues,
  Settings,
  Status,
  TestResult,
  TitleDetails,
  Verdict,
  VerdictFilter,
} from "./types";

export const MOCK = import.meta.env.VITE_MOCK === "1";

export class ApiError extends Error {
  readonly status: number;
  readonly fieldErrors: Record<string, string>;
  constructor(status: number, message: string, fieldErrors: Record<string, string> = {}) {
    super(message);
    this.status = status;
    this.fieldErrors = fieldErrors;
  }
}

/** True when the backend could not be reached at all (as opposed to an HTTP error). */
export function isUnreachable(err: unknown): boolean {
  return err instanceof ApiError && err.status === 0;
}

interface Envelope<T> {
  data: T;
  headers: Headers;
}

async function request<T>(method: "GET" | "POST" | "PUT", path: string, body?: unknown): Promise<T> {
  return (await send<T>(method, path, body)).data;
}

async function send<T>(method: "GET" | "POST" | "PUT", path: string, body?: unknown): Promise<Envelope<T>> {
  // Inline env check so production builds drop the mock chunk.
  if (import.meta.env.VITE_MOCK === "1") {
    const { mockFetch } = await import("./mock");
    const { data, headers } = await mockFetch(method, path, body);
    return { data: data as T, headers };
  }
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? { Accept: "application/json" } : { Accept: "application/json", "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "Proposarr is not reachable. Check that `proposarr serve` is running.");
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    let fieldErrors: Record<string, string> = {};
    try {
      const data = (await res.json()) as { error?: string; field_errors?: Record<string, string> };
      if (data.error) message = data.error;
      if (data.field_errors) fieldErrors = data.field_errors;
    } catch {
      // Not JSON; keep the status line.
    }
    // A proxy in front of a stopped backend answers 502/504.
    throw new ApiError(res.status === 502 || res.status === 504 ? 0 : res.status, message, fieldErrors);
  }
  if (res.status === 204) return { data: undefined as T, headers: res.headers };
  return { data: (await res.json()) as T, headers: res.headers };
}

export interface PicksQuery {
  kind?: Kind;
  /** `all` spans every succeeded run. */
  run?: "latest" | "all" | number;
  verdict?: VerdictFilter;
  /** Only picks whose latest request was added, newest request first. */
  added?: boolean;
  /** true: open-search runs only; false: taste runs only. */
  search?: boolean;
  /** One pick per title, the most recent. */
  distinct?: boolean;
  limit?: number;
  offset?: number;
}

export interface PicksPage {
  picks: Pick[];
  offset: number;
  /** Matching picks before limit/offset (X-Total-Count). */
  total: number;
}

function picksPath({ kind, run, verdict, added, search, distinct, limit, offset }: PicksQuery): string {
  const q = new URLSearchParams();
  if (kind) q.set("kind", kind);
  if (run !== undefined) q.set("run", String(run));
  if (verdict && verdict !== "all") q.set("verdict", verdict);
  if (added) q.set("added", "true");
  if (search !== undefined) q.set("search", String(search));
  if (distinct) q.set("distinct", "true");
  if (limit !== undefined) q.set("limit", String(limit));
  if (offset) q.set("offset", String(offset));
  return `/api/picks?${q}`;
}

export const api = {
  status: () => request<Status>("GET", "/api/status"),
  checks: () => request<CheckResult[]>("GET", "/api/connections/check"),
  config: () => request<AppConfig>("GET", "/api/config"),
  runs: (limit = 50) => request<Run[]>("GET", `/api/runs?limit=${limit}`),
  run: (id: number) => request<{ run: Run; picks: Pick[] }>("GET", `/api/runs/${id}`),
  startRun: (kind: Kind, vibe: string, useTaste = true) =>
    request<Run>("POST", "/api/runs", { kind, vibe: vibe.trim() || undefined, use_taste: useTaste }),
  picks: (q: PicksQuery) => request<Pick[]>("GET", picksPath(q)),
  /** One page of picks with the total from X-Total-Count. */
  picksPage: async (q: PicksQuery): Promise<PicksPage> => {
    const { data, headers } = await send<Pick[] | null>("GET", picksPath(q));
    const picks = data ?? [];
    const offset = q.offset ?? 0;
    const header = Number(headers.get("X-Total-Count"));
    // Servers without the header: assume another page exists while pages come back full.
    const total = headers.has("X-Total-Count") && Number.isFinite(header)
      ? header
      : offset + picks.length + (q.limit !== undefined && picks.length === q.limit ? 1 : 0);
    return { picks, offset, total };
  },
  title: (kind: Kind, tmdbId: number) => request<TitleDetails>("GET", `/api/titles/${kind}/${tmdbId}`),
  setVerdict: (id: number, verdict: Verdict | "", laterDays?: number) =>
    request<Pick>("POST", `/api/picks/${id}/verdict`, laterDays ? { verdict, later_days: laterDays } : { verdict }),
  appOptions: (app: App) => request<AppOptions>("GET", `/api/apps/${app}/options`),
  requestPick: (id: number, qualityProfileId: number, rootFolder?: string, ifNothingFits?: IfNothingFits) =>
    request<Pick>("POST", `/api/picks/${id}/request`, {
      quality_profile_id: qualityProfileId,
      root_folder: rootFolder || undefined,
      if_nothing_fits: ifNothingFits,
    }),
  switchProfile: (id: number, qualityProfileId: number) =>
    request<Pick>("POST", `/api/picks/${id}/request/profile`, { quality_profile_id: qualityProfileId }),
  owned: (runId: number) => request<OwnedTitle[] | null>("GET", `/api/runs/${runId}/owned`),
  searchOwnedMovie: (tmdbId: number, qualityProfileId: number, ifNothingFits?: IfNothingFits) =>
    request<OwnedTitle>("POST", `/api/library/movies/${tmdbId}/search`, { quality_profile_id: qualityProfileId, if_nothing_fits: ifNothingFits }),
  library: (kind: Kind) => request<Library>("GET", `/api/library?kind=${kind}`),
  settings: () => request<Settings>("GET", "/api/settings"),
  saveSettings: (values: SettingValues) => request<Settings>("PUT", "/api/settings", { values }),
  testService: (service: Service, values: SettingValues) => request<TestResult>("POST", "/api/settings/test", { service, values }),
};
