// Package pipeline runs one recommendation pass:
// snapshot -> history -> profile -> candidates -> agent -> verify.
package pipeline

import (
	"context"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/candidates"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

// Library is the owned-titles snapshot (snapshot.Library).
type Library interface {
	Titles(ctx context.Context, kind media.Kind) ([]media.Title, error)
}

// Metadata is TMDB (tmdb.Client).
type Metadata interface {
	candidates.Related
	Details(ctx context.Context, kind media.Kind, id int, region string) (tmdb.Details, error)
	Search(ctx context.Context, kind media.Kind, query string, year int) ([]tmdb.Item, error)
}

// Exclusions are TMDB ids a user already decided on (ignored, later, requested).
type Exclusions interface {
	Excluded(ctx context.Context, kind media.Kind) (map[int]bool, error)
}

// RatingsSource looks up real ratings for a title (Radarr/Sonarr metadata).
// It returns nil when none are known.
type RatingsSource interface {
	Ratings(ctx context.Context, kind media.Kind, tmdbID int) (*Ratings, error)
}

// Deps are the ports a run needs. History, Discover, Exclusions and Ratings are optional.
type Deps struct {
	Library    Library
	History    []history.Source
	Meta       Metadata
	Discover   candidates.Discover // movies only
	Exclusions Exclusions
	Ratings    RatingsSource
	Agent      agent.Provider
	Progress   func(msg string) // optional progress lines
	Now        func() time.Time
}

// Request is one run's settings.
type Request struct {
	Kind         media.Kind
	Vibe         string
	OpenSearch   bool // search by Vibe alone, without history, profile or candidates
	Model        string
	Effort       string
	Picks        int
	Candidates   int
	FreePicks    int
	Seeds        int
	TopTitles    int
	HistoryDays  int
	Region       string
	AgentEnv     []string
	Timeout      time.Duration
	MaxBudgetUSD float64
}

type Pick struct {
	TMDBID    int        `json:"tmdb_id"`
	IMDBID    string     `json:"imdb_id,omitempty"`
	Kind      media.Kind `json:"kind"`
	Title     string     `json:"title"`
	Year      int        `json:"year,omitempty"`
	Reason    string     `json:"reason"`
	RelatedTo []string   `json:"related_to"`
	Score     int        `json:"score"`
	Source    string     `json:"source"` // candidate | free
	Overview  string     `json:"overview,omitempty"`
	Genres    []string   `json:"genres,omitempty"`
	Rating    float64    `json:"rating,omitempty"`
	Streaming []string   `json:"streaming,omitempty"`
	PosterURL string     `json:"poster_url,omitempty"`
	Ratings   *Ratings   `json:"ratings,omitempty"`
}

// Ratings are real third-party ratings; zero values are unknown.
type Ratings struct {
	IMDB           *IMDBRating `json:"imdb,omitempty"`
	RottenTomatoes int         `json:"rotten_tomatoes,omitempty"` // critic score, 0-100
	Metacritic     int         `json:"metacritic,omitempty"`      // 0-100
}

type IMDBRating struct {
	Value float64 `json:"value"` // 0-10
	Votes int     `json:"votes"`
}

type Rejected struct {
	TMDBID int    `json:"tmdb_id,omitempty"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type Run struct {
	Kind           media.Kind      `json:"kind"`
	Vibe           string          `json:"vibe,omitempty"`
	OpenSearch     bool            `json:"open_search,omitempty"`
	Model          string          `json:"model"`
	Effort         string          `json:"effort"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     time.Time       `json:"finished_at"`
	LibraryCount   int             `json:"library_count"`
	HistoryCount   int             `json:"history_count"`
	CandidateCount int             `json:"candidate_count"`
	Profile        profile.Profile `json:"profile"`
	Picks          []Pick          `json:"picks"`
	Rejected       []Rejected      `json:"rejected,omitempty"`
	Warnings       []string        `json:"warnings,omitempty"`
	CostUSD        float64         `json:"cost_usd"`
	Usage          agent.Usage     `json:"usage"`
	NumTurns       int             `json:"num_turns"`
	SessionID      string          `json:"session_id,omitempty"`
}

type Pipeline struct{ d Deps }

func New(d Deps) *Pipeline { return &Pipeline{d: d} }
