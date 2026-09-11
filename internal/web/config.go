package web

import (
	"net/url"

	"github.com/leandervdo/proposarr/internal/config"
)

// publicConfig is config.Config with every secret replaced by a boolean.
type publicConfig struct {
	Listen      string         `json:"listen"`
	DataDir     string         `json:"data_dir"`
	HistoryDays int            `json:"history_days"`
	SnapshotTTL string         `json:"snapshot_ttl"`
	Radarr      publicArr      `json:"radarr"`
	Sonarr      publicArr      `json:"sonarr"`
	Plex        publicPlex     `json:"plex"`
	Jellyfin    publicJellyfin `json:"jellyfin"`
	TMDB        publicTMDB     `json:"tmdb"`
	Claude      publicClaude   `json:"claude"`
	Web         publicWeb      `json:"web"`
	Movies      publicKind     `json:"movies"`
	Series      publicKind     `json:"series"`
}

type publicArr struct {
	URL                 string `json:"url"`
	APIKeySet           bool   `json:"api_key_set"`
	RootFolder          string `json:"root_folder"`
	MinimumAvailability string `json:"minimum_availability,omitempty"`
}

type publicPlex struct {
	URL      string `json:"url"`
	TokenSet bool   `json:"token_set"`
}

type publicJellyfin struct {
	URL       string `json:"url"`
	APIKeySet bool   `json:"api_key_set"`
	UserID    string `json:"user_id"`
}

type publicTMDB struct {
	APIKeySet bool   `json:"api_key_set"`
	Region    string `json:"region"`
}

type publicClaude struct {
	Bin          string  `json:"bin"`
	Auth         string  `json:"auth"`
	Timeout      string  `json:"timeout"`
	MaxBudgetUSD float64 `json:"max_budget_usd"`
}

type publicWeb struct {
	AuthEnabled bool `json:"auth_enabled"`
}

type publicKind struct {
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	Picks      int    `json:"picks"`
	Candidates int    `json:"candidates"`
	FreePicks  int    `json:"free_picks"`
	Seeds      int    `json:"seeds"`
	TopTitles  int    `json:"top_titles"`
}

func publicConfigOf(c config.Config) publicConfig {
	kind := func(k config.KindSettings) publicKind {
		return publicKind{Model: k.Model, Effort: k.Effort, Picks: k.Picks, Candidates: k.Candidates, FreePicks: k.FreePicks, Seeds: k.Seeds, TopTitles: k.TopTitles}
	}
	return publicConfig{
		Listen:      c.Listen,
		DataDir:     c.DataDir,
		HistoryDays: c.HistoryDays,
		SnapshotTTL: c.SnapshotTTL.String(),
		Radarr:      publicArr{URL: redactURL(c.Radarr.URL), APIKeySet: c.Radarr.APIKey != "", RootFolder: c.Radarr.RootFolder, MinimumAvailability: c.Radarr.MinimumAvailability},
		Sonarr:      publicArr{URL: redactURL(c.Sonarr.URL), APIKeySet: c.Sonarr.APIKey != "", RootFolder: c.Sonarr.RootFolder},
		Plex:        publicPlex{URL: redactURL(c.Plex.URL), TokenSet: c.Plex.Token != ""},
		Jellyfin:    publicJellyfin{URL: redactURL(c.Jellyfin.URL), APIKeySet: c.Jellyfin.APIKey != "", UserID: c.Jellyfin.UserID},
		TMDB:        publicTMDB{APIKeySet: c.TMDB.APIKey != "", Region: c.TMDB.Region},
		Claude:      publicClaude{Bin: c.Claude.Bin, Auth: claudeAuth(c), Timeout: c.Claude.Timeout.String(), MaxBudgetUSD: c.Claude.MaxBudgetUSD},
		Web:         publicWeb{AuthEnabled: c.Web.AuthEnabled()},
		Movies:      kind(c.Movies),
		Series:      kind(c.Series),
	}
}

// redactURL drops user:password and the query string, which may carry credentials.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}
