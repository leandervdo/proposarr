// Package settings layers the editable settings: built-in defaults, values
// saved from the web UI, the config file and environment variables, each
// overriding the one before.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/store"
)

type Source string

const (
	SourceDefault Source = "default"
	SourceUI      Source = "ui"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
)

const (
	HintUndecryptable = "could not be decrypted; enter it again"
	HintDiscovered    = "read from initialize.json"
)

type fieldType int

const (
	typeString fieldType = iota
	typeURL
	typeSecret
	typeEnum
	typeInt
	typeFloat
	typeDuration
	typeRegion
	typeModel
)

// field is one editable key and where it lives in config.Config.
type field struct {
	key, env string
	typ      fieldType
	enum     []string
	min, max int
	positive bool // durations: zero is not allowed
	str      func(*config.Config) *string
	num      func(*config.Config) *int
	flt      func(*config.Config) *float64
	dur      func(*config.Config) *time.Duration
}

var fields = buildFields()

var byKey = func() map[string]*field {
	m := make(map[string]*field, len(fields))
	for i := range fields {
		m[fields[i].key] = &fields[i]
	}
	return m
}()

// Keys lists every editable setting in display order.
func Keys() []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.key
	}
	return out
}

func buildFields() []field {
	arr := func(name, env string, get func(*config.Config) *config.Arr) []field {
		return []field{
			{key: name + ".url", env: env + "URL", typ: typeURL, str: func(c *config.Config) *string { return &get(c).URL }},
			{key: name + ".api_key", env: env + "API_KEY", typ: typeSecret, str: func(c *config.Config) *string { return &get(c).APIKey }},
			{key: name + ".root_folder", env: env + "ROOT_FOLDER", typ: typeString, str: func(c *config.Config) *string { return &get(c).RootFolder }},
		}
	}
	kind := func(name, env string, get func(*config.Config) *config.KindSettings) []field {
		return []field{
			{key: name + ".model", env: env + "MODEL", typ: typeModel, str: func(c *config.Config) *string { return &get(c).Model }},
			{key: name + ".effort", env: env + "EFFORT", typ: typeEnum, enum: []string{"low", "medium", "high", "xhigh", "max"}, str: func(c *config.Config) *string { return &get(c).Effort }},
			{key: name + ".picks", env: env + "PICKS", typ: typeInt, min: 1, max: 50, num: func(c *config.Config) *int { return &get(c).Picks }},
			{key: name + ".candidates", env: env + "CANDIDATES", typ: typeInt, min: 10, max: 500, num: func(c *config.Config) *int { return &get(c).Candidates }},
			{key: name + ".free_picks", env: env + "FREE_PICKS", typ: typeInt, min: 0, max: 10, num: func(c *config.Config) *int { return &get(c).FreePicks }},
			{key: name + ".seeds", env: env + "SEEDS", typ: typeInt, min: 1, max: 50, num: func(c *config.Config) *int { return &get(c).Seeds }},
			{key: name + ".top_titles", env: env + "TOP_TITLES", typ: typeInt, min: 5, max: 200, num: func(c *config.Config) *int { return &get(c).TopTitles }},
		}
	}

	var fs []field
	fs = append(fs, arr("radarr", "PROPOSARR_RADARR_", func(c *config.Config) *config.Arr { return &c.Radarr })...)
	fs = append(fs, field{key: "radarr.minimum_availability", env: "PROPOSARR_RADARR_MINIMUM_AVAILABILITY", typ: typeEnum,
		enum: []string{"announced", "inCinemas", "released"}, str: func(c *config.Config) *string { return &c.Radarr.MinimumAvailability }})
	fs = append(fs, arr("sonarr", "PROPOSARR_SONARR_", func(c *config.Config) *config.Arr { return &c.Sonarr })...)
	fs = append(fs,
		field{key: "plex.url", env: "PROPOSARR_PLEX_URL", typ: typeURL, str: func(c *config.Config) *string { return &c.Plex.URL }},
		field{key: "plex.token", env: "PROPOSARR_PLEX_TOKEN", typ: typeSecret, str: func(c *config.Config) *string { return &c.Plex.Token }},
		field{key: "jellyfin.url", env: "PROPOSARR_JELLYFIN_URL", typ: typeURL, str: func(c *config.Config) *string { return &c.Jellyfin.URL }},
		field{key: "jellyfin.api_key", env: "PROPOSARR_JELLYFIN_API_KEY", typ: typeSecret, str: func(c *config.Config) *string { return &c.Jellyfin.APIKey }},
		field{key: "jellyfin.user_id", env: "PROPOSARR_JELLYFIN_USER_ID", typ: typeString, str: func(c *config.Config) *string { return &c.Jellyfin.UserID }},
		field{key: "tmdb.api_key", env: "PROPOSARR_TMDB_API_KEY", typ: typeSecret, str: func(c *config.Config) *string { return &c.TMDB.APIKey }},
		field{key: "tmdb.region", env: "PROPOSARR_TMDB_REGION", typ: typeRegion, str: func(c *config.Config) *string { return &c.TMDB.Region }},
		field{key: "claude.oauth_token", env: config.EnvOAuthToken, typ: typeSecret, str: func(c *config.Config) *string { return &c.Claude.OAuthToken }},
		field{key: "claude.api_key", env: config.EnvAPIKey, typ: typeSecret, str: func(c *config.Config) *string { return &c.Claude.APIKey }},
		field{key: "claude.timeout", env: "PROPOSARR_CLAUDE_TIMEOUT", typ: typeDuration, positive: true, dur: func(c *config.Config) *time.Duration { return &c.Claude.Timeout.Duration }},
		field{key: "claude.max_budget_usd", env: "PROPOSARR_CLAUDE_MAX_BUDGET_USD", typ: typeFloat, flt: func(c *config.Config) *float64 { return &c.Claude.MaxBudgetUSD }},
		field{key: "history_days", env: "PROPOSARR_HISTORY_DAYS", typ: typeInt, min: 1, max: 3650, num: func(c *config.Config) *int { return &c.HistoryDays }},
		field{key: "snapshot_ttl", env: "PROPOSARR_SNAPSHOT_TTL", typ: typeDuration, dur: func(c *config.Config) *time.Duration { return &c.SnapshotTTL.Duration }},
	)
	fs = append(fs, kind("movies", "PROPOSARR_MOVIES_", func(c *config.Config) *config.KindSettings { return &c.Movies })...)
	fs = append(fs, kind("series", "PROPOSARR_SERIES_", func(c *config.Config) *config.KindSettings { return &c.Series })...)
	return fs
}

// normalize validates a raw value and returns its canonical form.
func (f *field) normalize(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch f.typ {
	case typeURL:
		v = strings.TrimRight(v, "/")
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("must be an http:// or https:// URL")
		}
		return v, nil
	case typeSecret:
		if strings.ContainsFunc(v, unicode.IsSpace) {
			return "", errors.New("must not contain spaces")
		}
		if len(v) > 4096 {
			return "", errors.New("is too long")
		}
		return v, nil
	case typeModel:
		if strings.ContainsFunc(v, unicode.IsSpace) || len(v) > 100 {
			return "", errors.New("must be a model name without spaces")
		}
		return v, nil
	case typeEnum:
		for _, e := range f.enum {
			if strings.EqualFold(v, e) {
				return e, nil
			}
		}
		return "", fmt.Errorf("must be one of %s", strings.Join(f.enum, ", "))
	case typeInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return "", errors.New("must be a whole number")
		}
		if n < f.min || n > f.max {
			return "", fmt.Errorf("must be between %d and %d", f.min, f.max)
		}
		return strconv.Itoa(n), nil
	case typeFloat:
		x, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return "", errors.New("must be a number")
		}
		if x < 0 {
			return "", errors.New("must be 0 or more")
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case typeDuration:
		d, err := time.ParseDuration(v)
		if err != nil {
			return "", errors.New(`must be a duration such as "10m" or "6h"`)
		}
		if d < 0 || (f.positive && d == 0) {
			return "", errors.New("must be more than 0")
		}
		return formatDuration(d), nil
	case typeRegion:
		if len(v) != 2 || !isLetters(v) {
			return "", errors.New("must be a two-letter country code")
		}
		return strings.ToUpper(v), nil
	}
	if len(v) > 1024 {
		return "", errors.New("is too long")
	}
	return v, nil
}

func (f *field) apply(c *config.Config, v string) error {
	norm, err := f.normalize(v)
	if err != nil {
		return err
	}
	switch f.typ {
	case typeInt:
		*f.num(c), _ = strconv.Atoi(norm)
	case typeFloat:
		*f.flt(c), _ = strconv.ParseFloat(norm, 64)
	case typeDuration:
		*f.dur(c), _ = time.ParseDuration(norm)
	default:
		*f.str(c) = norm
	}
	return nil
}

// value is the effective value for the API and whether it counts as set.
func (f *field) value(c config.Config) (any, bool) {
	switch f.typ {
	case typeInt:
		return *f.num(&c), true
	case typeFloat:
		return *f.flt(&c), true
	case typeDuration:
		return formatDuration(*f.dur(&c)), true
	}
	s := *f.str(&c)
	return s, s != ""
}

func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func isLetters(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// Backend stores the values saved from the UI. Secret values are ciphertext.
type Backend interface {
	Settings(ctx context.Context) ([]store.Setting, error)
	SaveSettings(ctx context.Context, upsert []store.Setting, remove []string) error
}

// StaticBackend serves saved rows read elsewhere and refuses changes.
type StaticBackend []store.Setting

func (b StaticBackend) Settings(context.Context) ([]store.Setting, error) { return b, nil }

func (StaticBackend) SaveSettings(context.Context, []store.Setting, []string) error {
	return errors.New("settings are read-only here")
}

type Options struct {
	ConfigPath string              // "" when there is no config file
	Getenv     func(string) string // nil means os.Getenv
	Backend    Backend             // nil: no UI layer, nothing can be saved
	Cipher     *Cipher             // nil: secrets can be neither saved nor read
	Logger     *slog.Logger
}

type Service struct {
	o      Options
	log    *slog.Logger
	getenv func(string) string

	writeMu sync.Mutex // serializes Update

	warnMu sync.Mutex
	warned map[string]bool
}

func NewService(o Options) *Service {
	s := &Service{o: o, log: o.Logger, getenv: o.Getenv, warned: map[string]bool{}}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.getenv == nil {
		s.getenv = os.Getenv
	}
	return s
}

// Resolved is the effective configuration and where each editable key came from.
type Resolved struct {
	Config     config.Config
	ConfigFile string
	state      map[string]fieldState
}

type fieldState struct {
	source Source
	hint   string
}

func (r Resolved) Source(key string) Source {
	if st, ok := r.state[key]; ok {
		return st.source
	}
	return SourceDefault
}

// Undecryptable lists saved secrets that could not be read with the current key.
func (r Resolved) Undecryptable() []string {
	var out []string
	for _, f := range fields {
		if r.state[f.key].hint == HintUndecryptable {
			out = append(out, f.key)
		}
	}
	return out
}

// ValidationError carries one message per rejected key.
type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e.Fields[k]
	}
	return "invalid settings: " + strings.Join(parts, "; ")
}

func (s *Service) Resolve(ctx context.Context) (Resolved, error) {
	ui, bad, err := s.loadUI(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return s.resolve(ui, bad)
}

// Update validates values and saves them all, or nothing. null or "" removes
// the UI value for a key.
func (s *Service) Update(ctx context.Context, values map[string]json.RawMessage) (Resolved, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.o.Backend == nil {
		return Resolved{}, errors.New("settings cannot be saved without a database")
	}
	ui, bad, err := s.loadUI(ctx)
	if err != nil {
		return Resolved{}, err
	}
	cand, changes, err := s.apply(ui, bad, values, false)
	if err != nil {
		return Resolved{}, err
	}
	upsert, remove, err := s.rows(changes)
	if err != nil {
		return Resolved{}, err
	}
	if err := s.o.Backend.SaveSettings(ctx, upsert, remove); err != nil {
		return Resolved{}, fmt.Errorf("save settings: %w", err)
	}
	return cand, nil
}

// Draft merges unsaved values over the current settings without saving them.
// Keys locked by the file or environment are ignored, since those win anyway.
func (s *Service) Draft(ctx context.Context, values map[string]json.RawMessage) (config.Config, error) {
	ui, bad, err := s.loadUI(ctx)
	if err != nil {
		return config.Config{}, err
	}
	cand, _, err := s.apply(ui, bad, values, true)
	if err != nil {
		return config.Config{}, err
	}
	return cand.Config, nil
}

func (s *Service) apply(ui map[string]string, bad map[string]bool, values map[string]json.RawMessage, draft bool) (Resolved, map[string]string, error) {
	cur, err := s.resolve(ui, bad)
	if err != nil {
		return Resolved{}, nil, err
	}
	changes := map[string]string{}
	fe := map[string]string{}
	for key, raw := range values {
		f, ok := byKey[key]
		if !ok {
			fe[key] = "unknown setting"
			continue
		}
		switch cur.Source(key) {
		case SourceEnv:
			if !draft {
				fe[key] = "set by environment variable " + f.env
			}
			continue
		case SourceFile:
			if !draft {
				fe[key] = "set in the config file " + cur.ConfigFile
			}
			continue
		}
		v, err := rawString(raw)
		if err != nil {
			fe[key] = err.Error()
			continue
		}
		if strings.TrimSpace(v) == "" {
			changes[key] = ""
			continue
		}
		norm, err := f.normalize(v)
		if err != nil {
			fe[key] = err.Error()
			continue
		}
		if f.typ == typeSecret && s.o.Cipher == nil && !draft {
			fe[key] = "cannot be saved: no secret key is available"
			continue
		}
		changes[key] = norm
	}
	if len(fe) > 0 {
		return Resolved{}, nil, &ValidationError{Fields: fe}
	}

	next := make(map[string]string, len(ui)+len(changes))
	nextBad := make(map[string]bool, len(bad))
	for k, v := range ui {
		next[k] = v
	}
	for k := range bad {
		nextBad[k] = true
	}
	for k, v := range changes {
		delete(nextBad, k)
		if v == "" {
			delete(next, k)
		} else {
			next[k] = v
		}
	}
	cand, err := s.resolve(next, nextBad)
	if err != nil {
		return Resolved{}, nil, err
	}
	if _, _, err := cand.Config.Claude.Auth(); err != nil {
		for _, k := range []string{"claude.oauth_token", "claude.api_key"} {
			if _, ok := values[k]; ok {
				fe[k] = "set only one of claude.oauth_token and claude.api_key"
			}
		}
		if len(fe) > 0 {
			return Resolved{}, nil, &ValidationError{Fields: fe}
		}
	}
	return cand, changes, nil
}

func (s *Service) rows(changes map[string]string) ([]store.Setting, []string, error) {
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	now := time.Now().UTC()
	var upsert []store.Setting
	var remove []string
	for _, k := range keys {
		v := changes[k]
		if v == "" {
			remove = append(remove, k)
			continue
		}
		row := store.Setting{Key: k, Value: v, UpdatedAt: now}
		if byKey[k].typ == typeSecret {
			enc, err := s.o.Cipher.Encrypt(k, v)
			if err != nil {
				return nil, nil, fmt.Errorf("encrypt %s: %w", k, err)
			}
			row.Value, row.Secret = enc, true
		}
		upsert = append(upsert, row)
	}
	return upsert, remove, nil
}

func (s *Service) loadUI(ctx context.Context) (map[string]string, map[string]bool, error) {
	ui, bad := map[string]string{}, map[string]bool{}
	if s.o.Backend == nil {
		return ui, bad, nil
	}
	rows, err := s.o.Backend.Settings(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load saved settings: %w", err)
	}
	for _, r := range rows {
		if _, ok := byKey[r.Key]; !ok {
			continue
		}
		if !r.Secret {
			ui[r.Key] = r.Value
			continue
		}
		if s.o.Cipher == nil {
			bad[r.Key] = true
			s.warnOnce(r.Key, "no secret key is available")
			continue
		}
		plain, err := s.o.Cipher.Decrypt(r.Key, r.Value)
		if err != nil {
			bad[r.Key] = true
			s.warnOnce(r.Key, err.Error())
			continue
		}
		ui[r.Key] = plain
	}
	return ui, bad, nil
}

func (s *Service) resolve(ui map[string]string, bad map[string]bool) (Resolved, error) {
	cfg, err := config.Load(s.o.ConfigPath, s.getenv)
	if err != nil {
		return Resolved{}, err
	}
	file, err := fileValues(s.o.ConfigPath)
	if err != nil {
		return Resolved{}, err
	}
	res := Resolved{Config: cfg, ConfigFile: s.o.ConfigPath, state: make(map[string]fieldState, len(fields))}
	for i := range fields {
		f := &fields[i]
		st := fieldState{source: SourceDefault}
		switch {
		case s.getenv(f.env) != "":
			st.source = SourceEnv
		case file[f.key] != "":
			st.source = SourceFile
		case ui[f.key] != "":
			if err := f.apply(&res.Config, ui[f.key]); err != nil {
				s.warnOnce(f.key, "ignoring saved value: "+err.Error())
			} else {
				st.source = SourceUI
			}
		case bad[f.key]:
			st.hint = HintUndecryptable
		}
		res.state[f.key] = st
	}
	return res, nil
}

func (s *Service) warnOnce(key, reason string) {
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	if s.warned[key] {
		return
	}
	s.warned[key] = true
	s.log.Warn("saved setting could not be used", "key", key, "reason", reason)
}

// fileValues flattens the config file into dotted keys, keeping non-empty values.
func fileValues(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	flatten("", root, out)
	return out, nil
}

func flatten(prefix string, m map[string]any, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch x := v.(type) {
		case map[string]any:
			flatten(key, x, out)
		case nil:
		case string:
			if x != "" {
				out[key] = x
			}
		default:
			out[key] = fmt.Sprint(x)
		}
	}
}

func rawString(raw json.RawMessage) (string, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return "", nil
	}
	switch {
	case t[0] == '"':
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return "", errors.New("must be a string or a number")
		}
		return s, nil
	case t[0] == '-' || (t[0] >= '0' && t[0] <= '9'):
		var n json.Number
		if err := json.Unmarshal(t, &n); err != nil {
			return "", errors.New("must be a string or a number")
		}
		return n.String(), nil
	}
	return "", errors.New("must be a string or a number")
}

// View is the API shape of GET /api/settings. Secrets never appear in it.
type View struct {
	Fields   map[string]FieldView `json:"fields"`
	ReadOnly ReadOnly             `json:"read_only"`
}

type FieldView struct {
	Value  any    `json:"value,omitempty"`
	Secret bool   `json:"secret"`
	Set    bool   `json:"set"`
	Source Source `json:"source"`
	Locked bool   `json:"locked"`
	Env    string `json:"env"`
	Hint   string `json:"hint,omitempty"`
}

type ReadOnly struct {
	Listen     string `json:"listen"`
	DataDir    string `json:"data_dir"`
	ClaudeBin  string `json:"claude_bin"`
	WebAuth    bool   `json:"web_auth"`
	ConfigFile string `json:"config_file,omitempty"`
}

// View renders the settings; hints adds per-key notes such as HintDiscovered.
func (r Resolved) View(hints map[string]string) View {
	v := View{
		Fields: make(map[string]FieldView, len(fields)),
		ReadOnly: ReadOnly{Listen: r.Config.Listen, DataDir: r.Config.DataDir, ClaudeBin: r.Config.Claude.Bin,
			WebAuth: r.Config.Web.AuthEnabled(), ConfigFile: r.ConfigFile},
	}
	for i := range fields {
		f := &fields[i]
		st := r.state[f.key]
		if st.source == "" {
			st.source = SourceDefault
		}
		val, set := f.value(r.Config)
		fv := FieldView{Secret: f.typ == typeSecret, Set: set, Source: st.source,
			Locked: st.source == SourceEnv || st.source == SourceFile, Env: f.env, Hint: st.hint}
		if !fv.Secret {
			fv.Value = val
		}
		if fv.Hint == "" {
			fv.Hint = hints[f.key]
		}
		v.Fields[f.key] = fv
	}
	return v
}

// Missing lists what a run still needs, as setting keys.
func Missing(c config.Config) []string {
	missing := []string{}
	if c.TMDB.APIKey == "" {
		missing = append(missing, "tmdb.api_key")
	}
	if c.Radarr.URL == "" && c.Sonarr.URL == "" {
		missing = append(missing, "radarr.url or sonarr.url")
	}
	if c.Radarr.URL != "" && c.Radarr.APIKey == "" {
		missing = append(missing, "radarr.api_key")
	}
	if c.Sonarr.URL != "" && c.Sonarr.APIKey == "" {
		missing = append(missing, "sonarr.api_key")
	}
	return missing
}
