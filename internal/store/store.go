// Package store persists runs, picks, verdicts and requests in SQLite.
package store

import (
	"context"
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
	ID             int64               `json:"id"`
	Kind           media.Kind          `json:"kind"`
	Vibe           string              `json:"vibe,omitempty"`
	Model          string              `json:"model"`
	Effort         string              `json:"effort"`
	Status         RunStatus           `json:"status"`
	Error          string              `json:"error,omitempty"`
	StartedAt      time.Time           `json:"started_at"`
	FinishedAt     *time.Time          `json:"finished_at,omitempty"`
	CostUSD        float64             `json:"cost_usd"`
	InputTokens    int                 `json:"input_tokens"` // input + cache creation + cache read
	OutputTokens   int                 `json:"output_tokens"`
	NumTurns       int                 `json:"num_turns"`
	SessionID      string              `json:"session_id,omitempty"`
	LibraryCount   int                 `json:"library_count"`
	HistoryCount   int                 `json:"history_count"`
	CandidateCount int                 `json:"candidate_count"`
	PickCount      int                 `json:"pick_count"`
	Warnings       []string            `json:"warnings"`
	Rejected       []pipeline.Rejected `json:"rejected"`
	Profile        *profile.Profile    `json:"profile,omitempty"` // only filled by GetRun
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
}

type Pick struct {
	ID    int64 `json:"id"`
	RunID int64 `json:"run_id"`
	pipeline.Pick
	Verdict    Verdict    `json:"verdict,omitempty"`
	VerdictAt  *time.Time `json:"verdict_at,omitempty"`
	LaterUntil *time.Time `json:"later_until,omitempty"`
	Request    *Request   `json:"request,omitempty"` // latest request for this pick
}

type PickFilter struct {
	Kind      media.Kind // "" means both
	RunID     int64      // 0 means any run
	LatestRun bool       // only the most recent succeeded run of each kind; overrides RunID
	Verdict   *Verdict   // nil means any; &VerdictNone means undecided
	Limit     int        // 0 means 200
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
	GetPick(ctx context.Context, id int64) (Pick, error)
	// SetVerdict records a verdict for (kind, tmdbID); VerdictNone clears it.
	// until is only meaningful for VerdictLater.
	SetVerdict(ctx context.Context, kind media.Kind, tmdbID int, v Verdict, until *time.Time) error
	RecordRequest(ctx context.Context, r Request) error
	// Excluded returns ids that must not be proposed again: accepted, ignored,
	// later with until in the future, or successfully requested.
	Excluded(ctx context.Context, kind media.Kind) (map[int]bool, error)
	Close() error
}
