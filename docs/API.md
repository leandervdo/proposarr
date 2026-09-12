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
  id: number; kind: Kind; vibe?: string; use_taste: boolean; model: string; effort: string;
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
  release?: ReleaseCheck;  // Radarr movies: what Radarr's search did, see "Release check"
}

interface ReleaseCheck {
  status: "checking" | "grabbed" | "pending" | "waiting" | "searching" | "unavailable" | "failed";
  profile: string;               // the quality profile Radarr searched with
  switched_from?: string;        // nothing fit this profile, so Proposarr switched away from it
  release?: ReleaseInfo;         // grabbed: what Radarr grabbed; pending: what Radarr holds for a delay profile
  found: number;                 // waiting: releases the explaining search found
  qualities: { quality: string; count: number }[];  // waiting: what was found, most common first
  alternatives: ProfileOption[]; // waiting: other profiles that would grab a release now, in ranking order
  error?: string;                // failed
}
interface ReleaseInfo { title: string; quality: string; size?: number; indexer?: string; protocol?: string; seeders?: number }
interface ProfileOption { id: number; name: string; count: number; best: ReleaseInfo }

interface Pick {
  id: number; run_id: number; run_vibe?: string; run_use_taste: boolean; found_at: string;  // found_at = the run's started_at
  tmdb_id: number; imdb_id?: string; kind: Kind; title: string; year?: number;
  reason: string; related_to: string[]; score: number; source: "candidate" | "free";
  overview?: string; genres?: string[]; rating?: number; streaming?: string[]; poster_url?: string;
  ratings?: Ratings;
  verdict?: Verdict; verdict_at?: string; later_until?: string; request?: Request;
}

// Real ratings. Movies: from Radarr's metadata (IMDb, Rotten Tomatoes critic score, Metacritic).
// Series: Sonarr only carries the IMDb rating. Unknown values are omitted.
interface Ratings {
  imdb?: { value: number; votes: number };  // 0–10
  rotten_tomatoes?: number;                 // critic score, 0–100
  metacritic?: number;                      // 0–100
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

// Everything about one title, fetched live from TMDB (plus ratings from Radarr/Sonarr) for the detail modal.
interface TitleDetails {
  tmdb_id: number; kind: Kind; title: string; year?: number;
  tagline?: string; overview?: string; genres: string[];
  runtime?: number;            // minutes; typical episode length for series
  release_date?: string;       // movies: release date; series: first air date (YYYY-MM-DD)
  status?: string;             // e.g. "Released", "Returning Series", "Ended"
  seasons?: number; episodes?: number;  // series only
  poster_url?: string; backdrop_url?: string;  // backdrop at w1280
  directors: string[];         // movies: directors; series: creators
  cast: { name: string; character?: string; profile_url?: string }[];  // top 12 billed
  trailer?: { name: string; youtube_key: string };  // first official YouTube trailer, else first YouTube trailer/teaser
  streaming: string[];         // flatrate providers in the configured region
  imdb_id?: string;
  tmdb_rating?: number; tmdb_votes?: number;
  ratings?: Ratings;           // same shape as Pick.ratings
  in_library: boolean;
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
| POST | `/api/runs` | `{kind, vibe?, use_taste?}` | `202 Run` (status `running`). `use_taste` defaults to `true`; see "Open search" below. `409` when a run of that kind is already running. `400` when the kind's app or TMDB is not configured, or when `use_taste` is `false` and `vibe` is empty |
| GET | `/api/picks?kind=&run=latest\|all\|{id}&verdict=none\|accepted\|ignored\|later&added=true&search=true\|false&distinct=true&limit=&offset=` | | `Pick[]`, newest run first, then score. `run` defaults to `latest`; `all` spans every succeeded run. `added=true` keeps picks whose latest request has status `added`, ordered by `request.requested_at` newest first. `search=true` keeps picks from open-search runs (`use_taste` false), `search=false` from taste runs. `distinct=true` returns one pick per `(tmdb_id, kind)`, the most recent. `limit` defaults to 200 (max 1000), `offset` to 0. The `X-Total-Count` response header is the number of matching picks before `limit`/`offset`. Titles currently in the Radarr/Sonarr library are left out, except picks added through Proposarr (so they still show under Added) |
| POST | `/api/picks/{id}/verdict` | `{verdict: Verdict \| "", later_days?: number}` | `Pick`. `later_days` defaults to 30. `""` clears the verdict (undo) |
| GET | `/api/apps/{radarr\|sonarr}/options` | | `{quality_profiles: QualityProfile[], root_folders: RootFolder[], default_root_folder: string}` |
| POST | `/api/picks/{id}/request` | `{quality_profile_id: number, root_folder?: string, if_nothing_fits?: "switch" \| "wait"}` | `Pick` with `request` and verdict `accepted`. For a Radarr movie `request.release` has status `checking`; see "Release check". `if_nothing_fits` defaults to `wait`; it is ignored for series and when no profile is ranked below the chosen one in `radarr.profile_order`. `400` without `quality_profile_id` (there is no default), for another `if_nothing_fits`, or when several root folders exist and none was given or configured. `409` when the title is already in the library |
| POST | `/api/picks/{id}/request/profile` | `{quality_profile_id: number}` | `Pick` whose `request` has the new `quality_profile` and `release.status` `checking`, after moving the added movie to that profile in Radarr and starting Radarr's search. `400` without or with an unknown `quality_profile_id`, or for a series. `409` when the pick was not added to Radarr, the movie is no longer there, or its release check is still running. `502` when Radarr fails |
| GET | `/api/library?kind=` | | `{kind, titles: LibraryTitle[], profile: Profile \| null}` |
| GET | `/api/titles/{movies\|series}/{tmdb_id}` | | `TitleDetails`. Fetched live from TMDB with credits, videos and watch providers, and ratings from Radarr (movies) or Sonarr (series); cached in memory for 1 hour. `404` when TMDB does not know the id, `400` when TMDB is not configured. A ratings failure only leaves `ratings` absent |
| GET | `/api/events` | | Server-sent events, see below |
| GET | `/healthz` | | `200 ok`, plain text. Never requires auth (for container health checks) |

Everything else under `/` serves the single-page app, falling back to `index.html`.

## Release check

Radarr does every search and grab. Adding a movie turns on Radarr's own search, exactly as adding it in Radarr would. Proposarr then follows that search in the background and stores the outcome in `request.release`, starting from `checking`. When it is done, `pick.updated` carries the pick, and every endpoint that returns picks includes it, so several picks' states can be shown without polling.

- `grabbed`: Radarr grabbed `release`.
- `pending`: Radarr found `release` but holds it for a delay profile.
- `unavailable`: the movie is not released yet for its minimum availability; Radarr grabs it later.
- `searching`: Radarr's search was still running after about 45 seconds, or the server restarted during the check. Radarr carries on.
- `failed`: the outcome could not be read (`error`).
- `waiting`: Radarr grabbed nothing. Proposarr then runs one interactive search (Radarr's `GET /api/v3/release`, which grabs nothing) to explain why. `found` is what the indexers returned and `qualities` summarises it. `alternatives` lists the other quality profiles that would grab one of those releases now: ranked profiles first in the order of `radarr.profile_order`, then the rest in Radarr's order. Proposarr never guesses which profile is better. A release counts for a profile when its quality is allowed, its custom format score reaches the profile's minimum and its language fits, the way Radarr judges it. Rejections that don't depend on the profile (too few seeders, size limits, an unparsable title) rule it out for every profile.

`if_nothing_fits` is chosen per title with the quality profile. With `switch`, a `waiting` result makes Proposarr move the movie to the highest alternative ranked below its profile in `radarr.profile_order` and have Radarr search again, once. It never switches to a higher-ranked or unranked profile, and without a ranking it doesn't switch. The outcome then has `switched_from`, and `request.quality_profile` becomes the new profile. With `wait`, the user can switch later with `POST /api/picks/{id}/request/profile`. That request is recorded again with the new `quality_profile` and the original `requested_at`, and is followed the same way. It never switches a second time on its own.

Series are added with Sonarr's own search and have no `release`.

## Open search

A run normally ranks candidates against the taste profile (`use_taste: true`). With `use_taste: false` it is a search driven only by the free-text `vibe` ("90s heist movies with a twist ending", "short Korean thrillers"), not by the library or watch history:

- No watch history is read, no taste profile is built and no TMDB candidate list is gathered. The run's `history_count` and `candidate_count` are 0 and `profile` is absent.
- Claude suggests titles from the description alone. Every suggestion is resolved on TMDB by title and year (the same check as free picks) and dropped when it does not resolve.
- Titles already in the library, accepted, ignored, postponed or requested are still left out. The owned titles are listed in the prompt (up to the profile size) so Claude avoids them.
- Picks have `source: "free"` and may have an empty `related_to`; `reason` says how the title matches the description.
- Explicit constraints in the description (decade, country, language, genre) are hard requirements. Claude scores each suggestion on how well it fits the description; suggestions below 70 are dropped, so a search can return fewer picks rather than off-target ones.
- Ranking uses real ratings, not Claude's estimate. Claude suggests about twice the pick count (at most 50); after verification each remaining pick's rating score is the mean of the available values among IMDb × 10 (only with at least 1,000 votes) and the Rotten Tomatoes critic score. Picks are ordered by that score, the best `picks` are kept, and `score` is the rating score (rounded). A pick without either rating keeps Claude's score and sorts after rated picks.

## Collection

The UI's Collection page is built from `GET /api/picks` across all runs:

- **Added**: `run=all&added=true&distinct=true` — every title added to Sonarr/Radarr through Proposarr, newest request first.
- **Searches**: `run=all&search=true` — every pick from open-search runs, grouped client-side by `run_id` (with `run_vibe` and `found_at` as the group heading), newest search first. Searches without picks are not shown.

Clicking a title anywhere (Picks, Collection, Library) opens a detail modal fed by `GET /api/titles/{kind}/{tmdb_id}`, combined with the pick (reason, related titles, score, verdict, request) when the title came from a pick.

## IMDb and Rotten Tomatoes

`Pick.imdb_id` is the IMDb id (`tt0133093`) from TMDB's external ids, for movies and series. Link it as `https://www.imdb.com/title/<imdb_id>/`. It can be absent (older picks, or TMDB has none); clients then link an IMDb search: `https://www.imdb.com/find/?q=<title> <year>`.

`Pick.ratings` is filled for every run (taste runs show it too; only open search ranks by it). Rotten Tomatoes has no id-based URL; link its search: `https://www.rottentomatoes.com/search?search=<title>`.

## Settings

Connections and run settings are edited in the UI and stored in the database; a new install only needs the port and the `/config` volume. They apply immediately, without a restart (a run that is already going finishes with the old settings).

Precedence, lowest to highest: built-in default, UI, config file, environment variable. A field set by the file or an environment variable is `locked`: the UI shows it but cannot override it. Empty values in the file or environment do not count as set.

Secrets (`*.api_key`, `*.token`, `claude.oauth_token`) are write-only: the API never returns them, only whether one is set. They are encrypted at rest with AES-256-GCM, using `PROPOSARR_SECRET_KEY` or, when that is unset, a random key generated once in `<data_dir>/secret.key` (0600).

Editable keys:

| Key | Type |
|---|---|
| `radarr.url`, `sonarr.url`, `plex.url`, `jellyfin.url` | URL (http/https) |
| `radarr.api_key`, `sonarr.api_key` | secret, optional: read from `/initialize.json` when empty |
| `radarr.root_folder`, `sonarr.root_folder` | string |
| `radarr.minimum_availability` | `announced` \| `inCinemas` \| `released` |
| `radarr.profile_order` | Radarr quality profile ids or names, comma separated, best first; no id twice. The UI writes every profile's id |
| `plex.token`, `jellyfin.api_key`, `tmdb.api_key`, `claude.oauth_token`, `claude.api_key` | secret |
| `jellyfin.user_id` | string |
| `tmdb.region` | two-letter country code |
| `claude.timeout`, `snapshot_ttl` | Go duration (`10m`, `6h`); `claude.timeout` must be more than 0 |
| `claude.max_budget_usd` | number ≥ 0 |
| `history_days` | integer 1–3650 |
| `movies.model`, `series.model` | string |
| `movies.effort`, `series.effort` | `low` \| `medium` \| `high` \| `xhigh` \| `max` |
| `movies.picks`, `series.picks` | integer 1–50 |
| `movies.candidates`, `series.candidates` | integer 10–500 |
| `movies.free_picks`, `series.free_picks` | integer 0–10 |
| `movies.seeds`, `series.seeds` | integer 1–50 |
| `movies.top_titles`, `series.top_titles` | integer 5–1000 |

`listen`, `data_dir`, `claude.bin` and the web login (`PROPOSARR_WEB_USERNAME` / `PROPOSARR_WEB_PASSWORD`) stay file/environment only and are shown read-only.

```ts
type SettingSource = "default" | "ui" | "file" | "env";

interface SettingField {
  value?: string | number;  // omitted for secrets
  secret: boolean;
  set: boolean;             // a non-empty effective value exists
  source: SettingSource;
  locked: boolean;          // source is "file" or "env"
  env: string;              // the environment variable that would lock it, e.g. "PROPOSARR_RADARR_URL"
  hint?: string;            // e.g. "read from initialize.json"
}

interface Settings {
  fields: Record<string, SettingField>;
  read_only: { listen: string; data_dir: string; claude_bin: string; web_auth: boolean; config_file?: string };
}
```

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/settings` | | `Settings` |
| PUT | `/api/settings` | `{values: Record<string, string \| number \| null>}` | `Settings`. Omitted keys are unchanged; `null` or `""` removes the UI value (falls back to file/default). `400 {error, field_errors: Record<string, string>}` on invalid values, locked keys, unknown keys, or both Claude credentials set. Nothing is saved when any field fails |
| POST | `/api/settings/test` | `{service: "radarr"\|"sonarr"\|"plex"\|"jellyfin"\|"tmdb"\|"claude", values?: Record<string, string \| number \| null>}` | `{status: "ok"\|"fail", detail, discovered_api_key?: boolean}`. Tests with the unsaved `values` merged over the current settings, so the UI can test before saving; keys locked by the file or environment are ignored there. Nothing is saved. `claude` makes one short model call |

`GET /api/status` also returns `setup_required: boolean` and `missing: string[]` (e.g. `["tmdb.api_key", "radarr.url or sonarr.url"]`, or `"radarr.api_key"` when a URL is set but no key is configured or readable from `/initialize.json`), so the UI can show onboarding instead of an empty Picks page.

## Events (`GET /api/events`)

`text/event-stream`, a `: ping` comment every 25 s.

| Event | Data |
|---|---|
| `run.started` | `Run` |
| `run.progress` | `{run_id, kind, message}` |
| `run.finished` | `Run` (final status) |
| `pick.updated` | `Pick` (after a verdict or request, and when a release check finishes) |
