package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/store"
	"github.com/leandervdo/proposarr/internal/web"
	ui "github.com/leandervdo/proposarr/web"
)

func (c *cli) serveCmd(ctx context.Context, args []string) error {
	fs, cfgPath := c.newFlags("serve")
	listen := fs.String("listen", "", "address to listen on (default $PROPOSARR_LISTEN, else :8585)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := c.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if _, _, err := cfg.Claude.Auth(); err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(c.stderr, nil))
	c.resolveAPIKeys(ctx, &cfg, media.Movies, media.Series)

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "proposarr.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ws := web.New(serverOptions(cfg, buildDeps(cfg), st, log))
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: ws.Handler(), ReadHeaderTimeout: 10 * time.Second}
	// Event streams never go idle on their own; close them when shutdown starts.
	srv.RegisterOnShutdown(ws.CloseEvents)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("proposarr listening", "addr", ln.Addr().String(), "version", version, "web_auth", cfg.Web.AuthEnabled())

	select {
	case <-ctx.Done():
	case err := <-errc:
		ws.Shutdown()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(sctx)
	ws.Shutdown()
	return shutdownErr
}

// serverOptions wires the web server to the same adapters the CLI uses.
func serverOptions(cfg config.Config, d *deps, st store.Store, log *slog.Logger) web.Options {
	req := d.requester()
	// The web chooser applies the root folder from the request, else the configured one.
	req.RadarrRootFolder, req.SonarrRootFolder = "", ""
	return web.Options{
		Version: version,
		Config:  cfg,
		Store:   st,
		UI:      ui.Dist(),
		Logger:  log,
		NewRunner: func(progress func(string)) web.Runner {
			return d.pipeline(false, progress, st)
		},
		RunRequest: func(kind media.Kind, vibe string) (pipeline.Request, error) {
			missing := append(requireTMDB(cfg), requireApp(cfg, kind)...)
			if err := missingError("a "+string(kind)+" run", missing); err != nil {
				return pipeline.Request{}, err
			}
			env, _, err := d.agentEnv()
			if err != nil {
				return pipeline.Request{}, err
			}
			return runRequest(cfg, kind, vibe, env), nil
		},
		Adder: req,
		App: func(app string) (web.AppCatalog, string, bool) {
			switch {
			case app == "radarr" && d.radarr != nil:
				return d.radarr, cfg.Radarr.RootFolder, true
			case app == "sonarr" && d.sonarr != nil:
				return d.sonarr, cfg.Sonarr.RootFolder, true
			}
			return nil, "", false
		},
		Library: func(ctx context.Context, kind media.Kind) ([]media.Title, error) {
			return d.library(false).Titles(ctx, kind)
		},
		Check: func(ctx context.Context) []web.CheckResult {
			return runChecks(ctx, cfg, d)
		},
	}
}
