// Package store persists runs, picks, verdicts and requests in SQLite.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/profile"
)

type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunSucceeded   RunStatus = "succeeded"
	RunFailed      RunStatus = "failed"
	RunRateLimited RunStatus = "rate_limited"
)

// Verdict is the user's decision on a title, keyed by (tmdb_id, kind) across runs.
type Verdict string

const (
	VerdictNone     Verdict = ""
	VerdictAccepted Verdict = "accepted"
	VerdictIgnored  Verdict = "ignored"
	VerdictLater    Verdict = "later"
)

var ErrNotFound = errors.New("not found")

type Run struct {
	ID             int64                 `json:"id"`
	Kind           media.Kind            `json:"kind"`
	Vibe           string                `json:"vibe,omitempty"`
	UseTaste       bool                  `json:"use_taste"` // false for an open search
	Model          string                `json:"model"`
	Effort         string                `json:"effort"`
	Status         RunStatus             `json:"status"`
	Error          string                `json:"error,omitempty"`
	StartedAt      time.Time             `json:"started_at"`
	FinishedAt     *time.Time            `json:"finished_at,omitempty"`
	CostUSD        float64               `json:"cost_usd"`
	InputTokens    int                   `json:"input_tokens"` // input + cache creation + cache read
	OutputTokens   int                   `json:"output_tokens"`
	NumTurns       int                   `json:"num_turns"`
	SessionID      string                `json:"session_id,omitempty"`
	LibraryCount   int                   `json:"library_count"`
	HistoryCount   int                   `json:"history_count"`
	CandidateCount int                   `json:"candidate_count"`
	PickCount      int                   `json:"pick_count"`
	Warnings       []string              `json:"warnings"`
	Rejected       []pipeline.Rejected   `json:"rejected"`
	Owned          []pipeline.OwnedMatch `json:"owned"`             // open search: library titles that match the description
	Profile        *profile.Profile      `json:"profile,omitempty"` // only filled by GetRun
}

// LibrarySearch is a search Proposarr started for a title already in the
// library; only the latest one per title is kept. Kind, TMDBID and TargetID
// identify it and are not part of its JSON.
type LibrarySearch struct {
	Kind           media.Kind `json:"-"`
	TMDBID         int        `json:"-"`
	TargetID       int        `json:"-"` // the Radarr movie id
	QualityProfile string     `json:"quality_profile"`
	RequestedAt    time.Time  `json:"requested_at"`
	// Release is the request.ReleaseCheck: what Radarr's search did.
	Release json.RawMessage `json:"release"`
}

// Request is one add to Sonarr/Radarr, always with an explicitly chosen quality profile.
type Request struct {
	PickID         int64     `json:"pick_id"`
	App            string    `json:"app"` // radarr | sonarr
	TargetID       int       `json:"target_id,omitempty"`
	QualityProfile string    `json:"quality_profile"`
	RootFolder     string    `json:"root_folder"`
	Status         string    `json:"status"` // added | failed
	Error          string    `json:"error,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
	// Release is the request.ReleaseCheck of a Radarr movie: what Radarr's search did.
	Release json.RawMessage `json:"release,omitempty"`
}

type Pick struct {
	ID          int64     `json:"id"`
	RunID       int64     `json:"run_id"`
	RunVibe     string    `json:"run_vibe,omitempty"`
	RunUseTaste bool      `json:"run_use_taste"`
	FoundAt     time.Time `json:"found_at"` // the run's started_at
	pipeline.Pick
	Verdict    Verdict    `json:"verdict,omitempty"`
	VerdictAt  *time.Time `json:"verdict_at,omitempty"`
	LaterUntil *time.Time `json:"later_until,omitempty"`
	Request    *Request   `json:"request,omitempty"` // latest request for this pick
}

const (
	DefaultPickLimit = 200
	MaxPickLimit     = 1000
)

type PickFilter struct {
	Kind      media.Kind // "" means both
	RunID     int64      // 0 means any run, including failed ones
	LatestRun bool       // only the most recent succeeded run of each kind; overrides AllRuns and RunID
	AllRuns   bool       // every succeeded run; overrides RunID
	Verdict   *Verdict   // nil means any; &VerdictNone means undecided
	Added     bool       // the latest request has status added; ordered by its requested_at, newest first
	Search    *bool      // nil means any; true: open-search runs only; false: taste runs only
	Distinct  bool       // one pick per (tmdb_id, kind) among the matches: highest run id, then pick id
	// ExcludeTMDB leaves out picks of these TMDB ids (titles now in the library),
	// except picks whose latest request was added through Proposarr.
	ExcludeTMDB []int
	Limit       int // 0 means DefaultPickLimit; capped at MaxPickLimit
	Offset      int
}

type Store interface {
	// CreateRun inserts a run with status running and returns its id.
	CreateRun(ctx context.Context, r Run) (int64, error)
	// FinishRun stores the pipeline result and its picks. run may be nil when the
	// pipeline failed early. Status: rate_limited for *agent.SessionLimitError,
	// failed for any other error, else succeeded.
	FinishRun(ctx context.Context, id int64, run *pipeline.Run, runErr error) error
	ListRuns(ctx context.Context, limit int) ([]Run, error)
	GetRun(ctx context.Context, id int64) (Run, []Pick, error)
	// LatestProfile is the profile of the most recent succeeded run; nil when none.
	LatestProfile(ctx context.Context, kind media.Kind) (*profile.Profile, error)
	ListPicks(ctx context.Context, f PickFilter) ([]Pick, error)
	// CountPicks is the number of picks matching f, ignoring Limit and Offset.
	CountPicks(ctx context.Context, f PickFilter) (int, error)
	GetPick(ctx context.Context, id int64) (Pick, error)
	// SetVerdict records a verdict for (kind, tmdbID); VerdictNone clears it.
	// until is only meaningful for VerdictLater.
	SetVerdict(ctx context.Context, kind media.Kind, tmdbID int, v Verdict, until *time.Time) error
	RecordRequest(ctx context.Context, r Request) error
	// UpdateRequestRelease sets the release check, and the quality profile it
	// ended with, on the pick's latest request. ErrNotFound when there is none.
	UpdateRequestRelease(ctx context.Context, pickID int64, qualityProfile string, release json.RawMessage) error
	// RecordLibrarySearch stores a search for a library title, replacing the
	// previous one for (Kind, TMDBID).
	RecordLibrarySearch(ctx context.Context, s LibrarySearch) error
	// UpdateLibrarySearch sets the release check, and the quality profile it
	// ended with, on a title's search. ErrNotFound when there is none.
	UpdateLibrarySearch(ctx context.Context, kind media.Kind, tmdbID int, qualityProfile string, release json.RawMessage) error
	// LibrarySearches returns the searches for tmdbIDs that have one, by TMDB id.
	LibrarySearches(ctx context.Context, kind media.Kind, tmdbIDs []int) (map[int]LibrarySearch, error)
	// Excluded returns ids that must not be proposed again: accepted, ignored,
	// later with until in the future, or successfully requested.
	Excluded(ctx context.Context, kind media.Kind) (map[int]bool, error)
	Close() error
}
