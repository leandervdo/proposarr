// Package web serves the Proposarr HTTP API, server-sent events and the UI.
package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/store"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

// Runner runs one recommendation pass (pipeline.Pipeline).
type Runner interface {
	Run(ctx context.Context, req pipeline.Request) (*pipeline.Run, error)
}

// Adder adds a title to Sonarr or Radarr (request.Requester).
type Adder interface {
	Add(ctx context.Context, item request.Item, ch request.Chooser) (request.Result, error)
	// SwitchProfile moves an added movie to another quality profile and searches again.
	SwitchProfile(ctx context.Context, item request.Item, movieID, profileID int) (request.Result, error)
	// SearchExisting monitors a movie already in Radarr, moves it to profileID
	// when that differs, and has Radarr search for it.
	SearchExisting(ctx context.Context, tmdbID, profileID int, fallback request.Fallback) (request.Result, error)
}

// AppCatalog lists the quality profiles and root folders of an *arr app.
type AppCatalog interface {
	QualityProfiles(ctx context.Context) ([]arr.QualityProfile, error)
	RootFolders(ctx context.Context) ([]arr.RootFolder, error)
}

const (
	CheckOK   = "ok"
	CheckFail = "fail"
	CheckSkip = "skip"
)

type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Options wires the server to its ports. Nil functions disable their routes.
type Options struct {
	Version string
	// Config returns the current effective settings (API keys already resolved);
	// it changes when settings are saved. Secrets never leave the server.
	Config   func() config.Config
	Settings SettingsService
	Store    store.Store

	NewRunner func(progress func(string)) Runner
	// RunRequest builds a run's settings; useTaste false is an open search.
	RunRequest func(kind media.Kind, vibe string, useTaste bool) (pipeline.Request, error)
	Adder      Adder
	App        func(app string) (catalog AppCatalog, defaultRootFolder string, ok bool)
	Library    func(ctx context.Context, kind media.Kind) ([]media.Title, error)
	// Radarr reads library movies live, for owned matches. It returns nil when
	// Radarr is not configured.
	Radarr func() RadarrLibrary
	Check  func(ctx context.Context) []CheckResult
	// Title fetches one title's details from TMDB, with streaming providers for
	// region. An unknown id returns an error wrapping tmdb.ErrNotFound.
	Title func(ctx context.Context, kind media.Kind, tmdbID int, region string) (tmdb.FullDetails, error)
	// Ratings looks up a title's real ratings (Radarr/Sonarr). Optional; an
	// error only leaves the ratings out.
	Ratings func(ctx context.Context, kind media.Kind, tmdbID int) (*pipeline.Ratings, error)

	UI     fs.FS
	Logger *slog.Logger
	Now    func() time.Time
}

type Server struct {
	o      Options
	log    *slog.Logger
	now    func() time.Time
	events *hub
	titles *titleCache

	ctx    context.Context // server lifetime, parent of every run
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	running   map[media.Kind]*activeRun
	following map[int64]bool // picks whose release check runs in the background
	searching map[int]bool   // library movies, by TMDB id, whose search runs in the background
}

func New(o Options) *Server {
	s := &Server{o: o, log: o.Logger, now: o.Now, events: newHub(), titles: newTitleCache(titleCacheTTL, titleCacheSize),
		running: map[media.Kind]*activeRun{}, following: map[int64]bool{}, searching: map[int]bool{}}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/connections/check", s.checkConnections)
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/settings/test", s.testSettings)
	mux.HandleFunc("GET /api/runs", s.listRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/runs/{id}/owned", s.runOwned)
	mux.HandleFunc("POST /api/runs", s.createRun)
	mux.HandleFunc("GET /api/picks", s.listPicks)
	mux.HandleFunc("POST /api/picks/{id}/verdict", s.setVerdict)
	mux.HandleFunc("POST /api/picks/{id}/request", s.requestPick)
	mux.HandleFunc("POST /api/picks/{id}/request/profile", s.switchProfile)
	mux.HandleFunc("GET /api/apps/{app}/options", s.appOptions)
	mux.HandleFunc("GET /api/library", s.library)
	mux.HandleFunc("POST /api/library/movies/{tmdb_id}/search", s.searchLibraryMovie)
	mux.HandleFunc("GET /api/titles/{kind}/{tmdb_id}", s.titleDetails)
	mux.HandleFunc("GET /api/events", s.streamEvents)
	mux.HandleFunc("/", s.static)
	return securityHeaders(s.auth(mux))
}

// CloseEvents disconnects every event stream so http.Server.Shutdown is not held open.
func (s *Server) CloseEvents() { s.events.close() }

// Shutdown refuses new runs, cancels running ones and waits until they are recorded.
func (s *Server) Shutdown() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	s.events.close()
}

// cfg is the current effective configuration.
func (s *Server) cfg() config.Config {
	if s.o.Config == nil {
		return config.Default()
	}
	return s.o.Config()
}

// auth reads the web login once: it is file/environment only, not UI-editable.
func (s *Server) auth(next http.Handler) http.Handler {
	web := s.cfg().Web
	user, pass := web.Username, web.Password
	if user == "" || pass == "" {
		return next
	}
	wantUser, wantPass := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pass))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		gotUser, gotPass := sha256.Sum256([]byte(u)), sha256.Sum256([]byte(p))
		match := subtle.ConstantTimeCompare(gotUser[:], wantUser[:]) & subtle.ConstantTimeCompare(gotPass[:], wantPass[:])
		if !ok || match != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Proposarr", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
