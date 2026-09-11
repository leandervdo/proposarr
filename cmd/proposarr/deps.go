package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/snapshot"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

// deps is the composition root. Adapters that are not configured stay nil,
// and are only assigned to interface fields when non-nil.
type deps struct {
	cfg      config.Config
	radarr   *arr.Radarr
	sonarr   *arr.Sonarr
	tmdb     *tmdb.Client
	plex     *history.Plex
	jellyfin *history.Jellyfin
}

func buildDeps(cfg config.Config) *deps {
	hc := &http.Client{Timeout: 30 * time.Second}
	d := &deps{cfg: cfg}
	if cfg.Radarr.Configured() {
		d.radarr = arr.NewRadarr(cfg.Radarr.URL, cfg.Radarr.APIKey, hc)
	}
	if cfg.Sonarr.Configured() {
		d.sonarr = arr.NewSonarr(cfg.Sonarr.URL, cfg.Sonarr.APIKey, hc)
	}
	if cfg.TMDB.APIKey != "" {
		d.tmdb = tmdb.New(cfg.TMDB.APIKey, hc)
	}
	if cfg.Plex.Configured() {
		d.plex = history.NewPlex(cfg.Plex.URL, cfg.Plex.Token, hc)
	}
	if cfg.Jellyfin.Configured() {
		d.jellyfin = history.NewJellyfin(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey, cfg.Jellyfin.UserID, hc)
	}
	return d
}

func (d *deps) historySources() []history.Source {
	var out []history.Source
	if d.plex != nil {
		out = append(out, d.plex)
	}
	if d.jellyfin != nil {
		out = append(out, d.jellyfin)
	}
	return out
}

func (d *deps) library(refresh bool) *snapshot.Library {
	l := &snapshot.Library{Dir: d.cfg.DataDir, TTL: d.cfg.SnapshotTTL.Duration, Refresh: refresh}
	if d.radarr != nil {
		l.Radarr = d.radarr
	}
	if d.sonarr != nil {
		l.Sonarr = d.sonarr
	}
	if d.tmdb != nil {
		l.TMDB = d.tmdb
	}
	return l
}

// pipeline builds a run pipeline. excl may be nil (the CLI keeps no verdicts).
func (d *deps) pipeline(refresh bool, progress func(string), excl pipeline.Exclusions) *pipeline.Pipeline {
	pd := pipeline.Deps{
		Library:    d.library(refresh),
		History:    d.historySources(),
		Agent:      agent.Claude{Bin: d.cfg.Claude.Bin},
		Progress:   progress,
		Exclusions: excl,
	}
	if d.tmdb != nil {
		pd.Meta = d.tmdb
	}
	if d.radarr != nil {
		pd.Discover = d.radarr
	}
	return pipeline.New(pd)
}

func (d *deps) requester() *request.Requester {
	r := &request.Requester{
		RadarrRootFolder:    d.cfg.Radarr.RootFolder,
		SonarrRootFolder:    d.cfg.Sonarr.RootFolder,
		MinimumAvailability: d.cfg.Radarr.MinimumAvailability,
	}
	if d.radarr != nil {
		r.Radarr = d.radarr
	}
	if d.sonarr != nil {
		r.Sonarr = d.sonarr
	}
	if d.tmdb != nil {
		r.TMDB = d.tmdb
	}
	return r
}

// runRequest builds a pipeline request from the per-kind settings.
func runRequest(cfg config.Config, kind media.Kind, vibe string, env []string) pipeline.Request {
	ks := cfg.Kind(kind)
	return pipeline.Request{
		Kind:         kind,
		Vibe:         vibe,
		Model:        ks.Model,
		Effort:       ks.Effort,
		Picks:        ks.Picks,
		Candidates:   ks.Candidates,
		FreePicks:    ks.FreePicks,
		Seeds:        ks.Seeds,
		TopTitles:    ks.TopTitles,
		HistoryDays:  cfg.HistoryDays,
		Region:       cfg.TMDB.Region,
		AgentEnv:     env,
		Timeout:      cfg.Claude.Timeout.Duration,
		MaxBudgetUSD: cfg.Claude.MaxBudgetUSD,
	}
}

// agentEnv is the environment handed to the claude binary, and the credential
// variable it carries ("" when the binary's own login is used).
func (d *deps) agentEnv() ([]string, string, error) {
	name, value, err := d.cfg.Claude.Auth()
	if err != nil {
		return nil, "", err
	}
	return agent.BuildEnv(os.Environ(), name, value), name, nil
}

// resolveAPIKeys fills an empty Sonarr/Radarr api_key from the app's
// /initialize.json. A failure leaves the key empty, so requireApp reports it.
func (c *cli) resolveAPIKeys(ctx context.Context, cfg *config.Config, kinds ...media.Kind) {
	hc := &http.Client{Timeout: 10 * time.Second}
	for _, k := range kinds {
		a := &cfg.Radarr
		if k == media.Series {
			a = &cfg.Sonarr
		}
		if a.URL == "" || a.APIKey != "" {
			continue
		}
		key, err := arr.FetchAPIKey(ctx, a.URL, hc)
		if err != nil {
			fmt.Fprintf(c.stderr, "note: %s api_key is not set and could not be read: %v\n", k.App(), err)
			continue
		}
		a.APIKey = key
		fmt.Fprintf(c.stderr, "note: %s api_key is not set; using the key from %s/initialize.json\n", k.App(), a.URL)
	}
}

// requireApp reports the missing settings for the kind's *arr app.
func requireApp(cfg config.Config, kind media.Kind) []string {
	a, prefix, key := cfg.Radarr, "PROPOSARR_RADARR_", "radarr"
	if kind == media.Series {
		a, prefix, key = cfg.Sonarr, "PROPOSARR_SONARR_", "sonarr"
	}
	var missing []string
	if a.URL == "" {
		missing = append(missing, prefix+"URL ("+key+".url)")
	}
	if a.APIKey == "" {
		missing = append(missing, prefix+"API_KEY ("+key+".api_key)")
	}
	return missing
}

func requireTMDB(cfg config.Config) []string {
	if cfg.TMDB.APIKey == "" {
		return []string{"PROPOSARR_TMDB_API_KEY (tmdb.api_key)"}
	}
	return nil
}

func missingError(what string, missing []string) error {
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s needs %s", what, strings.Join(missing, ", "))
}
