package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
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
	d := buildDeps(cfg)

	failed := false
	ok := func(format string, a ...any) { fmt.Fprintf(c.stdout, "ok    %s\n", fmt.Sprintf(format, a...)) }
	skip := func(name string) { fmt.Fprintf(c.stdout, "skip  %s (not configured)\n", name) }
	fail := func(format string, a ...any) {
		failed = true
		fmt.Fprintf(c.stdout, "FAIL  %s\n", fmt.Sprintf(format, a...))
	}
	timed := func(fn func(ctx context.Context) error) error {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return fn(ctx)
	}

	var st arr.SystemStatus
	if d.radarr == nil && cfg.Radarr.URL != "" {
		fail("Radarr: no api_key (see note above)")
	} else if d.radarr == nil {
		skip("Radarr")
	} else if err := timed(func(ctx context.Context) (err error) { st, err = d.radarr.Status(ctx); return }); err != nil {
		fail("Radarr: %v", err)
	} else {
		ok("Radarr %s", st.Version)
	}
	if d.sonarr == nil && cfg.Sonarr.URL != "" {
		fail("Sonarr: no api_key (see note above)")
	} else if d.sonarr == nil {
		skip("Sonarr")
	} else if err := timed(func(ctx context.Context) (err error) { st, err = d.sonarr.Status(ctx); return }); err != nil {
		fail("Sonarr: %v", err)
	} else {
		ok("Sonarr %s", st.Version)
	}
	if d.radarr == nil && d.sonarr == nil {
		fail("Radarr or Sonarr must be configured")
	}

	if d.tmdb == nil {
		fail("TMDB (not configured, PROPOSARR_TMDB_API_KEY is required)")
	} else if err := timed(d.tmdb.Ping); err != nil {
		fail("TMDB: %v", err)
	} else {
		ok("TMDB")
	}

	if d.plex == nil {
		skip("Plex")
	} else if err := timed(d.plex.Ping); err != nil {
		fail("Plex: %v", err)
	} else {
		ok("Plex")
	}
	if d.jellyfin == nil {
		skip("Jellyfin")
	} else if err := timed(d.jellyfin.Ping); err != nil {
		fail("Jellyfin: %v", err)
	} else {
		ok("Jellyfin")
	}

	bin := cfg.Claude.Bin
	if bin == "" {
		bin = "claude"
	}
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	out, err := exec.CommandContext(vctx, bin, "--version").Output()
	cancel()
	if err != nil {
		fail("claude binary %q: %v", bin, err)
	} else {
		ok("claude %s", strings.TrimSpace(string(out)))
	}

	switch name, _, err := cfg.Claude.Auth(); {
	case err != nil:
		fail("Claude auth: %v", err)
	case name == "":
		ok("Claude auth: local claude login")
	default:
		ok("Claude auth: %s", name)
	}

	fmt.Fprintln(c.stdout, "\nRun `proposarr validate-token` to verify the Claude credential with a short model call.")
	if failed {
		return errFailed
	}
	return nil
}
