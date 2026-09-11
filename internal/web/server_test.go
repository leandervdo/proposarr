package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/store"
)

// fakeStore is an in-memory store.Store.
type fakeStore struct {
	mu         sync.Mutex
	nextID     int64
	runs       map[int64]store.Run
	picks      map[int64]store.Pick
	requests   []store.Request
	profile    *profile.Profile
	lastFilter store.PickFilter
}

func newFakeStore() *fakeStore {
	return &fakeStore{runs: map[int64]store.Run{}, picks: map[int64]store.Pick{}}
}

func (f *fakeStore) addPick(p store.Pick) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.picks[p.ID] = p
}

func (f *fakeStore) CreateRun(_ context.Context, r store.Run) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	r.ID = f.nextID
	f.runs[r.ID] = r
	return r.ID, nil
}

func (f *fakeStore) FinishRun(_ context.Context, id int64, run *pipeline.Run, runErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[id]
	now := time.Now()
	r.FinishedAt = &now
	r.Status = store.RunSucceeded
	if runErr != nil {
		r.Status, r.Error = store.RunFailed, runErr.Error()
	}
	if run != nil {
		r.PickCount = len(run.Picks)
		for _, p := range run.Picks {
			f.nextID++
			f.picks[f.nextID] = store.Pick{ID: f.nextID, RunID: id, Pick: p}
		}
	}
	f.runs[id] = r
	return nil
}

func (f *fakeStore) ListRuns(_ context.Context, limit int) ([]store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Run
	for _, r := range f.runs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out[:min(limit, len(out))], nil
}

func (f *fakeStore) GetRun(_ context.Context, id int64) (store.Run, []store.Pick, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return store.Run{}, nil, store.ErrNotFound
	}
	var picks []store.Pick
	for _, p := range f.picks {
		if p.RunID == id {
			picks = append(picks, p)
		}
	}
	return r, picks, nil
}

func (f *fakeStore) LatestProfile(context.Context, media.Kind) (*profile.Profile, error) {
	return f.profile, nil
}

func (f *fakeStore) ListPicks(_ context.Context, filter store.PickFilter) ([]store.Pick, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastFilter = filter
	var out []store.Pick
	for _, p := range f.picks {
		if filter.Kind != "" && p.Kind != filter.Kind {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeStore) GetPick(_ context.Context, id int64) (store.Pick, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.picks[id]
	if !ok {
		return store.Pick{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) SetVerdict(_ context.Context, kind media.Kind, tmdbID int, v store.Verdict, until *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, p := range f.picks {
		if p.Kind == kind && p.TMDBID == tmdbID {
			p.Verdict, p.LaterUntil = v, until
			f.picks[id] = p
		}
	}
	return nil
}

func (f *fakeStore) RecordRequest(_ context.Context, r store.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	if p, ok := f.picks[r.PickID]; ok {
		req := r
		p.Request = &req
		f.picks[r.PickID] = p
	}
	return nil
}

func (f *fakeStore) Excluded(context.Context, media.Kind) (map[int]bool, error) { return nil, nil }
func (f *fakeStore) Close() error                                               { return nil }

// fakeRunner reports progress, then waits for release (or cancellation).
type fakeRunner struct {
	progress func(string)
	release  chan struct{}
	run      *pipeline.Run
	err      error
}

func (r *fakeRunner) Run(ctx context.Context, req pipeline.Request) (*pipeline.Run, error) {
	r.progress("Loading " + req.Kind.App() + " library…")
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.run, r.err
}

type fakeCatalog struct {
	profiles []arr.QualityProfile
	folders  []arr.RootFolder
}

func (c fakeCatalog) QualityProfiles(context.Context) ([]arr.QualityProfile, error) {
	return c.profiles, nil
}
func (c fakeCatalog) RootFolders(context.Context) ([]arr.RootFolder, error) { return c.folders, nil }

// fakeAdder behaves like request.Requester towards its chooser.
type fakeAdder struct {
	mu      sync.Mutex
	calls   int
	err     error
	catalog fakeCatalog
}

func (a *fakeAdder) Add(ctx context.Context, item request.Item, ch request.Chooser) (request.Result, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if a.err != nil {
		return request.Result{}, a.err
	}
	qp, err := ch.ChooseQualityProfile(ctx, item, "Radarr", a.catalog.profiles)
	if err != nil {
		return request.Result{}, err
	}
	folder := a.catalog.folders[0]
	if len(a.catalog.folders) > 1 {
		if folder, err = ch.ChooseRootFolder(ctx, item, "Radarr", a.catalog.folders); err != nil {
			return request.Result{}, err
		}
	}
	return request.Result{App: "Radarr", ID: 42, Title: item.Title, QualityProfile: qp.Name, RootFolder: folder.Path}, nil
}

var testCatalog = fakeCatalog{
	profiles: []arr.QualityProfile{{ID: 4, Name: "HD-1080p"}, {ID: 5, Name: "Ultra-HD"}},
	folders:  []arr.RootFolder{{ID: 1, Path: "/media/movies", FreeSpace: 1 << 40}},
}

type testEnv struct {
	srv     *Server
	h       http.Handler
	store   *fakeStore
	adder   *fakeAdder
	release chan struct{}
}

func newEnv(t *testing.T, mutate func(o *Options)) *testEnv {
	t.Helper()
	env := &testEnv{store: newFakeStore(), adder: &fakeAdder{catalog: testCatalog}, release: make(chan struct{})}
	cfg := config.Default()
	cfg.Radarr = config.Arr{URL: "http://radarr:7878", APIKey: "radarr-secret-key", MinimumAvailability: "released"}
	cfg.TMDB.APIKey = "tmdb-secret-key"
	o := Options{
		Version: "test",
		Config:  func() config.Config { return cfg },
		Store:   env.store,
		NewRunner: func(progress func(string)) Runner {
			return &fakeRunner{progress: progress, release: env.release, run: &pipeline.Run{
				Kind:  media.Movies,
				Picks: []pipeline.Pick{{TMDBID: 329865, Kind: media.Movies, Title: "Arrival", Year: 2016, Score: 92}},
			}}
		},
		RunRequest: func(kind media.Kind, vibe string) (pipeline.Request, error) {
			if kind == media.Series {
				return pipeline.Request{}, errors.New("a series run needs PROPOSARR_SONARR_URL (sonarr.url)")
			}
			return pipeline.Request{Kind: kind, Vibe: vibe, Model: "claude-sonnet-5", Effort: "medium"}, nil
		},
		Adder: env.adder,
		App: func(app string) (AppCatalog, string, bool) {
			if app == "radarr" {
				return testCatalog, "", true
			}
			return nil, "", false
		},
		Library: func(context.Context, media.Kind) ([]media.Title, error) {
			return []media.Title{{TMDBID: 603, Title: "The Matrix", Year: 1999, PosterURL: "https://img/p.jpg"}}, nil
		},
		Check: func(context.Context) []CheckResult {
			return []CheckResult{{Name: "Radarr", Status: CheckOK, Detail: "6.3.0"}}
		},
	}
	if mutate != nil {
		mutate(&o)
	}
	env.srv = New(o)
	env.h = env.srv.Handler()
	t.Cleanup(func() {
		select {
		case <-env.release:
		default:
			close(env.release)
		}
		env.srv.Shutdown()
	})
	return env
}

// withConfig changes the configuration the server under test sees.
func withConfig(o *Options, change func(*config.Config)) {
	c := o.Config()
	change(&c)
	o.Config = func() config.Config { return c }
}

func (e *testEnv) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, code, rec.Body.String())
	}
}

func TestHealthzAndSecurityHeaders(t *testing.T) {
	env := newEnv(t, func(o *Options) {
		withConfig(o, func(c *config.Config) { c.Web = config.Web{Username: "admin", Password: "pw"} })
	})
	rec := env.do(t, "GET", "/healthz", "")
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	for h, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "same-origin", "X-Frame-Options": "DENY"} {
		if got := rec.Header().Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
}

func TestBasicAuth(t *testing.T) {
	env := newEnv(t, func(o *Options) {
		withConfig(o, func(c *config.Config) { c.Web = config.Web{Username: "admin", Password: "pw"} })
	})
	for _, target := range []string{"/api/status", "/", "/api/events"} {
		rec := env.do(t, "GET", target, "")
		wantStatus(t, rec, http.StatusUnauthorized)
		if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
			t.Errorf("%s: missing WWW-Authenticate", target)
		}
	}
	for _, c := range []struct {
		user, pass string
		code       int
	}{{"admin", "wrong", 401}, {"root", "pw", 401}, {"admin", "pw", 200}} {
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.SetBasicAuth(c.user, c.pass)
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		if rec.Code != c.code {
			t.Errorf("%s/%s: status %d, want %d", c.user, c.pass, rec.Code, c.code)
		}
	}
}

func TestStatus(t *testing.T) {
	env := newEnv(t, nil)
	rec := env.do(t, "GET", "/api/status", "")
	wantStatus(t, rec, http.StatusOK)
	got := decode[struct {
		Version     string          `json:"version"`
		ClaudeAuth  string          `json:"claude_auth"`
		Connections map[string]bool `json:"connections"`
		Running     []activeRun     `json:"running"`
	}](t, rec)
	if got.Version != "test" || got.ClaudeAuth != "local" || !got.Connections["radarr"] || got.Connections["sonarr"] || !got.Connections["tmdb"] {
		t.Fatalf("status = %+v", got)
	}
	if got.Running == nil || len(got.Running) != 0 || !strings.Contains(rec.Body.String(), `"running":[]`) {
		t.Fatalf("running = %s", rec.Body.String())
	}
}

func TestConfigHasNoSecrets(t *testing.T) {
	env := newEnv(t, func(o *Options) {
		withConfig(o, func(c *config.Config) {
			c.Plex = config.Plex{URL: "http://user:plexpass@plex:32400?X-Plex-Token=plex-secret-token", Token: "plex-secret-token"}
			c.Claude.OAuthToken = "sk-ant-oat-secret"
			c.Web = config.Web{Username: "admin", Password: "web-secret-pass"}
		})
	})
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.SetBasicAuth("admin", "web-secret-pass")
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, secret := range []string{"radarr-secret-key", "tmdb-secret-key", "plex-secret-token", "plexpass", "sk-ant-oat-secret", "web-secret-pass"} {
		if strings.Contains(body, secret) {
			t.Errorf("/api/config leaks %q: %s", secret, body)
		}
	}
	for _, want := range []string{`"api_key_set":true`, `"token_set":true`, `"auth":"oauth_token"`, `"auth_enabled":true`, `"url":"http://radarr:7878"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/api/config missing %s: %s", want, body)
		}
	}
}

func TestRunLifecycle(t *testing.T) {
	env := newEnv(t, nil)
	events, unsubscribe := env.srv.events.subscribe()
	defer unsubscribe()

	rec := env.do(t, "POST", "/api/runs", `{"kind":"movies","vibe":"  slow burn  "}`)
	wantStatus(t, rec, http.StatusAccepted)
	started := decode[store.Run](t, rec)
	if started.ID == 0 || started.Status != store.RunRunning || started.Vibe != "slow burn" || started.Model != "claude-sonnet-5" {
		t.Fatalf("started = %+v", started)
	}
	if !strings.Contains(rec.Body.String(), `"warnings":[]`) {
		t.Errorf("warnings should be an empty array: %s", rec.Body.String())
	}

	wantStatus(t, env.do(t, "POST", "/api/runs", `{"kind":"movies"}`), http.StatusConflict)

	var names []string
	next := func() event {
		t.Helper()
		select {
		case ev := <-events:
			names = append(names, ev.name)
			return ev
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event after %v", names)
			return event{}
		}
	}
	next() // run.started
	progress := next()
	if !strings.Contains(string(progress.data), "Loading Radarr library") {
		t.Fatalf("progress = %s", progress.data)
	}
	status := decode[struct {
		Running []activeRun `json:"running"`
	}](t, env.do(t, "GET", "/api/status", ""))
	if len(status.Running) != 1 || status.Running[0].RunID != started.ID || status.Running[0].Message != "Loading Radarr library…" {
		t.Fatalf("running = %+v", status.Running)
	}

	close(env.release)
	finished := next()
	if got := strings.Join(names, ","); got != "run.started,run.progress,run.finished" {
		t.Fatalf("events = %s", got)
	}
	var fin store.Run
	if err := json.Unmarshal(finished.data, &fin); err != nil || fin.Status != store.RunSucceeded || fin.PickCount != 1 {
		t.Fatalf("finished = %s (%v)", finished.data, err)
	}

	// The slot is free again once run.finished was published.
	env.release = make(chan struct{})
	wantStatus(t, env.do(t, "POST", "/api/runs", `{"kind":"movies"}`), http.StatusAccepted)

	runs := decode[[]store.Run](t, env.do(t, "GET", "/api/runs?limit=1", ""))
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	got := env.do(t, "GET", fmt.Sprintf("/api/runs/%d", started.ID), "")
	wantStatus(t, got, http.StatusOK)
	detail := decode[struct {
		Run   store.Run    `json:"run"`
		Picks []store.Pick `json:"picks"`
	}](t, got)
	if detail.Run.ID != started.ID || len(detail.Picks) != 1 || detail.Picks[0].Title != "Arrival" {
		t.Fatalf("detail = %+v", detail)
	}
	if !strings.Contains(got.Body.String(), `"related_to":[]`) {
		t.Errorf("related_to should be an empty array: %s", got.Body.String())
	}
	wantStatus(t, env.do(t, "GET", "/api/runs/999", ""), http.StatusNotFound)
	wantStatus(t, env.do(t, "GET", "/api/runs/abc", ""), http.StatusBadRequest)
	wantStatus(t, env.do(t, "GET", "/api/runs?limit=0", ""), http.StatusBadRequest)
}

func TestCreateRunErrors(t *testing.T) {
	env := newEnv(t, nil)
	wantStatus(t, env.do(t, "POST", "/api/runs", `{"kind":"books"}`), http.StatusBadRequest)
	wantStatus(t, env.do(t, "POST", "/api/runs", `not json`), http.StatusBadRequest)
	rec := env.do(t, "POST", "/api/runs", `{"kind":"series"}`)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "PROPOSARR_SONARR_URL") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	// A failed start must not leave the kind marked as running.
	wantStatus(t, env.do(t, "POST", "/api/runs", `{"kind":"series"}`), http.StatusBadRequest)
}

func TestSSEDeliversProgress(t *testing.T) {
	env := newEnv(t, nil)
	ts := httptest.NewServer(env.h)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	lines := bufio.NewScanner(resp.Body)
	if !lines.Scan() || lines.Text() != ": connected" {
		t.Fatalf("first line = %q", lines.Text())
	}

	rec := env.do(t, "POST", "/api/runs", `{"kind":"movies"}`)
	wantStatus(t, rec, http.StatusAccepted)
	for lines.Scan() {
		if lines.Text() == "event: run.progress" {
			if !lines.Scan() || !strings.HasPrefix(lines.Text(), "data: ") || !strings.Contains(lines.Text(), `"message":"Loading Radarr library…"`) {
				t.Fatalf("data line = %q", lines.Text())
			}
			return
		}
	}
	t.Fatalf("stream ended without run.progress: %v", lines.Err())
}

func TestListPicksFilters(t *testing.T) {
	env := newEnv(t, nil)
	env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 10, Kind: media.Movies, Title: "A"}})

	rec := env.do(t, "GET", "/api/picks", "")
	wantStatus(t, rec, http.StatusOK)
	if f := env.store.lastFilter; !f.LatestRun || f.Verdict != nil || f.Kind != "" {
		t.Fatalf("default filter = %+v", f)
	}
	wantStatus(t, env.do(t, "GET", "/api/picks?kind=movies&run=7&verdict=none", ""), http.StatusOK)
	if f := env.store.lastFilter; f.LatestRun || f.RunID != 7 || f.Kind != media.Movies || f.Verdict == nil || *f.Verdict != store.VerdictNone {
		t.Fatalf("filter = %+v", f)
	}
	wantStatus(t, env.do(t, "GET", "/api/picks?verdict=later", ""), http.StatusOK)
	if f := env.store.lastFilter; f.Verdict == nil || *f.Verdict != store.VerdictLater {
		t.Fatalf("filter = %+v", f)
	}
	for _, q := range []string{"verdict=maybe", "run=x", "run=-1", "kind=books"} {
		wantStatus(t, env.do(t, "GET", "/api/picks?"+q, ""), http.StatusBadRequest)
	}
}

func TestSetVerdict(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	env := newEnv(t, func(o *Options) { o.Now = func() time.Time { return now } })
	env.store.addPick(store.Pick{ID: 3, RunID: 1, Pick: pipeline.Pick{TMDBID: 10, Kind: media.Movies, Title: "A"}})
	events, unsubscribe := env.srv.events.subscribe()
	defer unsubscribe()

	rec := env.do(t, "POST", "/api/picks/3/verdict", `{"verdict":"later"}`)
	wantStatus(t, rec, http.StatusOK)
	p := decode[store.Pick](t, rec)
	if p.Verdict != store.VerdictLater || p.LaterUntil == nil || !p.LaterUntil.Equal(now.AddDate(0, 0, 30)) {
		t.Fatalf("pick = %+v", p)
	}
	if ev := <-events; ev.name != "pick.updated" {
		t.Fatalf("event = %s", ev.name)
	}

	p = decode[store.Pick](t, env.do(t, "POST", "/api/picks/3/verdict", `{"verdict":"later","later_days":7}`))
	if !p.LaterUntil.Equal(now.AddDate(0, 0, 7)) {
		t.Fatalf("later_days ignored: %+v", p.LaterUntil)
	}
	p = decode[store.Pick](t, env.do(t, "POST", "/api/picks/3/verdict", `{"verdict":""}`))
	if p.Verdict != store.VerdictNone {
		t.Fatalf("undo: %+v", p)
	}

	wantStatus(t, env.do(t, "POST", "/api/picks/3/verdict", `{"verdict":"love"}`), http.StatusBadRequest)
	wantStatus(t, env.do(t, "POST", "/api/picks/3/verdict", `{}`), http.StatusBadRequest)
	wantStatus(t, env.do(t, "POST", "/api/picks/99/verdict", `{"verdict":"ignored"}`), http.StatusNotFound)
}

func TestRequestRequiresQualityProfile(t *testing.T) {
	env := newEnv(t, nil)
	env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 329865, Kind: media.Movies, Title: "Arrival"}})
	for _, body := range []string{`{}`, `{"quality_profile_id":0}`, `{"quality_profile_id":-3,"root_folder":"/media/movies"}`} {
		rec := env.do(t, "POST", "/api/picks/1/request", body)
		wantStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), "quality_profile_id is required") {
			t.Errorf("%s: body = %s", body, rec.Body.String())
		}
	}
	// Even for an unknown pick the missing choice is reported first.
	wantStatus(t, env.do(t, "POST", "/api/picks/999/request", `{}`), http.StatusBadRequest)
	if env.adder.calls != 0 {
		t.Fatalf("adder called %d times without a quality profile", env.adder.calls)
	}
	if len(env.store.requests) != 0 {
		t.Fatalf("requests recorded: %+v", env.store.requests)
	}
}

func TestRequestSuccess(t *testing.T) {
	env := newEnv(t, nil)
	env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 329865, Kind: media.Movies, Title: "Arrival", Year: 2016}})

	rec := env.do(t, "POST", "/api/picks/1/request", `{"quality_profile_id":5}`)
	wantStatus(t, rec, http.StatusOK)
	p := decode[store.Pick](t, rec)
	if p.Verdict != store.VerdictAccepted || p.Request == nil {
		t.Fatalf("pick = %+v", p)
	}
	r := *p.Request
	if r.App != "radarr" || r.Status != "added" || r.QualityProfile != "Ultra-HD" || r.RootFolder != "/media/movies" || r.TargetID != 42 {
		t.Fatalf("request = %+v", r)
	}
	if env.adder.calls != 1 {
		t.Fatalf("adder calls = %d", env.adder.calls)
	}
}

func TestRequestErrors(t *testing.T) {
	cases := []struct {
		name       string
		adderErr   error
		folders    []arr.RootFolder
		body       string
		code       int
		wantBody   string
		wantFailed bool
	}{
		{name: "already in library", adderErr: fmt.Errorf("Arrival (2016): %w", request.ErrAlreadyInLibrary), body: `{"quality_profile_id":4}`, code: 409},
		{name: "not configured", adderErr: fmt.Errorf("Radarr: %w", request.ErrNotConfigured), body: `{"quality_profile_id":4}`, code: 400},
		{name: "unknown profile", body: `{"quality_profile_id":77}`, code: 400, wantBody: "no quality profile 77"},
		{name: "several folders, none chosen", folders: []arr.RootFolder{{Path: "/a"}, {Path: "/b"}}, body: `{"quality_profile_id":4}`, code: 400, wantBody: "choose one: /a, /b"},
		{name: "several folders, chosen", folders: []arr.RootFolder{{Path: "/a"}, {Path: "/b"}}, body: `{"quality_profile_id":4,"root_folder":"/b"}`, code: 200},
		{name: "unknown folder", folders: []arr.RootFolder{{Path: "/a"}, {Path: "/b"}}, body: `{"quality_profile_id":4,"root_folder":"/c"}`, code: 400, wantBody: `no root folder \"/c\"`},
		{name: "upstream failure", adderErr: errors.New("radarr: HTTP 500"), body: `{"quality_profile_id":4}`, code: 502, wantFailed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t, nil)
			env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 329865, Kind: media.Movies, Title: "Arrival"}})
			env.adder.err = tc.adderErr
			if tc.folders != nil {
				env.adder.catalog = fakeCatalog{profiles: testCatalog.profiles, folders: tc.folders}
			}
			rec := env.do(t, "POST", "/api/picks/1/request", tc.body)
			wantStatus(t, rec, tc.code)
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %s, want %q", rec.Body.String(), tc.wantBody)
			}
			failed := len(env.store.requests) == 1 && env.store.requests[0].Status == "failed"
			if failed != tc.wantFailed {
				t.Errorf("failed request recorded = %v, requests %+v", failed, env.store.requests)
			}
			if tc.code != 200 && !tc.wantFailed && len(env.store.requests) != 0 {
				t.Errorf("unexpected request rows: %+v", env.store.requests)
			}
		})
	}
	wantStatus(t, newEnv(t, nil).do(t, "POST", "/api/picks/5/request", `{"quality_profile_id":4}`), http.StatusNotFound)
}

func TestAppOptions(t *testing.T) {
	env := newEnv(t, nil)
	rec := env.do(t, "GET", "/api/apps/radarr/options", "")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{`{"id":4,"name":"HD-1080p"}`, `"path":"/media/movies"`, `"free_space":1099511627776`, `"default_root_folder":""`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	wantStatus(t, env.do(t, "GET", "/api/apps/sonarr/options", ""), http.StatusBadRequest)
	wantStatus(t, env.do(t, "GET", "/api/apps/lidarr/options", ""), http.StatusNotFound)
}

func TestLibrary(t *testing.T) {
	env := newEnv(t, nil)
	wantStatus(t, env.do(t, "GET", "/api/library", ""), http.StatusBadRequest)
	wantStatus(t, env.do(t, "GET", "/api/library?kind=series", ""), http.StatusBadRequest)

	rec := env.do(t, "GET", "/api/library?kind=movies", "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"profile":null`) || !strings.Contains(rec.Body.String(), `"poster_url":"https://img/p.jpg"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	env.store.profile = &profile.Profile{Kind: media.Movies, Top: []profile.Entry{{Title: "The Matrix", Weight: 3, Signal: "watched"}}}
	got := decode[struct {
		Kind    media.Kind       `json:"kind"`
		Titles  []media.Title    `json:"titles"`
		Profile *profile.Profile `json:"profile"`
	}](t, env.do(t, "GET", "/api/library?kind=movies", ""))
	if got.Kind != media.Movies || len(got.Titles) != 1 || got.Profile == nil || got.Profile.Top[0].Title != "The Matrix" {
		t.Fatalf("library = %+v", got)
	}
}

func TestConnectionsCheck(t *testing.T) {
	rec := newEnv(t, nil).do(t, "GET", "/api/connections/check", "")
	wantStatus(t, rec, http.StatusOK)
	if got := decode[[]CheckResult](t, rec); len(got) != 1 || got[0].Status != CheckOK {
		t.Fatalf("check = %+v", got)
	}
	rec = newEnv(t, func(o *Options) { o.Check = nil }).do(t, "GET", "/api/connections/check", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("nil check body = %s", rec.Body.String())
	}
}

func TestStatic(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":       {Data: []byte("<html>app</html>")},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":      {Data: []byte("<svg/>")},
		"assets/deep/x.js": {Data: []byte("x")},
	}
	env := newEnv(t, func(o *Options) { o.UI = ui })

	for _, target := range []string{"/", "/picks", "/runs/12"} {
		rec := env.do(t, "GET", target, "")
		wantStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), "app") || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: body %q cache %q", target, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}
	rec := env.do(t, "GET", "/assets/app-1.js", "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset cache = %q", rec.Header().Get("Cache-Control"))
	}
	wantStatus(t, env.do(t, "GET", "/favicon.svg", ""), http.StatusOK)
	wantStatus(t, env.do(t, "GET", "/assets/missing.js", ""), http.StatusNotFound)

	rec = env.do(t, "GET", "/api/nope", "")
	wantStatus(t, rec, http.StatusNotFound)
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("api 404 should be JSON: %s", rec.Body.String())
	}
	wantStatus(t, env.do(t, "POST", "/picks", ""), http.StatusMethodNotAllowed)

	notBuilt := newEnv(t, func(o *Options) { o.UI = fstest.MapFS{".gitkeep": {}} })
	rec = notBuilt.do(t, "GET", "/picks", "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "pnpm install") {
		t.Fatalf("not-built page = %s", rec.Body.String())
	}
}

func TestShutdownCancelsRuns(t *testing.T) {
	env := newEnv(t, nil)
	rec := env.do(t, "POST", "/api/runs", `{"kind":"movies"}`)
	wantStatus(t, rec, http.StatusAccepted)
	id := decode[store.Run](t, rec).ID

	done := make(chan struct{})
	go func() {
		env.srv.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not return")
	}
	run, _, err := env.store.GetRun(context.Background(), id)
	if err != nil || run.Status != store.RunFailed || run.FinishedAt == nil {
		t.Fatalf("run after shutdown = %+v, %v", run, err)
	}
	wantStatus(t, env.do(t, "POST", "/api/runs", `{"kind":"movies"}`), http.StatusServiceUnavailable)
}
