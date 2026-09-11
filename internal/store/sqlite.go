package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/profile"
)

var (
	_ Store               = (*SQLite)(nil)
	_ pipeline.Exclusions = (*SQLite)(nil)
)

// Fixed-width UTC timestamps, so text comparison in SQL orders correctly.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

var migrations = [][]string{
	{
		`CREATE TABLE runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			vibe TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			effort TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT,
			cost_usd REAL NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			num_turns INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL DEFAULT '',
			library_count INTEGER NOT NULL DEFAULT 0,
			history_count INTEGER NOT NULL DEFAULT 0,
			candidate_count INTEGER NOT NULL DEFAULT 0,
			pick_count INTEGER NOT NULL DEFAULT 0,
			warnings TEXT NOT NULL DEFAULT '[]',
			rejected TEXT NOT NULL DEFAULT '[]',
			profile TEXT
		)`,
		`CREATE INDEX runs_kind_status ON runs(kind, status, id)`,
		`CREATE TABLE picks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			tmdb_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			year INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			related_to TEXT NOT NULL DEFAULT '[]',
			score INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT '',
			overview TEXT NOT NULL DEFAULT '',
			genres TEXT NOT NULL DEFAULT '[]',
			rating REAL NOT NULL DEFAULT 0,
			streaming TEXT NOT NULL DEFAULT '[]',
			poster_url TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX picks_kind_tmdb ON picks(kind, tmdb_id)`,
		`CREATE INDEX picks_run ON picks(run_id)`,
		`CREATE TABLE verdicts (
			tmdb_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			verdict TEXT NOT NULL,
			until TEXT,
			decided_at TEXT NOT NULL,
			PRIMARY KEY (tmdb_id, kind)
		)`,
		`CREATE TABLE requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pick_id INTEGER NOT NULL REFERENCES picks(id) ON DELETE CASCADE,
			app TEXT NOT NULL,
			target_id INTEGER NOT NULL DEFAULT 0,
			quality_profile TEXT NOT NULL DEFAULT '',
			root_folder TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			requested_at TEXT NOT NULL
		)`,
		`CREATE INDEX requests_pick ON requests(pick_id, id)`,
	},
	{
		`CREATE TABLE settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			secret INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
	},
	{
		`ALTER TABLE runs ADD COLUMN use_taste INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE picks ADD COLUMN imdb_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE picks ADD COLUMN ratings TEXT`,
	},
}

// SQLite is the Store backed by a single SQLite file.
type SQLite struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies migrations.
// Runs left in status running by a previous process are marked failed.
func Open(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	s := &SQLite{db: db, now: time.Now}
	ctx := context.Background()
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE runs SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
		RunFailed, "interrupted: server restarted", fmtTime(s.now()), RunRunning); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: mark interrupted runs: %w", err)
	}
	return s, nil
}

func (s *SQLite) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	for i := current; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: migrate %d: %w", i+1, err)
		}
		for _, stmt := range migrations[i] {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: migrate %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migrate %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: migrate %d: %w", i+1, err)
		}
	}
	return nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) CreateRun(ctx context.Context, r Run) (int64, error) {
	if r.StartedAt.IsZero() {
		r.StartedAt = s.now()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (kind, vibe, use_taste, model, effort, status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(r.Kind), r.Vibe, r.UseTaste, r.Model, r.Effort, RunRunning, fmtTime(r.StartedAt))
	if err != nil {
		return 0, fmt.Errorf("store: create run: %w", err)
	}
	return res.LastInsertId()
}

func (s *SQLite) FinishRun(ctx context.Context, id int64, run *pipeline.Run, runErr error) error {
	status, errText := RunSucceeded, ""
	var limit *agent.SessionLimitError
	switch {
	case errors.As(runErr, &limit):
		status, errText = RunRateLimited, runErr.Error()
	case runErr != nil:
		status, errText = RunFailed, runErr.Error()
	}
	finished := fmtTime(s.now())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: finish run: %w", err)
	}
	defer tx.Rollback()

	var res sql.Result
	if run == nil {
		res, err = tx.ExecContext(ctx, `UPDATE runs SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
			status, errText, finished, id)
	} else {
		warnings, rejected, prof, jerr := runJSON(run)
		if jerr != nil {
			return jerr
		}
		u := run.Usage
		res, err = tx.ExecContext(ctx, `UPDATE runs SET status = ?, error = ?, finished_at = ?,
			cost_usd = ?, input_tokens = ?, output_tokens = ?, num_turns = ?, session_id = ?,
			library_count = ?, history_count = ?, candidate_count = ?, pick_count = ?,
			warnings = ?, rejected = ?, profile = ? WHERE id = ?`,
			status, errText, finished,
			run.CostUSD, u.InputTokens+u.CacheCreationInputTokens+u.CacheReadInputTokens, u.OutputTokens, run.NumTurns, run.SessionID,
			run.LibraryCount, run.HistoryCount, run.CandidateCount, len(run.Picks),
			warnings, rejected, prof, id)
	}
	if err != nil {
		return fmt.Errorf("store: finish run: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: finish run: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}

	if run != nil && len(run.Picks) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO picks (run_id, tmdb_id, imdb_id, kind, title, year, reason, related_to,
			score, source, overview, genres, rating, streaming, poster_url, ratings) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("store: finish run: %w", err)
		}
		defer stmt.Close()
		for _, p := range run.Picks {
			kind := p.Kind
			if kind == "" {
				kind = run.Kind
			}
			related, _ := json.Marshal(nonNil(p.RelatedTo))
			genres, _ := json.Marshal(nonNil(p.Genres))
			streaming, _ := json.Marshal(nonNil(p.Streaming))
			var ratings any
			if p.Ratings != nil {
				b, err := json.Marshal(p.Ratings)
				if err != nil {
					return fmt.Errorf("store: encode ratings: %w", err)
				}
				ratings = string(b)
			}
			if _, err := stmt.ExecContext(ctx, id, p.TMDBID, p.IMDBID, string(kind), p.Title, p.Year, p.Reason, string(related),
				p.Score, p.Source, p.Overview, string(genres), p.Rating, string(streaming), p.PosterURL, ratings); err != nil {
				return fmt.Errorf("store: insert pick: %w", err)
			}
		}
	}
	return tx.Commit()
}

func runJSON(run *pipeline.Run) (warnings, rejected string, prof any, err error) {
	w, err := json.Marshal(nonNil(run.Warnings))
	if err != nil {
		return "", "", nil, fmt.Errorf("store: encode warnings: %w", err)
	}
	r, err := json.Marshal(nonNil(run.Rejected))
	if err != nil {
		return "", "", nil, fmt.Errorf("store: encode rejected: %w", err)
	}
	if run.Profile.Kind != "" || len(run.Profile.Top) > 0 {
		p, err := json.Marshal(run.Profile)
		if err != nil {
			return "", "", nil, fmt.Errorf("store: encode profile: %w", err)
		}
		prof = string(p)
	}
	return string(w), string(r), prof, nil
}

const runColumns = `id, kind, vibe, use_taste, model, effort, status, error, started_at, finished_at, cost_usd,
	input_tokens, output_tokens, num_turns, session_id, library_count, history_count,
	candidate_count, pick_count, warnings, rejected`

type scanner interface{ Scan(dest ...any) error }

func scanRun(sc scanner, extra ...any) (Run, error) {
	var (
		r                     Run
		kind, status, started string
		finished              sql.NullString
		warnings, rejected    string
	)
	dest := []any{&r.ID, &kind, &r.Vibe, &r.UseTaste, &r.Model, &r.Effort, &status, &r.Error, &started, &finished, &r.CostUSD,
		&r.InputTokens, &r.OutputTokens, &r.NumTurns, &r.SessionID, &r.LibraryCount, &r.HistoryCount,
		&r.CandidateCount, &r.PickCount, &warnings, &rejected}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
		return Run{}, err
	}
	r.Kind, r.Status = media.Kind(kind), RunStatus(status)
	var err error
	if r.StartedAt, err = parseTime(started); err != nil {
		return Run{}, err
	}
	if r.FinishedAt, err = parseNullTime(finished); err != nil {
		return Run{}, err
	}
	if r.Warnings, err = decodeSlice[string](warnings); err != nil {
		return Run{}, err
	}
	if r.Rejected, err = decodeSlice[pipeline.Rejected](rejected); err != nil {
		return Run{}, err
	}
	return r, nil
}

func (s *SQLite) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list runs: %w", err)
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list runs: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) GetRun(ctx context.Context, id int64) (Run, []Pick, error) {
	var prof sql.NullString
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+`, profile FROM runs WHERE id = ?`, id), &prof)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, nil, ErrNotFound
	}
	if err != nil {
		return Run{}, nil, fmt.Errorf("store: get run: %w", err)
	}
	if r.Profile, err = decodeProfile(prof); err != nil {
		return Run{}, nil, err
	}
	picks, err := s.queryPicks(ctx, `p.run_id = ?`, []any{id}, -1)
	if err != nil {
		return Run{}, nil, err
	}
	return r, picks, nil
}

func (s *SQLite) LatestProfile(ctx context.Context, kind media.Kind) (*profile.Profile, error) {
	var prof sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT profile FROM runs WHERE kind = ? AND status = ? AND profile IS NOT NULL
		ORDER BY id DESC LIMIT 1`, string(kind), RunSucceeded).Scan(&prof)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: latest profile: %w", err)
	}
	return decodeProfile(prof)
}

func (s *SQLite) ListPicks(ctx context.Context, f PickFilter) ([]Pick, error) {
	var (
		where []string
		args  []any
	)
	if f.Kind != "" {
		where = append(where, `p.kind = ?`)
		args = append(args, string(f.Kind))
	}
	switch {
	case f.LatestRun:
		sub := `SELECT MAX(id) FROM runs WHERE status = ?`
		args = append(args, RunSucceeded)
		if f.Kind != "" {
			sub += ` AND kind = ?`
			args = append(args, string(f.Kind))
		}
		where = append(where, `p.run_id IN (`+sub+` GROUP BY kind)`)
	case f.RunID != 0:
		where = append(where, `p.run_id = ?`)
		args = append(args, f.RunID)
	}
	if f.Verdict != nil {
		if *f.Verdict == VerdictNone {
			where = append(where, `v.verdict IS NULL`)
		} else {
			where = append(where, `v.verdict = ?`)
			args = append(args, string(*f.Verdict))
		}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	return s.queryPicks(ctx, strings.Join(where, " AND "), args, limit)
}

func (s *SQLite) GetPick(ctx context.Context, id int64) (Pick, error) {
	picks, err := s.queryPicks(ctx, `p.id = ?`, []any{id}, 1)
	if err != nil {
		return Pick{}, err
	}
	if len(picks) == 0 {
		return Pick{}, ErrNotFound
	}
	return picks[0], nil
}

// queryPicks selects picks joined with their verdict and latest request.
// limit < 0 means no limit.
func (s *SQLite) queryPicks(ctx context.Context, where string, args []any, limit int) ([]Pick, error) {
	q := `SELECT p.id, p.run_id, p.tmdb_id, p.imdb_id, p.kind, p.title, p.year, p.reason, p.related_to, p.score, p.source,
			p.overview, p.genres, p.rating, p.streaming, p.poster_url, p.ratings,
			v.verdict, v.until, v.decided_at,
			rq.app, rq.target_id, rq.quality_profile, rq.root_folder, rq.status, rq.error, rq.requested_at
		FROM picks p
		LEFT JOIN verdicts v ON v.tmdb_id = p.tmdb_id AND v.kind = p.kind
		LEFT JOIN requests rq ON rq.id = (SELECT MAX(r2.id) FROM requests r2 WHERE r2.pick_id = p.id)`
	if where != "" {
		q += ` WHERE ` + where
	}
	q += ` ORDER BY p.run_id DESC, p.score DESC, p.id ASC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: query picks: %w", err)
	}
	defer rows.Close()

	out := []Pick{}
	for rows.Next() {
		var (
			p                                       Pick
			kind, related, genres, streaming        string
			ratings                                 sql.NullString
			verdict, until, decided                 sql.NullString
			app, qp, root, rstatus, rerr, requested sql.NullString
			target                                  sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.RunID, &p.TMDBID, &p.IMDBID, &kind, &p.Title, &p.Year, &p.Reason, &related, &p.Score, &p.Source,
			&p.Overview, &genres, &p.Rating, &streaming, &p.PosterURL, &ratings,
			&verdict, &until, &decided,
			&app, &target, &qp, &root, &rstatus, &rerr, &requested); err != nil {
			return nil, fmt.Errorf("store: scan pick: %w", err)
		}
		p.Kind = media.Kind(kind)
		if p.RelatedTo, err = decodeSlice[string](related); err != nil {
			return nil, err
		}
		if p.Genres, err = decodeSlice[string](genres); err != nil {
			return nil, err
		}
		if p.Streaming, err = decodeSlice[string](streaming); err != nil {
			return nil, err
		}
		if ratings.Valid && ratings.String != "" {
			p.Ratings = &pipeline.Ratings{}
			if err := json.Unmarshal([]byte(ratings.String), p.Ratings); err != nil {
				return nil, fmt.Errorf("store: decode ratings: %w", err)
			}
		}
		if verdict.Valid {
			p.Verdict = Verdict(verdict.String)
			if p.VerdictAt, err = parseNullTime(decided); err != nil {
				return nil, err
			}
			if p.LaterUntil, err = parseNullTime(until); err != nil {
				return nil, err
			}
		}
		if app.Valid {
			req := &Request{PickID: p.ID, App: app.String, TargetID: int(target.Int64), QualityProfile: qp.String,
				RootFolder: root.String, Status: rstatus.String, Error: rerr.String}
			if req.RequestedAt, err = parseTime(requested.String); err != nil {
				return nil, err
			}
			p.Request = req
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) SetVerdict(ctx context.Context, kind media.Kind, tmdbID int, v Verdict, until *time.Time) error {
	if v == VerdictNone {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM verdicts WHERE tmdb_id = ? AND kind = ?`, tmdbID, string(kind)); err != nil {
			return fmt.Errorf("store: clear verdict: %w", err)
		}
		return nil
	}
	switch v {
	case VerdictAccepted, VerdictIgnored, VerdictLater:
	default:
		return fmt.Errorf("store: unknown verdict %q", v)
	}
	var untilArg any
	if v == VerdictLater && until != nil {
		untilArg = fmtTime(*until)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO verdicts (tmdb_id, kind, verdict, until, decided_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (tmdb_id, kind) DO UPDATE SET verdict = excluded.verdict, until = excluded.until, decided_at = excluded.decided_at`,
		tmdbID, string(kind), string(v), untilArg, fmtTime(s.now()))
	if err != nil {
		return fmt.Errorf("store: set verdict: %w", err)
	}
	return nil
}

func (s *SQLite) RecordRequest(ctx context.Context, r Request) error {
	if r.RequestedAt.IsZero() {
		r.RequestedAt = s.now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests (pick_id, app, target_id, quality_profile, root_folder, status, error, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.PickID, r.App, r.TargetID, r.QualityProfile, r.RootFolder, r.Status, r.Error, fmtTime(r.RequestedAt))
	if err != nil {
		return fmt.Errorf("store: record request: %w", err)
	}
	return nil
}

func (s *SQLite) Excluded(ctx context.Context, kind media.Kind) (map[int]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM verdicts WHERE kind = ? AND
			(verdict IN (?, ?) OR (verdict = ? AND (until IS NULL OR until > ?)))
		UNION
		SELECT p.tmdb_id FROM requests r JOIN picks p ON p.id = r.pick_id WHERE p.kind = ? AND r.status = 'added'`,
		string(kind), VerdictAccepted, VerdictIgnored, VerdictLater, fmtTime(s.now()), string(kind))
	if err != nil {
		return nil, fmt.Errorf("store: excluded: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: excluded: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: bad timestamp %q: %w", s, err)
	}
	return t, nil
}

func parseNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func decodeSlice[T any](s string) ([]T, error) {
	out := []T{}
	if s == "" || s == "null" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("store: decode json: %w", err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

func decodeProfile(ns sql.NullString) (*profile.Profile, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	var p profile.Profile
	if err := json.Unmarshal([]byte(ns.String), &p); err != nil {
		return nil, fmt.Errorf("store: decode profile: %w", err)
	}
	return &p, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
