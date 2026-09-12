package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestListenAndWebAuth(t *testing.T) {
	c, err := Load("", envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8585" || c.Web.AuthEnabled() {
		t.Fatalf("defaults: listen %q, auth %v", c.Listen, c.Web.AuthEnabled())
	}

	p := writeFile(t, "listen: \":9000\"\nweb:\n  username: admin\n  password: hunter2\n")
	c, err = Load(p, envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":9000" || c.Web.Username != "admin" || c.Web.Password != "hunter2" || !c.Web.AuthEnabled() {
		t.Fatalf("yaml: %+v", c)
	}
	c, err = Load(p, envMap(map[string]string{"PROPOSARR_LISTEN": "127.0.0.1:9001", "PROPOSARR_WEB_PASSWORD": "env-pass"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:9001" || c.Web.Password != "env-pass" {
		t.Fatalf("env override: listen %q, password %q", c.Listen, c.Web.Password)
	}

	for _, env := range []map[string]string{{"PROPOSARR_WEB_USERNAME": "admin"}, {"PROPOSARR_WEB_PASSWORD": "x"}} {
		if _, err := Load("", envMap(env)); err == nil || !strings.Contains(err.Error(), "PROPOSARR_WEB_USERNAME") {
			t.Errorf("%v: err = %v, want both-or-neither error", env, err)
		}
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "proposarr.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefault(t *testing.T) {
	c := Default()
	if c.DataDir != "data" || c.HistoryDays != 180 || c.SnapshotTTL.Duration != 10*time.Minute {
		t.Errorf("unexpected top-level defaults: %+v", c)
	}
	if c.Radarr.MinimumAvailability != "released" || c.TMDB.Region != "US" {
		t.Errorf("unexpected adapter defaults: radarr=%+v tmdb=%+v", c.Radarr, c.TMDB)
	}
	if c.Claude.Bin != "claude" || c.Claude.Timeout.Duration != 10*time.Minute {
		t.Errorf("unexpected claude defaults: %+v", c.Claude)
	}
	want := KindSettings{Model: "claude-sonnet-5", Effort: "medium", Picks: 10, Candidates: 150, FreePicks: 3, Seeds: 15, TopTitles: 500}
	if c.Movies != want || c.Series != want {
		t.Errorf("kind defaults: movies=%+v series=%+v", c.Movies, c.Series)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	c, err := Load("", envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	if c.DataDir != d.DataDir || c.Movies != d.Movies || c.Claude.Bin != d.Claude.Bin {
		t.Errorf("Load(\"\") = %+v, want defaults", c)
	}
}

func TestLoadMissingPath(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), envMap(nil))
	if err == nil {
		t.Fatal("want error for missing explicit path")
	}
}

func TestLoadYAML(t *testing.T) {
	p := writeFile(t, `
data_dir: /config
history_days: 90
snapshot_ttl: 90s
radarr:
  url: http://radarr:7878/
  api_key: rkey
  root_folder: /movies
sonarr:
  url: http://sonarr:8989
  api_key: skey
plex:
  url: http://plex:32400/
  token: ptoken
tmdb:
  api_key: tkey
  region: NL
claude:
  timeout: 6h
  max_budget_usd: 1.5
series:
  model: claude-opus-5
  picks: 5
`)
	c, err := Load(p, envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != "/config" || c.HistoryDays != 90 || c.SnapshotTTL.Duration != 90*time.Second {
		t.Errorf("top-level: %+v", c)
	}
	if c.Radarr.URL != "http://radarr:7878" || c.Radarr.APIKey != "rkey" || c.Radarr.RootFolder != "/movies" {
		t.Errorf("radarr: %+v", c.Radarr)
	}
	if c.Radarr.MinimumAvailability != "released" {
		t.Errorf("default minimum availability lost: %q", c.Radarr.MinimumAvailability)
	}
	if !c.Radarr.Configured() || !c.Sonarr.Configured() || !c.Plex.Configured() || c.Jellyfin.Configured() {
		t.Errorf("Configured flags wrong")
	}
	if c.Plex.URL != "http://plex:32400" {
		t.Errorf("plex url not trimmed: %q", c.Plex.URL)
	}
	if c.TMDB.Region != "NL" || c.Claude.Timeout.Duration != 6*time.Hour || c.Claude.MaxBudgetUSD != 1.5 {
		t.Errorf("tmdb/claude: %+v %+v", c.TMDB, c.Claude)
	}
	if c.Series.Model != "claude-opus-5" || c.Series.Picks != 5 {
		t.Errorf("series: %+v", c.Series)
	}
	if c.Series.Effort != "medium" || c.Series.Candidates != 150 {
		t.Errorf("series defaults lost on partial override: %+v", c.Series)
	}
}

func TestLoadYAMLBadDuration(t *testing.T) {
	p := writeFile(t, "snapshot_ttl: soon\n")
	if _, err := Load(p, envMap(nil)); err == nil {
		t.Fatal("want error for invalid duration")
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	p := writeFile(t, `
data_dir: /yaml
history_days: 90
radarr:
  url: http://yaml-radarr
movies:
  model: yaml-model
  picks: 4
`)
	env := map[string]string{
		"PROPOSARR_DATA_DIR":             "/env",
		"PROPOSARR_HISTORY_DAYS":         "30",
		"PROPOSARR_SNAPSHOT_TTL":         "1h",
		"PROPOSARR_RADARR_URL":           "http://env-radarr/",
		"PROPOSARR_RADARR_API_KEY":       "envkey",
		"PROPOSARR_RADARR_PROFILE_ORDER": "Remux + WEB 2160p,8",
		"PROPOSARR_MOVIES_MODEL":         "env-model",
		"PROPOSARR_MOVIES_PICKS":         "12",
		"PROPOSARR_SERIES_EFFORT":        "high",
		"PROPOSARR_TMDB_REGION":          "DE",
		"PROPOSARR_CLAUDE_TIMEOUT":       "2m",
		"PROPOSARR_JELLYFIN_URL":         "http://jf/",
	}
	c, err := Load(p, envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != "/env" || c.HistoryDays != 30 || c.SnapshotTTL.Duration != time.Hour {
		t.Errorf("top-level: %+v", c)
	}
	if c.Radarr.URL != "http://env-radarr" || c.Radarr.APIKey != "envkey" || c.Radarr.ProfileOrder != "Remux + WEB 2160p,8" {
		t.Errorf("radarr: %+v", c.Radarr)
	}
	if c.Movies.Model != "env-model" || c.Movies.Picks != 12 || c.Series.Effort != "high" {
		t.Errorf("kinds: movies=%+v series=%+v", c.Movies, c.Series)
	}
	if c.TMDB.Region != "DE" || c.Claude.Timeout.Duration != 2*time.Minute || c.Jellyfin.URL != "http://jf" {
		t.Errorf("misc: %+v %+v %+v", c.TMDB, c.Claude, c.Jellyfin)
	}
}

func TestInvalidEnv(t *testing.T) {
	for _, name := range []string{
		"PROPOSARR_HISTORY_DAYS",
		"PROPOSARR_MOVIES_PICKS",
		"PROPOSARR_SERIES_CANDIDATES",
		"PROPOSARR_SNAPSHOT_TTL",
		"PROPOSARR_CLAUDE_TIMEOUT",
		"PROPOSARR_CLAUDE_MAX_BUDGET_USD",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load("", envMap(map[string]string{name: "not-a-number"}))
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not mention %s", err, name)
			}
		})
	}
}

func TestClaudeAuth(t *testing.T) {
	tests := []struct {
		name      string
		c         Claude
		wantName  string
		wantValue string
		wantErr   bool
	}{
		{"token only", Claude{OAuthToken: "tok"}, EnvOAuthToken, "tok", false},
		{"key only", Claude{APIKey: "key"}, EnvAPIKey, "key", false},
		{"none", Claude{}, "", "", false},
		{"both", Claude{OAuthToken: "tok", APIKey: "key"}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, value, err := tt.c.Auth()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if name != tt.wantName || value != tt.wantValue {
				t.Errorf("Auth() = %q, %q; want %q, %q", name, value, tt.wantName, tt.wantValue)
			}
		})
	}
}

func TestAuthFromEnv(t *testing.T) {
	c, err := Load("", envMap(map[string]string{EnvOAuthToken: "tok"}))
	if err != nil {
		t.Fatal(err)
	}
	if name, value, err := c.Claude.Auth(); err != nil || name != EnvOAuthToken || value != "tok" {
		t.Errorf("Auth() = %q, %q, %v", name, value, err)
	}

	c, err = Load("", envMap(map[string]string{EnvOAuthToken: "tok", EnvAPIKey: "key"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Claude.Auth(); err == nil {
		t.Error("want error when both credentials come from env")
	}
}

func TestKind(t *testing.T) {
	c := Default()
	c.Movies.Model = "m"
	c.Series.Model = "s"
	if got := c.Kind(media.Movies).Model; got != "m" {
		t.Errorf("Kind(Movies).Model = %q", got)
	}
	if got := c.Kind(media.Series).Model; got != "s" {
		t.Errorf("Kind(Series).Model = %q", got)
	}
}
