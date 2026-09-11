package settings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/store"
)

type memBackend struct {
	mu    sync.Mutex
	rows  map[string]store.Setting
	saves int
}

func newMem() *memBackend { return &memBackend{rows: map[string]store.Setting{}} }

func (m *memBackend) Settings(context.Context) ([]store.Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.Setting, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *memBackend) SaveSettings(_ context.Context, upsert []store.Setting, remove []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	for _, r := range upsert {
		m.rows[r.Key] = r
	}
	for _, k := range remove {
		delete(m.rows, k)
	}
	return nil
}

func testCipher(t *testing.T, b byte) *Cipher {
	t.Helper()
	c, err := NewCipher(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func values(t *testing.T, m map[string]any) map[string]json.RawMessage {
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

func newSvc(t *testing.T, env map[string]string, yamlBody string, b Backend, c *Cipher) *Service {
	t.Helper()
	path := ""
	if yamlBody != "" {
		path = filepath.Join(t.TempDir(), "proposarr.yaml")
		if err := os.WriteFile(path, []byte(yamlBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return NewService(Options{ConfigPath: path, Getenv: func(k string) string { return env[k] }, Backend: b, Cipher: c})
}

func TestPrecedenceAndSources(t *testing.T) {
	ctx := context.Background()
	c, b := testCipher(t, 1), newMem()
	if _, err := newSvc(t, nil, "", b, c).Update(ctx, values(t, map[string]any{
		"radarr.url":     "http://ui-radarr:7878",
		"radarr.api_key": "ui-radarr-key",
		"sonarr.url":     "http://ui-sonarr:8989/",
		"movies.picks":   5,
		"tmdb.region":    "de",
	})); err != nil {
		t.Fatal(err)
	}

	yamlBody := "radarr:\n  url: http://file-radarr:7878\n  api_key: \"\"\nmovies:\n  picks: 12\n"
	env := map[string]string{"PROPOSARR_MOVIES_PICKS": "20", "PROPOSARR_SONARR_URL": ""}
	res, err := newSvc(t, env, yamlBody, b, c).Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key       string
		src       Source
		got, want any
	}{
		{"radarr.url", SourceFile, res.Config.Radarr.URL, "http://file-radarr:7878"},
		{"radarr.api_key", SourceUI, res.Config.Radarr.APIKey, "ui-radarr-key"},  // empty in the file: not set there
		{"sonarr.url", SourceUI, res.Config.Sonarr.URL, "http://ui-sonarr:8989"}, // empty env: not set there
		{"movies.picks", SourceEnv, res.Config.Movies.Picks, 20},
		{"tmdb.region", SourceUI, res.Config.TMDB.Region, "DE"},
		{"series.picks", SourceDefault, res.Config.Series.Picks, 10},
	} {
		if res.Source(tc.key) != tc.src || tc.got != tc.want {
			t.Errorf("%s: source %s value %v, want %s %v", tc.key, res.Source(tc.key), tc.got, tc.src, tc.want)
		}
	}
	v := res.View(nil)
	if f := v.Fields["movies.picks"]; !f.Locked || f.Env != "PROPOSARR_MOVIES_PICKS" || f.Value != 20 {
		t.Errorf("movies.picks view = %+v", f)
	}
	if f := v.Fields["radarr.url"]; !f.Locked || f.Source != SourceFile {
		t.Errorf("radarr.url view = %+v", f)
	}
	if f := v.Fields["sonarr.url"]; f.Locked || f.Source != SourceUI {
		t.Errorf("sonarr.url view = %+v", f)
	}
}

func TestUpdateRejectsLockedAndUnknownKeysAllOrNothing(t *testing.T) {
	b := newMem()
	s := newSvc(t, map[string]string{"PROPOSARR_TMDB_API_KEY": "env-tmdb"}, "radarr:\n  url: http://file:7878\n", b, testCipher(t, 1))
	_, err := s.Update(context.Background(), values(t, map[string]any{
		"tmdb.api_key": "x",
		"radarr.url":   "http://other:7878",
		"bogus.key":    1,
		"sonarr.url":   "http://valid:8989",
	}))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	for key, want := range map[string]string{"tmdb.api_key": "PROPOSARR_TMDB_API_KEY", "radarr.url": "config file", "bogus.key": "unknown setting"} {
		if !strings.Contains(ve.Fields[key], want) {
			t.Errorf("%s: %q, want containing %q", key, ve.Fields[key], want)
		}
	}
	if _, ok := ve.Fields["sonarr.url"]; ok || len(ve.Fields) != 3 {
		t.Errorf("fields = %v", ve.Fields)
	}
	if b.saves != 0 || len(b.rows) != 0 {
		t.Fatalf("saved despite errors: %d saves, rows %v", b.saves, b.rows)
	}
}

func TestUpdateValidation(t *testing.T) {
	ctx := context.Background()
	bad := map[string]any{
		"radarr.url":                  "ftp://radarr",
		"plex.url":                    "radarr:7878",
		"movies.effort":               "extreme",
		"movies.picks":                0,
		"series.candidates":           "lots",
		"tmdb.region":                 "NLD",
		"claude.timeout":              "0s",
		"claude.max_budget_usd":       -1,
		"snapshot_ttl":                "soon",
		"plex.token":                  "has space",
		"history_days":                true,
		"radarr.minimum_availability": "tomorrow",
		"series.model":                "claude sonnet",
	}
	for key, v := range bad {
		t.Run(key, func(t *testing.T) {
			b := newMem()
			_, err := newSvc(t, nil, "", b, testCipher(t, 1)).Update(ctx, values(t, map[string]any{key: v, "sonarr.url": "http://ok:8989"}))
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Fields[key] == "" || len(ve.Fields) != 1 {
				t.Fatalf("err = %v", err)
			}
			if b.saves != 0 {
				t.Fatal("saved despite an invalid value")
			}
		})
	}

	res, err := newSvc(t, nil, "", newMem(), testCipher(t, 1)).Update(ctx, values(t, map[string]any{
		"radarr.url":                  " http://r:7878/ ",
		"tmdb.region":                 "nl",
		"movies.effort":               "HIGH",
		"snapshot_ttl":                "360m",
		"claude.max_budget_usd":       "0.50",
		"series.picks":                "7",
		"radarr.minimum_availability": "incinemas",
	}))
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	if c.Radarr.URL != "http://r:7878" || c.TMDB.Region != "NL" || c.Movies.Effort != "high" || c.SnapshotTTL.Duration != 6*time.Hour ||
		c.Claude.MaxBudgetUSD != 0.5 || c.Series.Picks != 7 || c.Radarr.MinimumAvailability != "inCinemas" {
		t.Fatalf("normalized config = %+v", c)
	}
	v := res.View(nil)
	if v.Fields["snapshot_ttl"].Value != "6h" || v.Fields["series.picks"].Value != 7 || v.Fields["claude.timeout"].Value != "10m" {
		t.Fatalf("view values = %v %v %v", v.Fields["snapshot_ttl"].Value, v.Fields["series.picks"].Value, v.Fields["claude.timeout"].Value)
	}
}

func TestClearAndClaudeCredentialConflict(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t, nil, "", newMem(), testCipher(t, 1))
	if _, err := s.Update(ctx, values(t, map[string]any{"claude.oauth_token": "sk-ant-oat-1", "movies.picks": 3})); err != nil {
		t.Fatal(err)
	}
	_, err := s.Update(ctx, values(t, map[string]any{"claude.api_key": "sk-ant-api-1"}))
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Fields["claude.api_key"], "only one") {
		t.Fatalf("err = %v", err)
	}
	res, err := s.Update(ctx, values(t, map[string]any{"claude.oauth_token": nil, "claude.api_key": "sk-ant-api-1"}))
	if err != nil || res.Config.Claude.OAuthToken != "" || res.Config.Claude.APIKey != "sk-ant-api-1" {
		t.Fatalf("switch credential: %+v, %v", res.Config.Claude, err)
	}
	res, err = s.Update(ctx, values(t, map[string]any{"movies.picks": ""}))
	if err != nil || res.Config.Movies.Picks != 10 || res.Source("movies.picks") != SourceDefault {
		t.Fatalf("clear: picks %d source %s, %v", res.Config.Movies.Picks, res.Source("movies.picks"), err)
	}
}

func TestSecretsEncryptedAndNeverInView(t *testing.T) {
	ctx := context.Background()
	b := newMem()
	s := newSvc(t, nil, "", b, testCipher(t, 1))
	if _, err := s.Update(ctx, values(t, map[string]any{"plex.token": "plex-secret-token", "plex.url": "http://plex:32400"})); err != nil {
		t.Fatal(err)
	}
	row := b.rows["plex.token"]
	if !row.Secret || !strings.HasPrefix(row.Value, "v1:") || strings.Contains(row.Value, "plex-secret-token") {
		t.Fatalf("stored secret row = %+v", row)
	}
	if b.rows["plex.url"].Secret || b.rows["plex.url"].Value != "http://plex:32400" {
		t.Fatalf("plain row = %+v", b.rows["plex.url"])
	}

	res, err := s.Resolve(ctx)
	if err != nil || res.Config.Plex.Token != "plex-secret-token" {
		t.Fatalf("resolved token %q, %v", res.Config.Plex.Token, err)
	}
	js, _ := json.Marshal(res.View(nil))
	if strings.Contains(string(js), "plex-secret-token") {
		t.Fatalf("view leaks the secret: %s", js)
	}
	if f := res.View(nil).Fields["plex.token"]; !f.Secret || !f.Set || f.Value != nil || f.Source != SourceUI {
		t.Fatalf("plex.token view = %+v", f)
	}
}

func TestWrongKeyLeavesSecretUnsetWithHint(t *testing.T) {
	ctx := context.Background()
	b := newMem()
	if _, err := newSvc(t, nil, "", b, testCipher(t, 1)).Update(ctx, values(t, map[string]any{"tmdb.api_key": "tmdb-secret", "tmdb.region": "NL"})); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*Cipher{"other key": testCipher(t, 2), "no key": nil} {
		res, err := newSvc(t, nil, "", b, c).Resolve(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Config.TMDB.APIKey != "" || res.Config.TMDB.Region != "NL" {
			t.Errorf("%s: tmdb = %+v", name, res.Config.TMDB)
		}
		if f := res.View(nil).Fields["tmdb.api_key"]; f.Set || f.Hint != HintUndecryptable || f.Source != SourceDefault {
			t.Errorf("%s: view = %+v", name, f)
		}
		if got := res.Undecryptable(); !slices.Equal(got, []string{"tmdb.api_key"}) {
			t.Errorf("%s: undecryptable = %v", name, got)
		}
	}
	res, err := newSvc(t, nil, "", b, testCipher(t, 2)).Update(ctx, values(t, map[string]any{"tmdb.api_key": "tmdb-new"}))
	if err != nil {
		t.Fatal(err)
	}
	if f := res.View(nil).Fields["tmdb.api_key"]; !f.Set || f.Hint != "" {
		t.Fatalf("re-entered secret view = %+v", f)
	}
}

func TestDraftIgnoresLockedKeysAndSavesNothing(t *testing.T) {
	ctx := context.Background()
	b := newMem()
	s := newSvc(t, map[string]string{"PROPOSARR_RADARR_URL": "http://env-radarr:7878"}, "", b, testCipher(t, 1))
	cfg, err := s.Draft(ctx, values(t, map[string]any{"radarr.url": "http://ignored:1", "radarr.api_key": "draft-key"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Radarr.URL != "http://env-radarr:7878" || cfg.Radarr.APIKey != "draft-key" || b.saves != 0 {
		t.Fatalf("draft = %+v, saves %d", cfg.Radarr, b.saves)
	}
	var ve *ValidationError
	if _, err := s.Draft(ctx, values(t, map[string]any{"movies.picks": 99})); !errors.As(err, &ve) {
		t.Fatalf("invalid draft err = %v", err)
	}
}

func TestLoadKey(t *testing.T) {
	noEnv := func(string) string { return "" }
	dir := filepath.Join(t.TempDir(), "data")
	if _, err := LoadKey(dir, noEnv, false); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
	k1, err := LoadKey(dir, noEnv, true)
	if err != nil || len(k1) != 32 {
		t.Fatalf("created key %d bytes, %v", len(k1), err)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, %v", info.Mode(), err)
	}
	if k2, err := LoadKey(dir, noEnv, false); err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("reloaded key differs: %v", err)
	}

	raw := bytes.Repeat([]byte{9}, 32)
	for name, v := range map[string]string{"base64": base64.StdEncoding.EncodeToString(raw), "hex": hex.EncodeToString(raw)} {
		k, err := LoadKey(dir, func(string) string { return v }, true)
		if err != nil || !bytes.Equal(k, raw) {
			t.Errorf("%s key = %x, %v", name, k, err)
		}
	}
	empty := t.TempDir()
	sum := sha256.Sum256([]byte("correct horse battery staple"))
	k, err := LoadKey(empty, func(string) string { return "correct horse battery staple" }, true)
	if err != nil || !bytes.Equal(k, sum[:]) {
		t.Fatalf("passphrase key = %x, %v", k, err)
	}
	if _, err := os.Stat(filepath.Join(empty, "secret.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("key file written although %s is set: %v", SecretKeyEnv, err)
	}
}

func TestCipherBindsKeyName(t *testing.T) {
	c := testCipher(t, 3)
	enc, err := c.Encrypt("plex.token", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := c.Decrypt("plex.token", enc); err != nil || p != "abc" {
		t.Fatalf("decrypt = %q, %v", p, err)
	}
	if _, err := c.Decrypt("tmdb.api_key", enc); err == nil {
		t.Fatal("ciphertext moved to another key decrypted")
	}
	if again, _ := c.Encrypt("plex.token", "abc"); again == enc {
		t.Fatal("nonce reused")
	}
}

func TestMissing(t *testing.T) {
	if got := Missing(config.Default()); !slices.Equal(got, []string{"tmdb.api_key", "radarr.url or sonarr.url"}) {
		t.Fatalf("zero config = %v", got)
	}
	cfg := config.Default()
	cfg.TMDB.APIKey, cfg.Radarr.URL = "k", "http://radarr:7878"
	if got := Missing(cfg); !slices.Equal(got, []string{"radarr.api_key"}) {
		t.Fatalf("missing key = %v", got)
	}
	cfg.Radarr.APIKey = "x"
	if got := Missing(cfg); got == nil || len(got) != 0 {
		t.Fatalf("complete = %#v", got)
	}
}

func TestViewCoversEveryKey(t *testing.T) {
	res, err := newSvc(t, nil, "", newMem(), testCipher(t, 1)).Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := res.View(map[string]string{"radarr.api_key": HintDiscovered})
	if len(v.Fields) != len(Keys()) || v.ReadOnly.Listen != ":8585" || v.ReadOnly.ClaudeBin != "claude" {
		t.Fatalf("view = %+v", v.ReadOnly)
	}
	if v.Fields["radarr.api_key"].Hint != HintDiscovered {
		t.Errorf("hint = %q", v.Fields["radarr.api_key"].Hint)
	}
	for key, f := range v.Fields {
		if f.Env == "" || f.Source != SourceDefault || f.Locked {
			t.Errorf("%s: %+v", key, f)
		}
	}
}
