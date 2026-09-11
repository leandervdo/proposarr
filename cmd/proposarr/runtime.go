package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/claudetoken"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/settings"
	"github.com/leandervdo/proposarr/internal/tmdb"
	"github.com/leandervdo/proposarr/internal/web"
)

// runtime holds the effective settings and adapters of `proposarr serve` and
// swaps them when settings are saved. An installed state is never modified, so
// a run keeps the adapters it started with.
type runtime struct {
	svc    *settings.Service
	log    *slog.Logger
	listen string         // --listen override
	hc     *http.Client   // API key discovery and connection tests
	agent  agent.Provider // nil uses the claude binary from the settings

	updateMu sync.Mutex
	mu       sync.RWMutex
	cur      *runtimeState
}

type runtimeState struct {
	res        settings.Resolved
	cfg        config.Config // res.Config plus API keys read from initialize.json
	deps       *deps
	discovered map[string]bool // e.g. "radarr.api_key"
}

var _ web.SettingsService = (*runtime)(nil)

func newRuntime(svc *settings.Service, log *slog.Logger, listen string) *runtime {
	return &runtime{svc: svc, log: log, listen: listen, hc: &http.Client{Timeout: 30 * time.Second}}
}

func (rt *runtime) reload(ctx context.Context) error {
	res, err := rt.svc.Resolve(ctx)
	if err != nil {
		return err
	}
	rt.install(ctx, res)
	return nil
}

func (rt *runtime) install(ctx context.Context, res settings.Resolved) {
	cfg := res.Config
	if rt.listen != "" {
		cfg.Listen = rt.listen
	}
	found := map[string]bool{}
	for _, d := range discoverAPIKeys(ctx, &cfg, rt.hc, media.Movies, media.Series) {
		if d.err != nil {
			rt.log.Warn("API key not available", "app", d.kind.App(), "err", d.err)
			continue
		}
		found[appKey(d.kind)+".api_key"] = true
		rt.log.Info("using the API key from initialize.json", "app", d.kind.App())
	}
	st := &runtimeState{res: res, cfg: cfg, deps: buildDeps(cfg), discovered: found}
	rt.mu.Lock()
	rt.cur = st
	rt.mu.Unlock()
}

func (rt *runtime) state() *runtimeState {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.cur
}

func (rt *runtime) View(context.Context) (settings.View, error) {
	st := rt.state()
	hints := map[string]string{}
	for key := range st.discovered {
		hints[key] = settings.HintDiscovered
	}
	return st.res.View(hints), nil
}

// Update saves the values and switches new work to adapters built from them.
func (rt *runtime) Update(ctx context.Context, values map[string]json.RawMessage) (settings.View, error) {
	rt.updateMu.Lock()
	defer rt.updateMu.Unlock()
	res, err := rt.svc.Update(ctx, values)
	if err != nil {
		return settings.View{}, err
	}
	rt.install(ctx, res)
	return rt.View(ctx)
}

// Test checks one service with the unsaved values merged over the current settings.
func (rt *runtime) Test(ctx context.Context, service string, values map[string]json.RawMessage) (web.TestResult, error) {
	cfg, err := rt.svc.Draft(ctx, values)
	if err != nil {
		return web.TestResult{}, err
	}
	claude := rt.agent
	if claude == nil {
		claude = agent.Claude{Bin: cfg.Claude.Bin}
	}
	return testConnection(ctx, service, cfg, rt.hc, claude), nil
}

func appKey(k media.Kind) string { return strings.ToLower(k.App()) }

func testConnection(ctx context.Context, service string, cfg config.Config, hc *http.Client, claude agent.Provider) web.TestResult {
	timeout := 20 * time.Second
	if service == "claude" {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	secrets := []string{cfg.Radarr.APIKey, cfg.Sonarr.APIKey, cfg.Plex.Token, cfg.Jellyfin.APIKey, cfg.TMDB.APIKey, cfg.Claude.OAuthToken, cfg.Claude.APIKey}
	fail := func(err error) web.TestResult {
		return web.TestResult{Status: web.CheckFail, Detail: scrub(err.Error(), secrets)}
	}
	ok := func(detail string) web.TestResult { return web.TestResult{Status: web.CheckOK, Detail: detail} }

	switch service {
	case "radarr", "sonarr":
		a, kind := cfg.Radarr, media.Movies
		if service == "sonarr" {
			a, kind = cfg.Sonarr, media.Series
		}
		if a.URL == "" {
			return fail(fmt.Errorf("%s.url is not set", service))
		}
		discovered := false
		if a.APIKey == "" {
			key, err := arr.FetchAPIKey(ctx, a.URL, hc)
			if err != nil {
				return fail(err)
			}
			a.APIKey, discovered = key, true
			secrets = append(secrets, key)
		}
		var (
			st  arr.SystemStatus
			err error
		)
		if kind == media.Series {
			st, err = arr.NewSonarr(a.URL, a.APIKey, hc).Status(ctx)
		} else {
			st, err = arr.NewRadarr(a.URL, a.APIKey, hc).Status(ctx)
		}
		if err != nil {
			return fail(err)
		}
		res := ok(st.Version)
		res.DiscoveredAPIKey = discovered
		return res
	case "plex":
		if cfg.Plex.URL == "" || cfg.Plex.Token == "" {
			return fail(errors.New("plex.url and plex.token are required"))
		}
		if err := history.NewPlex(cfg.Plex.URL, cfg.Plex.Token, hc).Ping(ctx); err != nil {
			return fail(err)
		}
		return ok("connected")
	case "jellyfin":
		if cfg.Jellyfin.URL == "" || cfg.Jellyfin.APIKey == "" {
			return fail(errors.New("jellyfin.url and jellyfin.api_key are required"))
		}
		if err := history.NewJellyfin(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey, cfg.Jellyfin.UserID, hc).Ping(ctx); err != nil {
			return fail(err)
		}
		return ok("connected")
	case "tmdb":
		if cfg.TMDB.APIKey == "" {
			return fail(errors.New("tmdb.api_key is not set"))
		}
		if err := tmdb.New(cfg.TMDB.APIKey, hc).Ping(ctx); err != nil {
			return fail(err)
		}
		return ok("connected")
	case "claude":
		name, value, err := cfg.Claude.Auth()
		if err != nil {
			return fail(err)
		}
		via := "the local claude login"
		if name != "" {
			via = name
		}
		err = claudetoken.Probe(ctx, claude, agent.BuildEnv(os.Environ(), name, value))
		var sl *agent.SessionLimitError
		switch {
		case errors.As(err, &sl):
			return ok(via + " is valid, but the session limit is reached")
		case err != nil:
			return fail(err)
		}
		return ok(via + " works")
	}
	return fail(fmt.Errorf("unknown service %q", service))
}

// scrub replaces any configured secret in a message shown in the UI.
func scrub(msg string, secrets []string) string {
	for _, s := range secrets {
		if len(s) >= 4 {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return msg
}
