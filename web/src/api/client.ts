import type {
  AppConfig,
  App,
  AppOptions,
  CheckResult,
  Kind,
  Library,
  Pick,
  Run,
  Service,
  SettingValues,
  Settings,
  Status,
  TestResult,
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

async function request<T>(method: "GET" | "POST" | "PUT", path: string, body?: unknown): Promise<T> {
  // Inline env check so production builds drop the mock chunk.
  if (import.meta.env.VITE_MOCK === "1") {
    const { mockFetch } = await import("./mock");
    return mockFetch(method, path, body) as Promise<T>;
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
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export interface PicksQuery {
  kind?: Kind;
  run?: "latest" | number;
  verdict?: VerdictFilter;
}

export const api = {
  status: () => request<Status>("GET", "/api/status"),
  checks: () => request<CheckResult[]>("GET", "/api/connections/check"),
  config: () => request<AppConfig>("GET", "/api/config"),
  runs: (limit = 50) => request<Run[]>("GET", `/api/runs?limit=${limit}`),
  run: (id: number) => request<{ run: Run; picks: Pick[] }>("GET", `/api/runs/${id}`),
  startRun: (kind: Kind, vibe: string) => request<Run>("POST", "/api/runs", { kind, vibe: vibe.trim() || undefined }),
  picks: ({ kind, run, verdict }: PicksQuery) => {
    const q = new URLSearchParams();
    if (kind) q.set("kind", kind);
    if (run !== undefined) q.set("run", String(run));
    if (verdict && verdict !== "all") q.set("verdict", verdict);
    return request<Pick[]>("GET", `/api/picks?${q}`);
  },
  setVerdict: (id: number, verdict: Verdict | "", laterDays?: number) =>
    request<Pick>("POST", `/api/picks/${id}/verdict`, laterDays ? { verdict, later_days: laterDays } : { verdict }),
  appOptions: (app: App) => request<AppOptions>("GET", `/api/apps/${app}/options`),
  requestPick: (id: number, qualityProfileId: number, rootFolder?: string) =>
    request<Pick>("POST", `/api/picks/${id}/request`, rootFolder ? { quality_profile_id: qualityProfileId, root_folder: rootFolder } : { quality_profile_id: qualityProfileId }),
  library: (kind: Kind) => request<Library>("GET", `/api/library?kind=${kind}`),
  settings: () => request<Settings>("GET", "/api/settings"),
  saveSettings: (values: SettingValues) => request<Settings>("PUT", "/api/settings", { values }),
  testService: (service: Service, values: SettingValues) => request<TestResult>("POST", "/api/settings/test", { service, values }),
};
