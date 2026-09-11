package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/settings"
	"github.com/leandervdo/proposarr/internal/store"
	"github.com/leandervdo/proposarr/internal/web"
)

// fakeArr is a Radarr that answers status and the movie list for one API key,
// and optionally exposes that key through /initialize.json.
type fakeArr struct {
	srv       *httptest.Server
	movieHits atomic.Int32
}

func newFakeArr(t *testing.T, key, version string, exposeKey bool) *fakeArr {
	t.Helper()
	f := &fakeArr{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/initialize.json" {
			if !exposeKey {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"apiRoot":"/api/v3","apiKey":%q}`, key)
			return
		}
		if r.Header.Get("X-Api-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v3/system/status":
			fmt.Fprintf(w, `{"appName":"Radarr","version":%q}`, version)
		case "/api/v3/movie":
			f.movieHits.Add(1)
			fmt.Fprint(w, `[{"id":1,"title":"Arrival","year":2016,"tmdbId":329865}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newTestRuntime(t *testing.T, env map[string]string) (*runtime, *store.SQLite) {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	vars := map[string]string{"PROPOSARR_DATA_DIR": dataDir}
	for k, v := range env {
		vars[k] = v
	}
	getenv := envMap(vars)
	st, err := store.Open(filepath.Join(dataDir, "proposarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key, err := settings.LoadKey(dataDir, getenv, true)
	if err != nil {
		t.Fatal(err)
	}
	ciph, err := settings.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(settings.Options{Getenv: getenv, Backend: st, Cipher: ciph})
	rt := newRuntime(svc, slog.New(slog.DiscardHandler), "")
	if err := rt.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return rt, st
}

func rawValues(t *testing.T, m map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for k, v := range m {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = b
	}
	return out
}

func serveHTTP(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func TestZeroConfigServeRequiresSetup(t *testing.T) {
	rt, st := newTestRuntime(t, nil)
	ws := web.New(serverOptions(rt, st, slog.New(slog.DiscardHandler)))
	t.Cleanup(ws.Shutdown)
	h := ws.Handler()

	rec := serveHTTP(h, "GET", "/api/status", "")
	var status struct {
		SetupRequired bool     `json:"setup_required"`
		Missing       []string `json:"missing"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if !status.SetupRequired || !slices.Contains(status.Missing, "tmdb.api_key") {
		t.Fatalf("status = %+v", status)
	}
	if rec := serveHTTP(h, "GET", "/", ""); rec.Code != 200 {
		t.Fatalf("UI: %d", rec.Code)
	}
	rec = serveHTTP(h, "POST", "/api/runs", `{"kind":"movies"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "tmdb.api_key") {
		t.Fatalf("run without settings: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveHTTP(h, "GET", "/api/settings", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"radarr.url":{"value":"","secret":false,"set":false,"source":"default"`) {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSavingSettingsReloadsAdapters(t *testing.T) {
	a := newFakeArr(t, "key-a-1234", "6.3.0", false)
	b := newFakeArr(t, "key-b-5678", "6.4.0", true)
	rt, st := newTestRuntime(t, map[string]string{"PROPOSARR_SNAPSHOT_TTL": "0s"})
	ws := web.New(serverOptions(rt, st, slog.New(slog.DiscardHandler)))
	t.Cleanup(ws.Shutdown)
	h := ws.Handler()
	ctx := context.Background()

	rec := serveHTTP(h, "PUT", "/api/settings", `{"values":{"radarr.url":"`+a.srv.URL+`","radarr.api_key":"key-a-1234","tmdb.api_key":"tmdb-secret-key"}}`)
	if rec.Code != 200 {
		t.Fatalf("save A: %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"key-a-1234", "tmdb-secret-key"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("PUT response leaks %q", secret)
		}
	}
	if rec := serveHTTP(h, "GET", "/api/library?kind=movies", ""); rec.Code != 200 || a.movieHits.Load() != 1 {
		t.Fatalf("library via A: %d %s (hits %d)", rec.Code, rec.Body.String(), a.movieHits.Load())
	}
	before := rt.state()

	// B's key is only available through initialize.json.
	rec = serveHTTP(h, "PUT", "/api/settings", `{"values":{"radarr.url":"`+b.srv.URL+`","radarr.api_key":null}}`)
	if rec.Code != 200 {
		t.Fatalf("save B: %d %s", rec.Code, rec.Body.String())
	}
	var view settings.View
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if f := view.Fields["radarr.api_key"]; f.Set || f.Hint != settings.HintDiscovered {
		t.Fatalf("radarr.api_key view = %+v", f)
	}
	if rec := serveHTTP(h, "GET", "/api/library?kind=movies", ""); rec.Code != 200 || b.movieHits.Load() != 1 || a.movieHits.Load() != 1 {
		t.Fatalf("library after reload: %d (A %d, B %d)", rec.Code, a.movieHits.Load(), b.movieHits.Load())
	}

	// Work started before the save keeps the adapters it began with.
	if _, err := before.deps.library(true).Titles(ctx, media.Movies); err != nil || a.movieHits.Load() != 2 {
		t.Fatalf("earlier state: hits A %d, err %v", a.movieHits.Load(), err)
	}

	// The CLI sees what the UI saved.
	rows, err := st.Settings(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("saved rows = %+v, %v", rows, err)
	}
}

func TestSettingsTestUsesDraftValues(t *testing.T) {
	ctx := context.Background()
	arrSrv := newFakeArr(t, "secret-radarr-key", "6.3.0.10514", true)
	rt, st := newTestRuntime(t, nil)

	res, err := rt.Test(ctx, "radarr", rawValues(t, map[string]any{"radarr.url": arrSrv.srv.URL}))
	if err != nil || res.Status != web.CheckOK || res.Detail != "6.3.0.10514" || !res.DiscoveredAPIKey {
		t.Fatalf("radarr test = %+v, %v", res, err)
	}
	if rows, _ := st.Settings(ctx); len(rows) != 0 || rt.state().cfg.Radarr.URL != "" {
		t.Fatalf("a test must not save: rows %v, url %q", rows, rt.state().cfg.Radarr.URL)
	}

	res, err = rt.Test(ctx, "radarr", rawValues(t, map[string]any{"radarr.url": arrSrv.srv.URL, "radarr.api_key": "wrong-key-123"}))
	if err != nil || res.Status != web.CheckFail || strings.Contains(res.Detail, "wrong-key-123") {
		t.Fatalf("wrong key test = %+v, %v", res, err)
	}

	var ve *settings.ValidationError
	if _, err := rt.Test(ctx, "radarr", rawValues(t, map[string]any{"radarr.url": "ftp://radarr"})); !errors.As(err, &ve) {
		t.Fatalf("invalid draft err = %v", err)
	}

	fake := &agent.Fake{Result: agent.Result{Text: "ok"}}
	rt.agent = fake
	res, err = rt.Test(ctx, "claude", rawValues(t, map[string]any{"claude.oauth_token": "sk-ant-oat-draft"}))
	if err != nil || res.Status != web.CheckOK || strings.Contains(res.Detail, "sk-ant-oat-draft") {
		t.Fatalf("claude test = %+v, %v", res, err)
	}
	calls := fake.Calls()
	if len(calls) != 1 || !slices.Contains(calls[0].Env, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-draft") {
		t.Fatalf("claude probe env did not carry the draft token")
	}

	ws := web.New(serverOptions(rt, st, slog.New(slog.DiscardHandler)))
	t.Cleanup(ws.Shutdown)
	rec := serveHTTP(ws.Handler(), "POST", "/api/settings/test", `{"service":"radarr","values":{"radarr.url":"`+arrSrv.srv.URL+`"}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"discovered_api_key":true`) || strings.Contains(rec.Body.String(), "secret-radarr-key") {
		t.Fatalf("HTTP test: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCLIAppliesSettingsSavedInTheUI(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)
	dataDir := filepath.Join(dir, "data")
	st, err := store.Open(filepath.Join(dataDir, "proposarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() // the server keeps the database open while the CLI reads it
	key, err := settings.LoadKey(dataDir, envMap(nil), true)
	if err != nil {
		t.Fatal(err)
	}
	ciph, _ := settings.NewCipher(key)
	svc := settings.NewService(settings.Options{Getenv: envMap(nil), Backend: st, Cipher: ciph})
	if _, err := svc.Update(ctx, rawValues(t, map[string]any{
		"radarr.url":   "http://ui-radarr:7878",
		"tmdb.api_key": "ui-tmdb-key",
		"movies.picks": 7,
	})); err != nil {
		t.Fatal(err)
	}

	c, _, stderr := newTestCLI(false)
	c.getenv = envMap(map[string]string{"PROPOSARR_MOVIES_PICKS": "9"})
	cfg, err := c.loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Radarr.URL != "http://ui-radarr:7878" || cfg.TMDB.APIKey != "ui-tmdb-key" || cfg.Movies.Picks != 9 {
		t.Fatalf("cfg = radarr %q tmdb %q picks %d (stderr %q)", cfg.Radarr.URL, cfg.TMDB.APIKey, cfg.Movies.Picks, stderr.String())
	}

	t.Chdir(t.TempDir())
	c, _, _ = newTestCLI(false)
	if cfg, err := c.loadConfig(""); err != nil || cfg.Radarr.URL != "" {
		t.Fatalf("without a database: %q, %v", cfg.Radarr.URL, err)
	}
}
