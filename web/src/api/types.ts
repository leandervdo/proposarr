// Mirrors docs/API.md.

export type Kind = "movies" | "series";
export type RunStatus = "running" | "succeeded" | "failed" | "rate_limited";
export type Verdict = "accepted" | "ignored" | "later";
export type App = "radarr" | "sonarr";

export interface Rejected {
  tmdb_id?: number;
  title: string;
  reason: string;
}

export interface Run {
  id: number;
  kind: Kind;
  vibe?: string;
  model: string;
  effort: string;
  status: RunStatus;
  error?: string;
  started_at: string;
  finished_at?: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  num_turns: number;
  session_id?: string;
  library_count: number;
  history_count: number;
  candidate_count: number;
  pick_count: number;
  warnings: string[] | null;
  rejected: Rejected[] | null;
  profile?: Profile;
}

export interface Request {
  pick_id: number;
  app: App;
  target_id?: number;
  quality_profile: string;
  root_folder: string;
  status: "added" | "failed";
  error?: string;
  requested_at: string;
}

export interface Pick {
  id: number;
  run_id: number;
  tmdb_id: number;
  kind: Kind;
  title: string;
  year?: number;
  reason: string;
  related_to: string[] | null;
  score: number;
  source: "candidate" | "free";
  overview?: string;
  genres?: string[];
  rating?: number;
  streaming?: string[];
  poster_url?: string;
  verdict?: Verdict;
  verdict_at?: string;
  later_until?: string;
  request?: Request;
}

export type Signal = "rewatched" | "watched" | "partial" | "owned";

export interface ProfileEntry {
  tmdb_id?: number;
  title: string;
  year?: number;
  genres?: string[];
  weight: number;
  signal: Signal;
  in_library: boolean;
  last_watched: string;
  added: string;
}

export interface Profile {
  kind: Kind;
  top: ProfileEntry[] | null;
  genres: { name: string; share: number }[] | null;
  library_count: number;
  history_count: number;
}

export interface LibraryTitle {
  tmdb_id?: number;
  tvdb_id?: number;
  title: string;
  year?: number;
  genres?: string[];
  added: string;
  poster_url?: string;
}

export interface Library {
  kind: Kind;
  titles: LibraryTitle[] | null;
  profile: Profile | null;
}

export interface QualityProfile {
  id: number;
  name: string;
}

export interface RootFolder {
  id: number;
  path: string;
  free_space: number;
}

export interface AppOptions {
  quality_profiles: QualityProfile[];
  root_folders: RootFolder[];
  default_root_folder: string;
}

export interface RunningRun {
  run_id: number;
  kind: Kind;
  message: string;
}

export interface Status {
  version: string;
  claude_auth: "oauth_token" | "api_key" | "local";
  connections: Record<"radarr" | "sonarr" | "tmdb" | "plex" | "jellyfin", boolean>;
  running: RunningRun[] | null;
  setup_required?: boolean;
  missing?: string[] | null;
}

export type SettingSource = "default" | "ui" | "file" | "env";

export interface SettingField {
  value?: string | number;
  secret: boolean;
  set: boolean;
  source: SettingSource;
  locked: boolean;
  env: string;
  hint?: string;
}

export interface Settings {
  fields: Record<string, SettingField>;
  read_only: { listen: string; data_dir: string; claude_bin: string; web_auth: boolean; config_file?: string };
}

export type SettingValues = Record<string, string | number | null>;

export type Service = "radarr" | "sonarr" | "plex" | "jellyfin" | "tmdb" | "claude";

export interface TestResult {
  status: "ok" | "fail";
  detail: string;
  discovered_api_key?: boolean;
}

export interface CheckResult {
  name: string;
  status: "ok" | "fail" | "skip";
  detail: string;
}

// /api/config is loosely specified; every field is optional.
export interface AppConfig {
  listen?: string;
  data_dir?: string;
  history_days?: number;
  snapshot_ttl?: string;
  radarr?: { url?: string; api_key_set?: boolean; root_folder?: string; minimum_availability?: string };
  sonarr?: { url?: string; api_key_set?: boolean; root_folder?: string };
  plex?: { url?: string; token_set?: boolean };
  jellyfin?: { url?: string; api_key_set?: boolean; user_id?: string };
  tmdb?: { api_key_set?: boolean; region?: string };
  claude?: { bin?: string; auth?: string; timeout?: string; max_budget_usd?: number };
  web?: { auth_enabled?: boolean; username_set?: boolean };
  movies?: KindConfig;
  series?: KindConfig;
  [key: string]: unknown;
}

export interface KindConfig {
  model?: string;
  effort?: string;
  picks?: number;
  candidates?: number;
  free_picks?: number;
  seeds?: number;
  top_titles?: number;
}

export type VerdictFilter = "none" | Verdict | "all";

export interface ProgressEvent {
  run_id: number;
  kind: Kind;
  message: string;
}
