package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/web"
)

func (c *cli) checkCmd(ctx context.Context, args []string) error {
	fs, cfgPath := c.newFlags("check")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := c.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	c.resolveAPIKeys(ctx, &cfg, media.Movies, media.Series)

	failed := false
	for _, r := range runChecks(ctx, cfg, buildDeps(cfg)) {
		switch r.Status {
		case web.CheckOK:
			fmt.Fprintf(c.stdout, "ok    %s\n", strings.TrimSpace(r.Name+" "+r.Detail))
		case web.CheckSkip:
			fmt.Fprintf(c.stdout, "skip  %s (%s)\n", r.Name, r.Detail)
		default:
			failed = true
			fmt.Fprintf(c.stdout, "FAIL  %s: %s\n", r.Name, r.Detail)
		}
	}
	fmt.Fprintln(c.stdout, "\nRun `proposarr validate-token` to verify the Claude credential with a short model call.")
	if failed {
		return errFailed
	}
	return nil
}

// runChecks tests every configured connection. `check` and the web UI share it.
func runChecks(ctx context.Context, cfg config.Config, d *deps) []web.CheckResult {
	var out []web.CheckResult
	add := func(name, status, detail string) {
		out = append(out, web.CheckResult{Name: name, Status: status, Detail: detail})
	}
	timed := func(fn func(ctx context.Context) error) error {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return fn(ctx)
	}
	// ping reports a nil fn as not configured.
	ping := func(name string, fn func(ctx context.Context) error) {
		if fn == nil {
			add(name, web.CheckSkip, "not configured")
			return
		}
		if err := timed(fn); err != nil {
			add(name, web.CheckFail, err.Error())
			return
		}
		add(name, web.CheckOK, "")
	}

	var st arr.SystemStatus
	switch {
	case d.radarr != nil:
		if err := timed(func(ctx context.Context) (err error) { st, err = d.radarr.Status(ctx); return }); err != nil {
			add("Radarr", web.CheckFail, err.Error())
		} else {
			add("Radarr", web.CheckOK, st.Version)
		}
	case cfg.Radarr.URL != "":
		add("Radarr", web.CheckFail, "no api_key, and none could be read from initialize.json")
	default:
		add("Radarr", web.CheckSkip, "not configured")
	}
	switch {
	case d.sonarr != nil:
		if err := timed(func(ctx context.Context) (err error) { st, err = d.sonarr.Status(ctx); return }); err != nil {
			add("Sonarr", web.CheckFail, err.Error())
		} else {
			add("Sonarr", web.CheckOK, st.Version)
		}
	case cfg.Sonarr.URL != "":
		add("Sonarr", web.CheckFail, "no api_key, and none could be read from initialize.json")
	default:
		add("Sonarr", web.CheckSkip, "not configured")
	}
	if d.radarr == nil && d.sonarr == nil {
		add("Radarr or Sonarr", web.CheckFail, "at least one must be configured")
	}

	if d.tmdb == nil {
		add("TMDB", web.CheckFail, "not configured, PROPOSARR_TMDB_API_KEY is required")
	} else if err := timed(d.tmdb.Ping); err != nil {
		add("TMDB", web.CheckFail, err.Error())
	} else {
		add("TMDB", web.CheckOK, "")
	}

	if d.plex != nil {
		ping("Plex", d.plex.Ping)
	} else {
		ping("Plex", nil)
	}
	if d.jellyfin != nil {
		ping("Jellyfin", d.jellyfin.Ping)
	} else {
		ping("Jellyfin", nil)
	}

	bin := cfg.Claude.Bin
	if bin == "" {
		bin = "claude"
	}
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	v, err := exec.CommandContext(vctx, bin, "--version").Output()
	cancel()
	if err != nil {
		add("claude", web.CheckFail, fmt.Sprintf("binary %q: %v", bin, err))
	} else {
		add("claude", web.CheckOK, strings.TrimSpace(string(v)))
	}

	switch name, _, err := cfg.Claude.Auth(); {
	case err != nil:
		add("Claude auth", web.CheckFail, err.Error())
	case name == "":
		add("Claude auth", web.CheckOK, "via local claude login")
	default:
		add("Claude auth", web.CheckOK, "via "+name)
	}
	return out
}
