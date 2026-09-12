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
  /** false for an open search driven only by `vibe`. */
  use_taste: boolean;
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
  /** Radarr movies: how Radarr's search went. Starts as "checking" and is updated through pick.updated. */
  release?: ReleaseCheck;
}

export interface Pick {
  id: number;
  run_id: number;
  /** The run's description. */
  run_vibe?: string;
  /** false when the pick came from an open search. */
  run_use_taste: boolean;
  /** The run's started_at. */
  found_at: string;
  tmdb_id: number;
  imdb_id?: string;
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
  ratings?: Ratings;
  verdict?: Verdict;
  verdict_at?: string;
  later_until?: string;
  request?: Request;
}

/** One indexer release as Radarr parsed it. */
export interface ReleaseInfo {
  title: string;
  /** e.g. "Remux-1080p" */
  quality: string;
  /** bytes, when known */
  size?: number;
  indexer?: string;
  /** "torrent" or "usenet" */
  protocol?: string;
  /** Torrents from a search only. */
  seeders?: number;
}

/** Another quality profile that would grab a release right now. */
export interface ProfileOption {
  id: number;
  name: string;
  /** Releases this profile would accept. */
  count: number;
  /** The one it would most likely grab. */
  best: ReleaseInfo;
}

/** What to do when Radarr's search grabs nothing for the chosen profile (movies only). */
export type IfNothingFits = "switch" | "wait";

/** What Radarr's own search did right after adding a movie (Radarr only). */
export interface ReleaseCheck {
  /**
   * checking: Proposarr is still following Radarr's search.
   * grabbed: Radarr grabbed `release`. pending: Radarr holds `release` for a delay profile.
   * waiting: Radarr grabbed nothing. searching: Radarr was still searching when Proposarr stopped waiting.
   * unavailable: not released yet under Radarr's minimum availability. failed: the outcome couldn't be read.
   */
  status: "checking" | "grabbed" | "pending" | "waiting" | "searching" | "unavailable" | "failed";
  /** The profile Radarr searched with; after a switch, the new one. */
  profile: string;
  /** Proposarr switched away from this profile because nothing fit it. */
  switched_from?: string;
  release?: ReleaseInfo;
  /** waiting: releases the explaining search found (0 = none on the indexers). */
  found: number;
  /** waiting: what was found, most common first. */
  qualities: { quality: string; count: number }[];
  /** waiting: other profiles that would grab one now, best first. */
  alternatives: ProfileOption[];
  error?: string;
}

/** Real ratings. Series usually only have IMDb. Unknown values are omitted. */
export interface Ratings {
  /** value 0–10 */
  imdb?: { value: number; votes: number };
  /** critic score 0–100 */
  rotten_tomatoes?: number;
  /** 0–100 */
  metacritic?: number;
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

export interface CastMember {
  name: string;
  character?: string;
  profile_url?: string;
}

/** Everything about one title, fetched live from TMDB (plus ratings from Radarr/Sonarr). */
export interface TitleDetails {
  tmdb_id: number;
  kind: Kind;
  title: string;
  year?: number;
  tagline?: string;
  overview?: string;
  genres: string[] | null;
  /** Minutes; the typical episode length for series. */
  runtime?: number;
  /** Movies: release date; series: first air date (YYYY-MM-DD). */
  release_date?: string;
  /** e.g. "Released", "Returning Series", "Ended". */
  status?: string;
  seasons?: number;
  episodes?: number;
  poster_url?: string;
  /** w1280 */
  backdrop_url?: string;
  /** Movies: directors; series: creators. */
  directors: string[] | null;
  /** Top 12 billed. */
  cast: CastMember[] | null;
  trailer?: { name: string; youtube_key: string };
  /** Flatrate providers in the configured region. */
  streaming: string[] | null;
  imdb_id?: string;
  tmdb_rating?: number;
  tmdb_votes?: number;
  ratings?: Ratings;
  in_library: boolean;
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
