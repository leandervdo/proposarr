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
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/settings"
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
	path, err := resolveConfigPath(*cfgPath, c.getenv)
	if err != nil {
		return err
	}
	// The file and environment decide where the data lives; everything else can
	// come from the web UI.
	boot, err := config.Load(path, c.getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(c.stderr, nil))

	if err := os.MkdirAll(boot.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	st, err := store.Open(filepath.Join(boot.DataDir, "proposarr.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()
	key, err := settings.LoadKey(boot.DataDir, c.getenv, true)
	if err != nil {
		return err
	}
	ciph, err := settings.NewCipher(key)
	if err != nil {
		return err
	}
	svc := settings.NewService(settings.Options{ConfigPath: path, Getenv: c.getenv, Backend: st, Cipher: ciph, Logger: log})
	rt := newRuntime(svc, log, *listen)
	if err := rt.reload(ctx); err != nil {
		return err
	}
	cur := rt.state()
	if _, _, err := cur.cfg.Claude.Auth(); err != nil {
		log.Warn("Claude credentials", "err", err)
	}
	if missing := settings.Missing(cur.cfg); len(missing) > 0 {
		log.Info("setup required: open the web UI to configure Proposarr", "missing", strings.Join(missing, ", "))
	}

	ws := web.New(serverOptions(rt, st, log))
	ln, err := net.Listen("tcp", cur.cfg.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: ws.Handler(), ReadHeaderTimeout: 10 * time.Second}
	// Event streams never go idle on their own; close them when shutdown starts.
	srv.RegisterOnShutdown(ws.CloseEvents)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("proposarr listening", "addr", ln.Addr().String(), "version", version, "web_auth", cur.cfg.Web.AuthEnabled())

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

// serverOptions wires the web server to the runtime's current adapters, the
// same ones the CLI uses. Every call reads the latest state.
func serverOptions(rt *runtime, st store.Store, log *slog.Logger) web.Options {
	return web.Options{
		Version:  version,
		Config:   func() config.Config { return rt.state().cfg },
		Settings: rt,
		Store:    st,
		UI:       ui.Dist(),
		Logger:   log,
		NewRunner: func(progress func(string)) web.Runner {
			return rt.state().deps.pipeline(false, progress, st)
		},
		RunRequest: func(kind media.Kind, vibe string, useTaste bool) (pipeline.Request, error) {
			s := rt.state()
			missing := append(requireTMDB(s.cfg), requireApp(s.cfg, kind)...)
			if err := missingError("a "+string(kind)+" run", missing); err != nil {
				return pipeline.Request{}, err
			}
			env, _, err := s.deps.agentEnv()
			if err != nil {
				return pipeline.Request{}, err
			}
			return runRequest(s.cfg, kind, vibe, !useTaste, env), nil
		},
		Adder: adderFunc(func(ctx context.Context, item request.Item, ch request.Chooser) (request.Result, error) {
			req := rt.state().deps.requester()
			// The web chooser applies the root folder from the request, else the configured one.
			req.RadarrRootFolder, req.SonarrRootFolder = "", ""
			return req.Add(ctx, item, ch)
		}),
		App: func(app string) (web.AppCatalog, string, bool) {
			s := rt.state()
			switch {
			case app == "radarr" && s.deps.radarr != nil:
				return s.deps.radarr, s.cfg.Radarr.RootFolder, true
			case app == "sonarr" && s.deps.sonarr != nil:
				return s.deps.sonarr, s.cfg.Sonarr.RootFolder, true
			}
			return nil, "", false
		},
		Library: func(ctx context.Context, kind media.Kind) ([]media.Title, error) {
			return rt.state().deps.library(false).Titles(ctx, kind)
		},
		Check: func(ctx context.Context) []web.CheckResult {
			s := rt.state()
			return runChecks(ctx, s.cfg, s.deps)
		},
	}
}

type adderFunc func(ctx context.Context, item request.Item, ch request.Chooser) (request.Result, error)

func (f adderFunc) Add(ctx context.Context, item request.Item, ch request.Chooser) (request.Result, error) {
	return f(ctx, item, ch)
}
