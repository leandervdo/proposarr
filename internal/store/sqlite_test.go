package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/profile"
)

var ctx = context.Background()

func openTest(t *testing.T) (*SQLite, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "proposarr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func pick(id, score int, title string) pipeline.Pick {
	return pipeline.Pick{TMDBID: id, Title: title, Year: 2000 + id%20, Reason: "because", Score: score, Source: "candidate"}
}

// finishedRun creates a run and finishes it successfully with the given picks.
func finishedRun(t *testing.T, s *SQLite, kind media.Kind, picks ...pipeline.Pick) int64 {
	t.Helper()
	id, err := s.CreateRun(ctx, Run{Kind: kind, Model: "m", Effort: "e"})
	if err != nil {
		t.Fatal(err)
	}
	run := &pipeline.Run{Kind: kind, Picks: picks,
		Profile: profile.Profile{Kind: kind, Top: []profile.Entry{{TMDBID: 1, Title: "Seed", Weight: 3, Signal: "watched"}}}}
	if err := s.FinishRun(ctx, id, run, nil); err != nil {
		t.Fatal(err)
	}
	return id
}

func titles(ps []Pick) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Title)
	}
	return out
}

func TestReopenMigratesOnceAndMarksInterrupted(t *testing.T) {
	s, path := openTest(t)
	id, err := s.CreateRun(ctx, Run{Kind: media.Movies, Vibe: "cosy"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	var versions int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != len(migrations) {
		t.Errorf("schema_version rows = %d, want %d", versions, len(migrations))
	}
	r, _, err := s2.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RunFailed || r.Error != "interrupted: server restarted" || r.FinishedAt == nil || r.Vibe != "cosy" {
		t.Errorf("interrupted run = %+v", r)
	}
}

func TestMigrateFromV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proposarr.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{`CREATE TABLE schema_version (version INTEGER NOT NULL)`}
	for i, m := range migrations[:2] {
		stmts = append(stmts, m...)
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO schema_version (version) VALUES (%d)`, i+1))
	}
	stmts = append(stmts,
		`INSERT INTO runs (kind, vibe, status, started_at, finished_at) VALUES ('movies', 'old', 'succeeded', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:01:00.000000000Z')`,
		`INSERT INTO picks (run_id, tmdb_id, kind, title, score) VALUES (1, 603, 'movies', 'The Matrix', 90)`,
		`INSERT INTO settings (key, value, updated_at) VALUES ('movies.picks', '12', '2026-01-01T00:00:00.000000000Z')`,
	)
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("version = %d, %v", version, err)
	}
	old, picks, err := s.GetRun(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !old.UseTaste || old.Vibe != "old" || len(picks) != 1 || picks[0].Title != "The Matrix" || picks[0].IMDBID != "" || picks[0].Ratings != nil {
		t.Errorf("old run = %+v picks = %+v", old, picks)
	}
	if st, err := s.Settings(ctx); err != nil || len(st) != 1 || st[0].Value != "12" {
		t.Errorf("settings = %+v, %v", st, err)
	}

	search, err := s.CreateRun(ctx, Run{Kind: media.Series, Vibe: "short Korean thrillers", UseTaste: false})
	if err != nil {
		t.Fatal(err)
	}
	taste, err := s.CreateRun(ctx, Run{Kind: media.Movies, UseTaste: true})
	if err != nil {
		t.Fatal(err)
	}
	pk := pipeline.Pick{TMDBID: 95396, IMDBID: "tt11280740", Kind: media.Series, Title: "Severance", Score: 87, Source: "free",
		RelatedTo: []string{}, Genres: []string{}, Streaming: []string{},
		Ratings: &pipeline.Ratings{IMDB: &pipeline.IMDBRating{Value: 8.7, Votes: 300000}}}
	if err := s.FinishRun(ctx, search, &pipeline.Run{Kind: media.Series, OpenSearch: true, Picks: []pipeline.Pick{pk}}, nil); err != nil {
		t.Fatal(err)
	}
	r, got, err := s.GetRun(ctx, search)
	if err != nil {
		t.Fatal(err)
	}
	if r.UseTaste || r.Status != RunSucceeded || r.Profile != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Pick, pk) {
		t.Errorf("open search run = %+v picks = %+v", r, got)
	}
	runs, err := s.ListRuns(ctx, 10)
	if err != nil || len(runs) != 3 {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	for _, run := range runs {
		if want := run.ID != search; run.UseTaste != want {
			t.Errorf("run %d use_taste = %v, want %v", run.ID, run.UseTaste, want)
		}
	}
	_ = taste
}

func TestFinishRunSucceeded(t *testing.T) {
	s, _ := openTest(t)
	id, err := s.CreateRun(ctx, Run{Kind: media.Movies, Model: "claude-sonnet-5", Effort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	run := &pipeline.Run{
		Kind: media.Movies, CostUSD: 0.25, NumTurns: 3, SessionID: "sess",
		Usage:        agent.Usage{InputTokens: 10, CacheCreationInputTokens: 100, CacheReadInputTokens: 1000, OutputTokens: 42},
		LibraryCount: 185, HistoryCount: 12, CandidateCount: 150,
		Warnings: []string{"plex history failed"},
		Rejected: []pipeline.Rejected{{TMDBID: 9, Title: "Owned", Reason: "already in library"}},
		Profile:  profile.Profile{Kind: media.Movies, Top: []profile.Entry{{TMDBID: 603, Title: "The Matrix", Weight: 4, Signal: "rewatched"}}},
		Picks:    []pipeline.Pick{pick(1, 90, "A"), pick(2, 80, "B")},
	}
	if err := s.FinishRun(ctx, id, run, nil); err != nil {
		t.Fatal(err)
	}
	r, picks, err := s.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RunSucceeded || r.Error != "" || r.FinishedAt == nil {
		t.Errorf("status = %+v", r)
	}
	if r.InputTokens != 1110 || r.OutputTokens != 42 || r.CostUSD != 0.25 || r.NumTurns != 3 || r.SessionID != "sess" {
		t.Errorf("usage fields = %+v", r)
	}
	if r.LibraryCount != 185 || r.HistoryCount != 12 || r.CandidateCount != 150 || r.PickCount != 2 {
		t.Errorf("counts = %+v", r)
	}
	if !reflect.DeepEqual(r.Warnings, run.Warnings) || !reflect.DeepEqual(r.Rejected, run.Rejected) {
		t.Errorf("warnings/rejected = %v %v", r.Warnings, r.Rejected)
	}
	if r.Profile == nil || len(r.Profile.Top) != 1 || r.Profile.Top[0].Title != "The Matrix" {
		t.Errorf("profile = %+v", r.Profile)
	}
	if got := titles(picks); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("picks = %v", got)
	}
	if picks[0].Kind != media.Movies {
		t.Errorf("pick kind = %q, want movies from run", picks[0].Kind)
	}

	runs, err := s.ListRuns(ctx, 0)
	if err != nil || len(runs) != 1 || runs[0].Profile != nil {
		t.Errorf("ListRuns = %+v, %v (profile must be omitted)", runs, err)
	}
}

func TestFinishRunFailures(t *testing.T) {
	s, _ := openTest(t)

	failedID, _ := s.CreateRun(ctx, Run{Kind: media.Series})
	if err := s.FinishRun(ctx, failedID, nil, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	r, picks, err := s.GetRun(ctx, failedID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RunFailed || r.Error != "boom" || r.Warnings == nil || r.Rejected == nil || len(picks) != 0 || r.Profile != nil {
		t.Errorf("failed run = %+v picks=%v", r, picks)
	}

	limitedID, _ := s.CreateRun(ctx, Run{Kind: media.Movies})
	partial := &pipeline.Run{Kind: media.Movies, CandidateCount: 7, Warnings: []string{"w"}}
	runErr := fmt.Errorf("pipeline: %w", &agent.SessionLimitError{Message: "You've hit your session limit"})
	if err := s.FinishRun(ctx, limitedID, partial, runErr); err != nil {
		t.Fatal(err)
	}
	r, _, _ = s.GetRun(ctx, limitedID)
	if r.Status != RunRateLimited || r.CandidateCount != 7 || r.Error == "" {
		t.Errorf("rate limited run = %+v", r)
	}

	if err := s.FinishRun(ctx, 999, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishRun missing = %v, want ErrNotFound", err)
	}
}

func TestPickRoundTrip(t *testing.T) {
	s, _ := openTest(t)
	full := pipeline.Pick{
		TMDBID: 329865, IMDBID: "tt2543164", Kind: media.Movies, Title: "Arrival", Year: 2016, Reason: "Like Interstellar",
		RelatedTo: []string{"Interstellar (2014)"}, Score: 92, Source: "free", Overview: "Linguist meets aliens",
		Genres: []string{"Drama", "Science Fiction"}, Rating: 7.6, Streaming: []string{"Netflix"},
		PosterURL: "https://image.tmdb.org/t/p/w500/x.jpg",
		Ratings:   &pipeline.Ratings{IMDB: &pipeline.IMDBRating{Value: 7.9, Votes: 850000}, RottenTomatoes: 94, Metacritic: 81},
	}
	bare := pipeline.Pick{TMDBID: 2, Title: "Bare", Score: 10}
	runID := finishedRun(t, s, media.Movies, full, bare)

	picks, err := s.ListPicks(ctx, PickFilter{RunID: runID})
	if err != nil || len(picks) != 2 {
		t.Fatalf("ListPicks = %v, %v", picks, err)
	}
	if !reflect.DeepEqual(picks[0].Pick, full) || picks[0].RunID != runID {
		t.Errorf("round trip:\n got %+v\nwant %+v", picks[0].Pick, full)
	}
	if picks[1].RelatedTo == nil || picks[1].Genres == nil || picks[1].Streaming == nil {
		t.Errorf("empty slices must be non-nil: %+v", picks[1].Pick)
	}
	if picks[1].Ratings != nil || picks[1].IMDBID != "" {
		t.Errorf("bare pick ratings %+v imdb %q", picks[1].Ratings, picks[1].IMDBID)
	}

	got, err := s.GetPick(ctx, picks[0].ID)
	if err != nil || got.Title != "Arrival" || got.Verdict != VerdictNone || got.Request != nil {
		t.Errorf("GetPick = %+v, %v", got, err)
	}
}

func TestListPicksFilters(t *testing.T) {
	s, _ := openTest(t)
	run1 := finishedRun(t, s, media.Movies, pick(1, 70, "m1"), pick(2, 60, "m2"))
	run2 := finishedRun(t, s, media.Movies, pick(3, 50, "m3-low"), pick(4, 90, "m4-high"))
	run3 := finishedRun(t, s, media.Series, pick(5, 80, "s5"))
	failed, _ := s.CreateRun(ctx, Run{Kind: media.Movies})
	if err := s.FinishRun(ctx, failed, &pipeline.Run{Kind: media.Movies, Picks: []pipeline.Pick{pick(6, 99, "m6-failed")}}, errors.New("verify failed")); err != nil {
		t.Fatal(err)
	}
	_ = run1

	none := VerdictNone
	ignored := VerdictIgnored
	tests := []struct {
		name string
		f    PickFilter
		want []string
	}{
		{"kind movies ordered by run then score", PickFilter{Kind: media.Movies}, []string{"m6-failed", "m4-high", "m3-low", "m1", "m2"}},
		{"run id", PickFilter{RunID: run3}, []string{"s5"}},
		{"latest succeeded movies run", PickFilter{Kind: media.Movies, LatestRun: true}, []string{"m4-high", "m3-low"}},
		{"latest run per kind", PickFilter{LatestRun: true}, []string{"s5", "m4-high", "m3-low"}},
		{"latest overrides run id", PickFilter{Kind: media.Movies, LatestRun: true, RunID: run1}, []string{"m4-high", "m3-low"}},
		{"limit", PickFilter{Kind: media.Movies, Limit: 2}, []string{"m6-failed", "m4-high"}},
		{"undecided", PickFilter{Kind: media.Movies, LatestRun: true, Verdict: &none}, []string{"m4-high"}},
		{"ignored", PickFilter{Verdict: &ignored}, []string{"m3-low"}},
	}
	if err := s.SetVerdict(ctx, media.Movies, 3, VerdictIgnored, nil); err != nil {
		t.Fatal(err)
	}
	// Same tmdb id under the other kind must not leak across kinds.
	if err := s.SetVerdict(ctx, media.Series, 4, VerdictIgnored, nil); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListPicks(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(titles(got), tt.want) {
				t.Errorf("got %v, want %v", titles(got), tt.want)
			}
		})
	}
	_ = run2
}

// storedRun creates a run and finishes it with the given picks and error.
func storedRun(t *testing.T, s *SQLite, r Run, runErr error, picks ...pipeline.Pick) int64 {
	t.Helper()
	id, err := s.CreateRun(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, id, &pipeline.Run{Kind: r.Kind, Picks: picks}, runErr); err != nil {
		t.Fatal(err)
	}
	return id
}

type pickCase struct {
	name  string
	f     PickFilter
	want  []string
	total int
}

func checkPickCases(t *testing.T, s *SQLite, cases []pickCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListPicks(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(titles(got), tc.want) {
				t.Errorf("got %v, want %v", titles(got), tc.want)
			}
			if n, err := s.CountPicks(ctx, tc.f); err != nil || n != tc.total {
				t.Errorf("CountPicks = %d, %v, want %d", n, err, tc.total)
			}
		})
	}
}

func TestListPicksCollection(t *testing.T) {
	s, _ := openTest(t)
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	taste1 := storedRun(t, s, Run{Kind: media.Movies, UseTaste: true, StartedAt: t0}, nil, pick(1, 90, "a1"), pick(2, 80, "b1"))
	search1 := storedRun(t, s, Run{Kind: media.Movies, Vibe: "heists", StartedAt: t0.Add(day)}, nil, pick(1, 70, "a2"), pick(3, 60, "c2"))
	storedRun(t, s, Run{Kind: media.Movies, Vibe: "failed search", StartedAt: t0.Add(2 * day)}, errors.New("boom"), pick(4, 99, "d3"))
	storedRun(t, s, Run{Kind: media.Series, Vibe: "korean thrillers", StartedAt: t0.Add(3 * day)}, nil, pick(1, 50, "e4"))
	storedRun(t, s, Run{Kind: media.Movies, UseTaste: true, StartedAt: t0.Add(4 * day)}, nil, pick(3, 30, "c5a"), pick(3, 20, "c5b"), pick(2, 10, "b5"))

	yes, no := true, false
	all := []string{"c5a", "c5b", "b5", "e4", "a2", "c2", "a1", "b1"}
	checkPickCases(t, s, []pickCase{
		{"all succeeded runs", PickFilter{AllRuns: true}, all, 8},
		{"all overrides run id", PickFilter{AllRuns: true, RunID: search1}, all, 8},
		{"latest overrides all", PickFilter{Kind: media.Movies, LatestRun: true, AllRuns: true}, []string{"c5a", "c5b", "b5"}, 3},
		{"open searches", PickFilter{AllRuns: true, Search: &yes}, []string{"e4", "a2", "c2"}, 3},
		{"open searches of any status", PickFilter{Search: &yes}, []string{"e4", "d3", "a2", "c2"}, 4},
		{"taste runs", PickFilter{AllRuns: true, Search: &no}, []string{"c5a", "c5b", "b5", "a1", "b1"}, 5},
		{"distinct keeps the most recent", PickFilter{AllRuns: true, Distinct: true}, []string{"c5b", "b5", "e4", "a2"}, 4},
		{"distinct applies after the filters", PickFilter{AllRuns: true, Search: &yes, Distinct: true}, []string{"e4", "a2", "c2"}, 3},
		{"distinct per kind", PickFilter{Kind: media.Series, AllRuns: true, Distinct: true}, []string{"e4"}, 1},
		{"limit and offset", PickFilter{AllRuns: true, Limit: 3, Offset: 2}, []string{"b5", "e4", "a2"}, 8},
		{"offset past the end", PickFilter{AllRuns: true, Offset: 8}, []string{}, 8},
		{"negative offset", PickFilter{AllRuns: true, Limit: 1, Offset: -3}, []string{"c5a"}, 8},
		{"limit above the maximum", PickFilter{AllRuns: true, Limit: 5000}, all, 8},
	})

	checkRunFields := func(where string, p Pick, vibe string, useTaste bool, found time.Time) {
		t.Helper()
		if p.RunVibe != vibe || p.RunUseTaste != useTaste || !p.FoundAt.Equal(found) {
			t.Errorf("%s: %s run_vibe %q use_taste %v found_at %v", where, p.Title, p.RunVibe, p.RunUseTaste, p.FoundAt)
		}
	}
	_, searchPicks, err := s.GetRun(ctx, search1)
	if err != nil || len(searchPicks) != 2 {
		t.Fatalf("GetRun = %+v, %v", searchPicks, err)
	}
	checkRunFields("GetRun", searchPicks[0], "heists", false, t0.Add(day))
	p, err := s.GetPick(ctx, searchPicks[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	checkRunFields("GetPick", p, "heists", false, t0.Add(day))
	listed, err := s.ListPicks(ctx, PickFilter{RunID: taste1})
	if err != nil || len(listed) != 2 {
		t.Fatalf("ListPicks = %+v, %v", listed, err)
	}
	checkRunFields("ListPicks", listed[0], "", true, t0)

	for _, tc := range []struct {
		p    Pick
		want map[string]any
	}{
		{listed[0], map[string]any{"run_use_taste": true, "found_at": "2026-09-01T10:00:00Z"}},
		{p, map[string]any{"run_vibe": "heists", "run_use_taste": false, "found_at": "2026-09-02T10:00:00Z"}},
	} {
		b, err := json.Marshal(tc.p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for k, v := range tc.want {
			if m[k] != v {
				t.Errorf("%s: %s = %v, want %v", tc.p.Title, k, m[k], v)
			}
		}
		if _, ok := m["run_vibe"]; ok != (tc.p.RunVibe != "") {
			t.Errorf("%s: run_vibe present = %v", tc.p.Title, ok)
		}
	}
}

func TestListPicksAdded(t *testing.T) {
	s, _ := openTest(t)
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	run1 := storedRun(t, s, Run{Kind: media.Movies, UseTaste: true, StartedAt: t0}, nil,
		pick(10, 90, "added early"), pick(11, 80, "added late"), pick(12, 70, "failed"), pick(13, 60, "added then failed"))
	run2 := storedRun(t, s, Run{Kind: media.Movies, UseTaste: true, StartedAt: t0.Add(time.Hour)}, nil,
		pick(10, 50, "picked again"), pick(14, 40, "failed then added"))
	storedRun(t, s, Run{Kind: media.Series, UseTaste: true, StartedAt: t0.Add(2 * time.Hour)}, nil, pick(20, 90, "series unrequested"))

	ids := map[string]int64{}
	for _, run := range []int64{run1, run2} {
		_, ps, err := s.GetRun(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			ids[p.Title] = p.ID
		}
	}
	for _, r := range []Request{
		{PickID: ids["added early"], Status: "added", RequestedAt: t0.Add(1 * time.Minute)},
		{PickID: ids["added late"], Status: "added", RequestedAt: t0.Add(3 * time.Minute)},
		{PickID: ids["failed"], Status: "failed", Error: "timeout", RequestedAt: t0.Add(2 * time.Minute)},
		{PickID: ids["added then failed"], Status: "added", RequestedAt: t0},
		{PickID: ids["added then failed"], Status: "failed", RequestedAt: t0.Add(4 * time.Minute)},
		{PickID: ids["failed then added"], Status: "failed", RequestedAt: t0},
		{PickID: ids["failed then added"], Status: "added", RequestedAt: t0.Add(2 * time.Minute)},
	} {
		r.App = "radarr"
		if err := s.RecordRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	added := []string{"added late", "failed then added", "added early"}
	checkPickCases(t, s, []pickCase{
		{"newest request first", PickFilter{AllRuns: true, Added: true}, added, 3},
		{"distinct keeps the added pick", PickFilter{AllRuns: true, Added: true, Distinct: true}, added, 3},
		{"limit and offset", PickFilter{AllRuns: true, Added: true, Limit: 1, Offset: 1}, []string{"failed then added"}, 3},
		{"latest run only", PickFilter{Kind: media.Movies, LatestRun: true, Added: true}, []string{"failed then added"}, 1},
		{"series", PickFilter{Kind: media.Series, AllRuns: true, Added: true}, []string{}, 0},
	})
}

// Titles that are in the library now are never shown as picks, unless
// Proposarr added them itself (they belong under Added).
func TestListPicksExcludesLibrary(t *testing.T) {
	s, _ := openTest(t)
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	run := storedRun(t, s, Run{Kind: media.Movies, StartedAt: t0}, nil,
		pick(10, 90, "added in radarr directly"), pick(11, 80, "not in library"), pick(12, 70, "added through proposarr"))
	_, ps, err := s.GetRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Title == "added through proposarr" {
			if err := s.RecordRequest(ctx, Request{PickID: p.ID, App: "radarr", Status: "added", RequestedAt: t0}); err != nil {
				t.Fatal(err)
			}
		}
	}

	owned := []int{10, 12}
	checkPickCases(t, s, []pickCase{
		{"no library filter", PickFilter{AllRuns: true}, []string{"added in radarr directly", "not in library", "added through proposarr"}, 3},
		{"library titles left out", PickFilter{AllRuns: true, ExcludeTMDB: owned}, []string{"not in library", "added through proposarr"}, 2},
		{"added tab keeps own adds", PickFilter{AllRuns: true, Added: true, ExcludeTMDB: owned}, []string{"added through proposarr"}, 1},
		{"undecided hides owned", PickFilter{LatestRun: true, Verdict: new(Verdict), ExcludeTMDB: owned}, []string{"not in library", "added through proposarr"}, 2},
	})
}

func TestSetVerdict(t *testing.T) {
	s, _ := openTest(t)
	runID := finishedRun(t, s, media.Movies, pick(10, 50, "t"))
	get := func() Pick {
		t.Helper()
		ps, err := s.ListPicks(ctx, PickFilter{RunID: runID})
		if err != nil || len(ps) != 1 {
			t.Fatalf("ListPicks = %v, %v", ps, err)
		}
		return ps[0]
	}

	if err := s.SetVerdict(ctx, media.Movies, 10, VerdictIgnored, nil); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Verdict != VerdictIgnored || p.VerdictAt == nil || p.LaterUntil != nil {
		t.Errorf("ignored = %+v", p)
	}

	until := time.Date(2030, 1, 2, 3, 4, 5, 6, time.UTC)
	if err := s.SetVerdict(ctx, media.Movies, 10, VerdictLater, &until); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Verdict != VerdictLater || p.LaterUntil == nil || !p.LaterUntil.Equal(until) {
		t.Errorf("later = %+v", p)
	}

	if err := s.SetVerdict(ctx, media.Movies, 10, VerdictAccepted, &until); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Verdict != VerdictAccepted || p.LaterUntil != nil {
		t.Errorf("accepted must drop until: %+v", p)
	}

	if err := s.SetVerdict(ctx, media.Movies, 10, VerdictNone, nil); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.Verdict != VerdictNone || p.VerdictAt != nil {
		t.Errorf("cleared = %+v", p)
	}

	if err := s.SetVerdict(ctx, media.Movies, 10, Verdict("maybe"), nil); err == nil {
		t.Error("unknown verdict accepted")
	}
}

func TestExcludedAndRequests(t *testing.T) {
	s, _ := openTest(t)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	runID := finishedRun(t, s, media.Movies, pick(20, 90, "requested"), pick(21, 80, "failed request"), pick(22, 70, "plain"))
	finishedRun(t, s, media.Series, pick(30, 90, "series added"))

	future, past := now.Add(24*time.Hour), now.Add(-time.Hour)
	for _, v := range []struct {
		kind  media.Kind
		id    int
		v     Verdict
		until *time.Time
	}{
		{media.Movies, 1, VerdictAccepted, nil},
		{media.Movies, 2, VerdictIgnored, nil},
		{media.Movies, 3, VerdictLater, &future},
		{media.Movies, 4, VerdictLater, &past},
		{media.Movies, 5, VerdictLater, nil},
		{media.Series, 6, VerdictIgnored, nil},
	} {
		if err := s.SetVerdict(ctx, v.kind, v.id, v.v, v.until); err != nil {
			t.Fatal(err)
		}
	}

	picks, _ := s.ListPicks(ctx, PickFilter{RunID: runID})
	byTitle := map[string]Pick{}
	for _, p := range picks {
		byTitle[p.Title] = p
	}
	first := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	reqs := []Request{
		{PickID: byTitle["requested"].ID, App: "radarr", Status: "failed", Error: "timeout", RequestedAt: first},
		{PickID: byTitle["requested"].ID, App: "radarr", TargetID: 77, QualityProfile: "HD Bluray + WEB", RootFolder: "/media/movies", Status: "added"},
		{PickID: byTitle["failed request"].ID, App: "radarr", QualityProfile: "HD", Status: "failed", Error: "400"},
	}
	for _, r := range reqs {
		if err := s.RecordRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	seriesPicks, _ := s.ListPicks(ctx, PickFilter{Kind: media.Series})
	if err := s.RecordRequest(ctx, Request{PickID: seriesPicks[0].ID, App: "sonarr", Status: "added"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Excluded(ctx, media.Movies)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]bool{1: true, 2: true, 3: true, 5: true, 20: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Excluded(movies) = %v, want %v", got, want)
	}
	if got, _ := s.Excluded(ctx, media.Series); !reflect.DeepEqual(got, map[int]bool{6: true, 30: true}) {
		t.Errorf("Excluded(series) = %v", got)
	}

	p, err := s.GetPick(ctx, byTitle["requested"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Request == nil || p.Request.Status != "added" || p.Request.TargetID != 77 ||
		p.Request.QualityProfile != "HD Bluray + WEB" || p.Request.RootFolder != "/media/movies" || !p.Request.RequestedAt.Equal(now) {
		t.Errorf("latest request = %+v", p.Request)
	}
	if err := s.RecordRequest(ctx, Request{PickID: 9999, App: "radarr", Status: "added"}); err == nil {
		t.Error("request for a missing pick must fail the foreign key")
	}
}

func TestNotFoundAndLatestProfile(t *testing.T) {
	s, _ := openTest(t)
	if _, _, err := s.GetRun(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRun = %v", err)
	}
	if _, err := s.GetPick(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPick = %v", err)
	}
	if p, err := s.LatestProfile(ctx, media.Movies); p != nil || err != nil {
		t.Errorf("LatestProfile empty = %v, %v", p, err)
	}

	finishedRun(t, s, media.Movies)
	failed, _ := s.CreateRun(ctx, Run{Kind: media.Movies})
	s.FinishRun(ctx, failed, &pipeline.Run{Kind: media.Movies, Profile: profile.Profile{Kind: media.Movies,
		Top: []profile.Entry{{Title: "From failed run"}}}}, errors.New("x"))

	p, err := s.LatestProfile(ctx, media.Movies)
	if err != nil || p == nil || p.Top[0].Title != "Seed" {
		t.Errorf("LatestProfile = %+v, %v (must skip failed runs)", p, err)
	}
	if p, _ := s.LatestProfile(ctx, media.Series); p != nil {
		t.Errorf("LatestProfile series = %+v", p)
	}
}

func TestListRunsOrderAndLimit(t *testing.T) {
	s, _ := openTest(t)
	for i := 0; i < 3; i++ {
		finishedRun(t, s, media.Movies)
	}
	runs, err := s.ListRuns(ctx, 2)
	if err != nil || len(runs) != 2 || runs[0].ID < runs[1].ID {
		t.Errorf("ListRuns = %+v, %v", runs, err)
	}
}

func TestConcurrentVerdictsAndReads(t *testing.T) {
	s, _ := openTest(t)
	var picks []pipeline.Pick
	for i := 1; i <= 20; i++ {
		picks = append(picks, pick(i, i, fmt.Sprint("t", i)))
	}
	finishedRun(t, s, media.Movies, picks...)

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 1; i <= 20; i++ {
				v := []Verdict{VerdictAccepted, VerdictIgnored, VerdictNone}[(i+g)%3]
				if err := s.SetVerdict(ctx, media.Movies, i, v, nil); err != nil {
					errs <- err
				}
			}
		}(g)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := s.ListPicks(ctx, PickFilter{Kind: media.Movies}); err != nil {
					errs <- err
				}
				if _, err := s.Excluded(ctx, media.Movies); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
