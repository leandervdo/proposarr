package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/settings"
)

type fakeSettings struct {
	mu        sync.Mutex
	view      settings.View
	updateErr error
	updated   map[string]json.RawMessage
	result    TestResult
	testErr   error
	tested    string
}

func (f *fakeSettings) View(context.Context) (settings.View, error) { return f.view, nil }

func (f *fakeSettings) Update(_ context.Context, values map[string]json.RawMessage) (settings.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = values
	return f.view, f.updateErr
}

func (f *fakeSettings) Test(_ context.Context, service string, _ map[string]json.RawMessage) (TestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = service
	return f.result, f.testErr
}

func TestSettingsRoutes(t *testing.T) {
	fs := &fakeSettings{view: settings.View{Fields: map[string]settings.FieldView{
		"radarr.url": {Value: "http://radarr:7878", Set: true, Source: settings.SourceUI, Env: "PROPOSARR_RADARR_URL"},
	}}}
	env := newEnv(t, func(o *Options) { o.Settings = fs })

	rec := env.do(t, "GET", "/api/settings", "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"radarr.url":{"value":"http://radarr:7878"`) {
		t.Fatalf("GET body = %s", rec.Body.String())
	}

	wantStatus(t, env.do(t, "PUT", "/api/settings", `{"values":{"radarr.url":"http://new:7878","movies.picks":12,"plex.token":null}}`), http.StatusOK)
	if len(fs.updated) != 3 || string(fs.updated["plex.token"]) != "null" {
		t.Fatalf("update got %v", fs.updated)
	}
	wantStatus(t, env.do(t, "PUT", "/api/settings", `{}`), http.StatusBadRequest)
	wantStatus(t, env.do(t, "PUT", "/api/settings", `nope`), http.StatusBadRequest)

	fs.updateErr = &settings.ValidationError{Fields: map[string]string{"movies.picks": "must be between 1 and 50"}}
	rec = env.do(t, "PUT", "/api/settings", `{"values":{"movies.picks":99}}`)
	wantStatus(t, rec, http.StatusBadRequest)
	got := decode[struct {
		Error       string            `json:"error"`
		FieldErrors map[string]string `json:"field_errors"`
	}](t, rec)
	if got.FieldErrors["movies.picks"] != "must be between 1 and 50" || got.Error == "" {
		t.Fatalf("validation body = %s", rec.Body.String())
	}
	fs.updateErr = errors.New("disk full")
	wantStatus(t, env.do(t, "PUT", "/api/settings", `{"values":{}}`), http.StatusInternalServerError)

	fs.result = TestResult{Status: "ok", Detail: "6.3.0", DiscoveredAPIKey: true}
	rec = env.do(t, "POST", "/api/settings/test", `{"service":"radarr","values":{"radarr.url":"http://r:7878"}}`)
	wantStatus(t, rec, http.StatusOK)
	if fs.tested != "radarr" || !strings.Contains(rec.Body.String(), `"discovered_api_key":true`) {
		t.Fatalf("test body = %s (service %q)", rec.Body.String(), fs.tested)
	}
	wantStatus(t, env.do(t, "POST", "/api/settings/test", `{"service":"lidarr"}`), http.StatusBadRequest)
	fs.testErr = &settings.ValidationError{Fields: map[string]string{"radarr.url": "must be an http:// or https:// URL"}}
	rec = env.do(t, "POST", "/api/settings/test", `{"service":"radarr","values":{"radarr.url":"ftp://x"}}`)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), `"field_errors"`) {
		t.Fatalf("test validation body = %s", rec.Body.String())
	}

	noSettings := newEnv(t, nil)
	wantStatus(t, noSettings.do(t, "GET", "/api/settings", ""), http.StatusServiceUnavailable)
}

func TestStatusSetupRequired(t *testing.T) {
	type statusBody struct {
		SetupRequired bool     `json:"setup_required"`
		Missing       []string `json:"missing"`
	}
	ready := decode[statusBody](t, newEnv(t, nil).do(t, "GET", "/api/status", ""))
	if ready.SetupRequired || ready.Missing == nil || len(ready.Missing) != 0 {
		t.Fatalf("configured status = %+v", ready)
	}

	env := newEnv(t, func(o *Options) { o.Config = config.Default })
	rec := env.do(t, "GET", "/api/status", "")
	wantStatus(t, rec, http.StatusOK)
	got := decode[statusBody](t, rec)
	if !got.SetupRequired || !slices.Contains(got.Missing, "tmdb.api_key") || !slices.Contains(got.Missing, "radarr.url or sonarr.url") {
		t.Fatalf("zero-config status = %+v", got)
	}
}
