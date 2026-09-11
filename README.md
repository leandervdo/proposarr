# Proposarr

Proposarr proposes new series and movies for a Sonarr and Radarr library, and adds the ones you accept.

Your Plex or Jellyfin watch history is the main taste signal. Claude, run headlessly through the official Claude Code CLI, ranks candidate titles against that history. You can use a Claude subscription token or an Anthropic API key.

| Radarr Discover | Proposarr |
|---|---|
| Knows what you own, not what you watched | Plex or Jellyfin watch history is the primary taste signal |
| Aggregates TMDB "recommended" counts | Candidates are ranked by Claude against a taste profile |
| No reasoning | Every pick names the library titles it relates to |
| No memory of ignored titles | Accept, ignore and later verdicts persist per TMDB id (planned) |
| No intent input | Free-text "vibe" per run |
| Movies only | Series and movies |

## Status

Early development. The command-line tool (milestone 1) is in place; state, the web UI and scheduling are not built yet.

Planned milestones:

1. **CLI recommends.** `proposarr run` prints picks for your real library.
2. **State.** SQLite store, verdicts, run log with cost.
3. **MCP toolbox.** Read-only library, history and TMDB tools for the model.
4. **Web UI.** Poster grid of picks, runs timeline, connections and settings.
5. **Scheduling and usage.** Scheduled runs, subscription usage, notifications.
6. **Ship.** Versioned releases. The Docker image, compose example and CI already exist.

## Requirements

- Go 1.26 or newer
- The Claude Code CLI: `npm install -g @anthropic-ai/claude-code`
- A TMDB API key
- Radarr and/or Sonarr v4 or newer
- Optional: Plex or Jellyfin, for watch history

## Install

```sh
go install github.com/leandervdo/proposarr/cmd/proposarr@latest
```

Or from source:

```sh
git clone https://github.com/leandervdo/proposarr
cd proposarr
go build ./cmd/proposarr
```

## Configuration

The normal way to configure Proposarr is the web UI: start `proposarr serve`, open it, and enter Radarr, Sonarr, Plex or Jellyfin, TMDB and the Claude credential under Connections and Settings. Each connection has a Test button, and saved settings apply immediately, without a restart. They are stored in `proposarr.db` in the data directory; API keys and tokens are encrypted there (AES-256-GCM) and are never sent back to the browser.

A YAML file and environment variables are optional overrides, for people who prefer to configure everything up front. Precedence, lowest to highest: built-in default, web UI, config file, environment variable. A setting that the file or an environment variable provides is shown in the UI as locked, with the name of the variable, and can only be changed there. The file is read from `--config PATH`, else `PROPOSARR_CONFIG` (both must point to an existing file), else `./proposarr.yaml` if it exists. See [`proposarr.example.yaml`](proposarr.example.yaml) for every key.

CLI commands (`run`, `add`, `check`, `validate-token`) use the settings saved in the UI too, whenever `proposarr.db` exists in the data directory, with the same precedence.

The listen address, data directory, `claude` binary path and web login can only be set in the file or the environment.

| Variable | Default | Notes |
|---|---|---|
| `PROPOSARR_LISTEN` | `:8585` | Address `proposarr serve` listens on |
| `PROPOSARR_WEB_USERNAME`, `PROPOSARR_WEB_PASSWORD` | | Optional HTTP Basic login for the web UI and API. Set both or neither |
| `PROPOSARR_DATA_DIR` | `data` | Snapshot cache, SQLite database and run data |
| `PROPOSARR_SECRET_KEY` | | Key for secrets saved from the UI: base64 or hex of 32 bytes, or any passphrase. When unset, a random key is created once in `<data dir>/secret.key`; keep that file with the database |
| `PROPOSARR_HISTORY_DAYS` | `180` | Watch-history window |
| `PROPOSARR_SNAPSHOT_TTL` | `6h` | Library snapshot lifetime |
| `PROPOSARR_SONARR_URL`, `PROPOSARR_SONARR_API_KEY` | | URL required for series. API key optional, see below |
| `PROPOSARR_SONARR_ROOT_FOLDER` | | Optional default root folder |
| `PROPOSARR_RADARR_URL`, `PROPOSARR_RADARR_API_KEY` | | URL required for movies. API key optional, see below |
| `PROPOSARR_RADARR_ROOT_FOLDER` | | Optional default root folder |
| `PROPOSARR_RADARR_MINIMUM_AVAILABILITY` | `released` | `announced`, `inCinemas` or `released` |
| `PROPOSARR_PLEX_URL`, `PROPOSARR_PLEX_TOKEN` | | Optional |
| `PROPOSARR_JELLYFIN_URL`, `PROPOSARR_JELLYFIN_API_KEY` | | Optional |
| `PROPOSARR_JELLYFIN_USER_ID` | | Empty merges every user |
| `PROPOSARR_TMDB_API_KEY` | | Required. v3 key or v4 read access token |
| `PROPOSARR_TMDB_REGION` | `US` | Region for streaming availability |
| `PROPOSARR_CLAUDE_BIN` | `claude` | Path to the Claude Code CLI |
| `CLAUDE_CODE_OAUTH_TOKEN` | | Subscription token, see below |
| `ANTHROPIC_API_KEY` | | API key, see below |
| `PROPOSARR_CLAUDE_TIMEOUT` | `10m` | Wall-clock limit per run |
| `PROPOSARR_CLAUDE_MAX_BUDGET_USD` | `0` | Spend cap per run, 0 for none |
| `PROPOSARR_MOVIES_MODEL`, `PROPOSARR_SERIES_MODEL` | `claude-sonnet-5` | |
| `PROPOSARR_MOVIES_EFFORT`, `PROPOSARR_SERIES_EFFORT` | `medium` | `low`, `medium`, `high`, `xhigh`, `max` |
| `PROPOSARR_MOVIES_PICKS`, `PROPOSARR_SERIES_PICKS` | `10` | Picks per run |
| `PROPOSARR_MOVIES_CANDIDATES`, `PROPOSARR_SERIES_CANDIDATES` | `150` | Candidate list cap |
| `PROPOSARR_MOVIES_FREE_PICKS`, `PROPOSARR_SERIES_FREE_PICKS` | `3` | Picks allowed outside the candidate list |
| `PROPOSARR_MOVIES_SEEDS`, `PROPOSARR_SERIES_SEEDS` | `15` | Profile titles used to fetch recommendations |
| `PROPOSARR_MOVIES_TOP_TITLES`, `PROPOSARR_SERIES_TOP_TITLES` | `40` | Profile titles shown to the model |

**Sonarr/Radarr API key fallback.** When the API key is left empty, Proposarr reads it from the app's `/initialize.json`, the file its web UI loads. That only works while Sonarr/Radarr do not require a login from Proposarr's address (Settings → General → Authentication Required → Disabled for Local Addresses), and it is not an official API. A configured key always wins; set one if you enable authentication.

## Usage

```sh
proposarr serve [--listen ADDR]   # web UI and HTTP API
proposarr run --kind movies|series [--vibe TEXT] [--picks N] [--model M] [--effort E] [--json] [--refresh] [--add]
proposarr add --kind movies|series --tmdb ID [--quality-profile NAME|ID] [--root-folder PATH]
proposarr check            # test every configured connection
proposarr validate-token   # probe the Claude credential with a one-line model call
proposarr version
```

Every command accepts `--config PATH`. `run` prints the picks to stdout, or the whole run as JSON with `--json`; progress, warnings and rejected picks go to stderr. `--refresh` ignores the cached library snapshot. `--picks`, `--model` and `--effort` override the per-kind settings for one run.

`check` prints `ok`, `FAIL` or `skip` for Radarr, Sonarr, TMDB, Plex, Jellyfin, the `claude` binary and the Claude credential mode, and exits 1 if anything failed. It makes no model call; `validate-token` does.

### Web UI and API

`proposarr serve` starts the web UI and the HTTP API on one port (`PROPOSARR_LISTEN`, default `:8585`; `--listen` overrides it). Runs, picks, verdicts and requests are stored in `proposarr.db` under the data directory. Start a run, accept, ignore or postpone picks, and add accepted picks to Sonarr or Radarr from the browser; adding asks for a quality profile for that title, just like the CLI. Titles you accepted, ignored, postponed or added are left out of later runs.

The UI has no login by default. Set `PROPOSARR_WEB_USERNAME` and `PROPOSARR_WEB_PASSWORD` to require HTTP Basic auth, especially if anyone else can reach the port: the UI can add titles to Sonarr and Radarr and start Claude runs. `/healthz` stays open for container health checks.

The API is documented in [`docs/API.md`](docs/API.md). A binary built without the UI (`web/dist` empty) serves a short notice page instead; build the UI with `pnpm install && pnpm build` in `web/` before `go build`.

### Quality profile per title

Proposarr never adds a title with a default quality profile. Every add asks which of the app's quality profiles to use for that title:

- `proposarr run --add` goes through the picks and asks `Add Arrival (2016) to Radarr? [y/N/q]` for each. For every pick you accept, it lists the Radarr or Sonarr quality profiles and asks which one to use. Answer with a number or a profile name. From the second title on, Enter reuses the profile you chose for the previous title; `q` stops adding. `--add` needs an interactive terminal and cannot be combined with `--json`.
- `proposarr add` asks the same question for one title. When stdin is not a terminal, pass `--quality-profile` (a name or id) instead.

If the app has several root folders and none is configured (`--root-folder`, or `root_folder` in the config), you are asked for the root folder too.

## How a run works

1. **Snapshot.** Read the Sonarr or Radarr library, cached for six hours.
2. **History.** Read Plex or Jellyfin watch history for the configured window.
3. **Profile.** Weight titles: rewatched highest, then watched, then partially watched, then owned but unwatched.
4. **Candidates.** Collect TMDB recommendations (and similar titles for series) for the top profile titles, plus Radarr's discover list for movies. Drop anything already owned or watched.
5. **Claude.** Run the Claude Code CLI once, headlessly. It gets no built-in tools, gets the profile and candidates in the prompt, and must return JSON that matches a schema.
6. **Verify.** Check every pick by TMDB id against the library and history. Resolve picks from outside the candidate list through TMDB search, and drop any that don't resolve.

## Claude authentication

Set exactly one of:

- `CLAUDE_CODE_OAUTH_TOKEN`, created with `claude setup-token`, to use a Claude subscription.
- `ANTHROPIC_API_KEY`, to use API billing.

If neither is set, the CLI's own login is used. The credential reaches the `claude` binary through its environment only, never through command-line arguments. Proposarr never calls the Anthropic API with a subscription token itself; only the official Claude Code CLI uses it.

Anthropic's terms cover Claude Code. Whether a scheduled, headless Claude Code run fits your plan is your decision as the operator.

## Docker

Images for `linux/amd64` and `linux/arm64` are published as [`leander1999/proposarr`](https://hub.docker.com/r/leander1999/proposarr). The image bundles Proposarr and the Claude Code CLI, and works with any Sonarr and Radarr install it can reach over the network.

Only the port and a volume are needed:

```sh
docker run -d --name proposarr \
  -p 8585:8585 \
  -v /path/to/proposarr:/config \
  leander1999/proposarr:latest
```

Then open `http://<host>:8585` and fill in the connections in the web UI. See [`docker-compose.example.yml`](docker-compose.example.yml) for a compose file.

- **User.** Set `PUID` and `PGID` to the owner of the directory you mount at `/config` (default 1000; 99 and 100 on Unraid).
- **Config.** Settings entered in the UI, the database and the generated `secret.key` live under `/config/data`. Environment variables (see the table above) or `/config/proposarr.yaml` are optional; anything they set is locked in the UI. The library cache and the Claude CLI's own state also live under `/config`.
- **Claude authentication.** The container cannot use the login of the `claude` CLI on your desktop. Create a token with `claude setup-token` and enter it in the UI (or set `CLAUDE_CODE_OAUTH_TOKEN`), or use an Anthropic API key.
- **Network.** Sonarr, Radarr, Plex and Jellyfin URLs must be reachable from inside the container: use the service name when they share a Docker network (`http://radarr:7878`), or a LAN address. `localhost` inside the container is the container itself.
- **Web UI.** The container runs `proposarr serve` on port 8585. Publish it with `-p 8585:8585` and open `http://<host>:8585`.
- **Running commands.** CLI commands also work inside the running container:

  ```sh
  docker exec proposarr proposarr check
  docker exec -it proposarr proposarr run --kind movies --add
  ```

  `-it` is needed for `--add`, because every title you add asks for a quality profile.

## Credits

Proposarr is a Go rewrite of [Recommendarr](https://github.com/Teagan42/recommendarr) (MIT). The prompt wording, ranking criteria, exclusion rules, and the Sonarr, Radarr, Plex and Jellyfin endpoint shapes are ported from it.

## License

MIT. See [LICENSE](LICENSE).
