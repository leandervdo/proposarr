# Proposarr HTTP API (v0)

Served by `proposarr serve` on `PROPOSARR_LISTEN` (default `:8585`) together with the web UI. JSON, snake_case fields. Errors are `{"error": "message"}` with a 4xx/5xx status.

When `PROPOSARR_WEB_USERNAME` and `PROPOSARR_WEB_PASSWORD` are both set, every route (API and UI) requires HTTP Basic auth.

Go zero times (`0001-01-01T00:00:00Z`) in `last_watched` / `added` mean unknown.

## Types

```ts
type Kind = "movies" | "series";
type RunStatus = "running" | "succeeded" | "failed" | "rate_limited";
type Verdict = "accepted" | "ignored" | "later";

interface Rejected { tmdb_id?: number; title: string; reason: string }

interface Run {
  id: number; kind: Kind; vibe?: string; model: string; effort: string;
  status: RunStatus; error?: string; started_at: string; finished_at?: string;
  cost_usd: number; input_tokens: number; output_tokens: number; num_turns: number;
  session_id?: string; library_count: number; history_count: number;
  candidate_count: number; pick_count: number; warnings: string[]; rejected: Rejected[];
  profile?: Profile; // only on GET /api/runs/{id}
}

interface Request {
  pick_id: number; app: "radarr" | "sonarr"; target_id?: number;
  quality_profile: string; root_folder: string; status: "added" | "failed";
  error?: string; requested_at: string;
}

interface Pick {
  id: number; run_id: number; tmdb_id: number; kind: Kind; title: string; year?: number;
  reason: string; related_to: string[]; score: number; source: "candidate" | "free";
  overview?: string; genres?: string[]; rating?: number; streaming?: string[]; poster_url?: string;
  verdict?: Verdict; verdict_at?: string; later_until?: string; request?: Request;
}

interface ProfileEntry {
  tmdb_id?: number; title: string; year?: number; genres?: string[]; weight: number;
  signal: "rewatched" | "watched" | "partial" | "owned"; in_library: boolean;
  last_watched: string; added: string;
}
interface Profile {
  kind: Kind; top: ProfileEntry[]; genres: { name: string; share: number }[];
  library_count: number; history_count: number;
}

interface LibraryTitle {
  tmdb_id?: number; tvdb_id?: number; title: string; year?: number; genres?: string[];
  added: string; poster_url?: string;
}

interface QualityProfile { id: number; name: string }
interface RootFolder { id: number; path: string; free_space: number }
```

## Endpoints

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/status` | | `{version, claude_auth: "oauth_token"\|"api_key"\|"local", connections: {radarr, sonarr, tmdb, plex, jellyfin: boolean}, running: {run_id, kind, message}[]}` |
| GET | `/api/connections/check` | | `{name, status: "ok"\|"fail"\|"skip", detail}[]` (live test of every connection) |
| GET | `/api/config` | | Settings with secrets replaced by booleans, e.g. `radarr: {url, api_key_set, root_folder}`, `claude: {bin, auth, timeout, max_budget_usd}`, `movies`/`series: {model, effort, picks, candidates, free_picks}` |
| GET | `/api/runs?limit=50` | | `Run[]`, newest first, without `profile` |
| GET | `/api/runs/{id}` | | `{run: Run, picks: Pick[]}` |
| POST | `/api/runs` | `{kind, vibe?}` | `202 Run` (status `running`). `409` when a run of that kind is already running. `400` when the kind's app or TMDB is not configured |
| GET | `/api/picks?kind=&run=latest\|{id}&verdict=none\|accepted\|ignored\|later` | | `Pick[]`, newest run first, then score |
| POST | `/api/picks/{id}/verdict` | `{verdict: Verdict \| "", later_days?: number}` | `Pick`. `later_days` defaults to 30. `""` clears the verdict (undo) |
| GET | `/api/apps/{radarr\|sonarr}/options` | | `{quality_profiles: QualityProfile[], root_folders: RootFolder[], default_root_folder: string}` |
| POST | `/api/picks/{id}/request` | `{quality_profile_id: number, root_folder?: string}` | `Pick` with `request` and verdict `accepted`. `400` without `quality_profile_id` (there is no default) or when several root folders exist and none was given or configured. `409` when the title is already in the library |
| GET | `/api/library?kind=` | | `{kind, titles: LibraryTitle[], profile: Profile \| null}` |
| GET | `/api/events` | | Server-sent events, see below |
| GET | `/healthz` | | `200 ok`, plain text. Never requires auth (for container health checks) |

Everything else under `/` serves the single-page app, falling back to `index.html`.

## Events (`GET /api/events`)

`text/event-stream`, a `: ping` comment every 25 s.

| Event | Data |
|---|---|
| `run.started` | `Run` |
| `run.progress` | `{run_id, kind, message}` |
| `run.finished` | `Run` (final status) |
| `pick.updated` | `Pick` (after a verdict or request) |
