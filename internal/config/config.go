// Package config loads settings from an optional YAML file, then PROPOSARR_*
// environment variables, which win.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/leandervdo/proposarr/internal/media"
)

type Config struct {
	Listen      string   `yaml:"listen"`
	DataDir     string   `yaml:"data_dir"`
	HistoryDays int      `yaml:"history_days"`
	SnapshotTTL Duration `yaml:"snapshot_ttl"`

	Sonarr   Arr      `yaml:"sonarr"`
	Radarr   Arr      `yaml:"radarr"`
	Plex     Plex     `yaml:"plex"`
	Jellyfin Jellyfin `yaml:"jellyfin"`
	TMDB     TMDB     `yaml:"tmdb"`
	Claude   Claude   `yaml:"claude"`
	Web      Web      `yaml:"web"`

	Movies KindSettings `yaml:"movies"`
	Series KindSettings `yaml:"series"`
}

type Arr struct {
	URL        string `yaml:"url"`
	APIKey     string `yaml:"api_key"`
	RootFolder string `yaml:"root_folder"`
	// MinimumAvailability is Radarr only: announced, inCinemas or released.
	MinimumAvailability string `yaml:"minimum_availability"`
}

func (a Arr) Configured() bool { return a.URL != "" && a.APIKey != "" }

// Web holds optional HTTP Basic credentials for `proposarr serve`.
type Web struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// AuthEnabled reports whether the web UI requires a login.
func (w Web) AuthEnabled() bool { return w.Username != "" && w.Password != "" }

type Plex struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

func (p Plex) Configured() bool { return p.URL != "" && p.Token != "" }

type Jellyfin struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	UserID string `yaml:"user_id"` // empty merges every user
}

func (j Jellyfin) Configured() bool { return j.URL != "" && j.APIKey != "" }

type TMDB struct {
	APIKey string `yaml:"api_key"` // v3 key or v4 read access token
	Region string `yaml:"region"`  // watch-provider region, ISO 3166-1
}

type Claude struct {
	Bin          string   `yaml:"bin"`
	OAuthToken   string   `yaml:"oauth_token"`
	APIKey       string   `yaml:"api_key"`
	Timeout      Duration `yaml:"timeout"`
	MaxBudgetUSD float64  `yaml:"max_budget_usd"`
}

// Auth env var names consumed by the claude binary.
const (
	EnvOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
	EnvAPIKey     = "ANTHROPIC_API_KEY"
)

// Auth returns the env var and value to hand the claude binary. Both empty
// means no credential is configured and the binary's own login is used.
func (c Claude) Auth() (name, value string, err error) {
	switch {
	case c.OAuthToken != "" && c.APIKey != "":
		return "", "", errors.New("both a Claude OAuth token and an Anthropic API key are set; configure only one")
	case c.OAuthToken != "":
		return EnvOAuthToken, c.OAuthToken, nil
	case c.APIKey != "":
		return EnvAPIKey, c.APIKey, nil
	}
	return "", "", nil
}

type KindSettings struct {
	Model      string `yaml:"model"`
	Effort     string `yaml:"effort"`
	Picks      int    `yaml:"picks"`
	Candidates int    `yaml:"candidates"`
	FreePicks  int    `yaml:"free_picks"`
	Seeds      int    `yaml:"seeds"`
	TopTitles  int    `yaml:"top_titles"`
}

// Duration decodes "90s", "10m", "6h" from YAML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	d.Duration = v
	return nil
}

func defaultKind() KindSettings {
	return KindSettings{Model: "claude-sonnet-5", Effort: "medium", Picks: 10, Candidates: 150, FreePicks: 3, Seeds: 15, TopTitles: 500}
}

// Default returns the settings used when nothing is configured.
func Default() Config {
	return Config{
		Listen:      ":8585",
		DataDir:     "data",
		HistoryDays: 180,
		SnapshotTTL: Duration{6 * time.Hour},
		Radarr:      Arr{MinimumAvailability: "released"},
		TMDB:        TMDB{Region: "US"},
		Claude:      Claude{Bin: "claude", Timeout: Duration{10 * time.Minute}},
		Movies:      defaultKind(),
		Series:      defaultKind(),
	}
}

// Load reads path (skipped when empty) and applies env overrides from getenv.
func Load(path string, getenv func(string) string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg, getenv); err != nil {
		return cfg, err
	}
	if (cfg.Web.Username == "") != (cfg.Web.Password == "") {
		return cfg, errors.New("web: set both username and password (PROPOSARR_WEB_USERNAME, PROPOSARR_WEB_PASSWORD) or neither")
	}
	return cfg, nil
}

// Kind returns the per-kind settings.
func (c Config) Kind(k media.Kind) KindSettings {
	if k == media.Series {
		return c.Series
	}
	return c.Movies
}

func applyEnv(c *Config, getenv func(string) string) error {
	var errs []error
	str := func(name string, dst *string) {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	num := func(name string, dst *int) {
		if v := getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				return
			}
			*dst = n
		}
	}
	dur := func(name string, dst *Duration) {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				return
			}
			dst.Duration = d
		}
	}

	str("PROPOSARR_LISTEN", &c.Listen)
	str("PROPOSARR_WEB_USERNAME", &c.Web.Username)
	str("PROPOSARR_WEB_PASSWORD", &c.Web.Password)
	str("PROPOSARR_DATA_DIR", &c.DataDir)
	num("PROPOSARR_HISTORY_DAYS", &c.HistoryDays)
	dur("PROPOSARR_SNAPSHOT_TTL", &c.SnapshotTTL)

	for _, a := range []struct {
		prefix string
		dst    *Arr
	}{{"PROPOSARR_SONARR_", &c.Sonarr}, {"PROPOSARR_RADARR_", &c.Radarr}} {
		str(a.prefix+"URL", &a.dst.URL)
		str(a.prefix+"API_KEY", &a.dst.APIKey)
		str(a.prefix+"ROOT_FOLDER", &a.dst.RootFolder)
	}
	str("PROPOSARR_RADARR_MINIMUM_AVAILABILITY", &c.Radarr.MinimumAvailability)

	str("PROPOSARR_PLEX_URL", &c.Plex.URL)
	str("PROPOSARR_PLEX_TOKEN", &c.Plex.Token)
	str("PROPOSARR_JELLYFIN_URL", &c.Jellyfin.URL)
	str("PROPOSARR_JELLYFIN_API_KEY", &c.Jellyfin.APIKey)
	str("PROPOSARR_JELLYFIN_USER_ID", &c.Jellyfin.UserID)
	str("PROPOSARR_TMDB_API_KEY", &c.TMDB.APIKey)
	str("PROPOSARR_TMDB_REGION", &c.TMDB.Region)

	str("PROPOSARR_CLAUDE_BIN", &c.Claude.Bin)
	str(EnvOAuthToken, &c.Claude.OAuthToken)
	str(EnvAPIKey, &c.Claude.APIKey)
	dur("PROPOSARR_CLAUDE_TIMEOUT", &c.Claude.Timeout)
	if v := getenv("PROPOSARR_CLAUDE_MAX_BUDGET_USD"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("PROPOSARR_CLAUDE_MAX_BUDGET_USD: %w", err))
		} else {
			c.Claude.MaxBudgetUSD = f
		}
	}

	for _, k := range []struct {
		prefix string
		dst    *KindSettings
	}{{"PROPOSARR_MOVIES_", &c.Movies}, {"PROPOSARR_SERIES_", &c.Series}} {
		str(k.prefix+"MODEL", &k.dst.Model)
		str(k.prefix+"EFFORT", &k.dst.Effort)
		num(k.prefix+"PICKS", &k.dst.Picks)
		num(k.prefix+"CANDIDATES", &k.dst.Candidates)
		num(k.prefix+"FREE_PICKS", &k.dst.FreePicks)
		num(k.prefix+"SEEDS", &k.dst.Seeds)
		num(k.prefix+"TOP_TITLES", &k.dst.TopTitles)
	}

	c.Sonarr.URL = strings.TrimRight(c.Sonarr.URL, "/")
	c.Radarr.URL = strings.TrimRight(c.Radarr.URL, "/")
	c.Plex.URL = strings.TrimRight(c.Plex.URL, "/")
	c.Jellyfin.URL = strings.TrimRight(c.Jellyfin.URL, "/")
	return errors.Join(errs...)
}
