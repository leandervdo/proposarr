package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
)

func addedPick(t *testing.T, s *SQLite) Pick {
	t.Helper()
	id, err := s.CreateRun(ctx, Run{Kind: media.Movies, UseTaste: true})
	if err != nil {
		t.Fatal(err)
	}
	pk := pipeline.Pick{TMDBID: 9693, Kind: media.Movies, Title: "Children of Men", Year: 2006, Score: 90, Source: "candidate",
		RelatedTo: []string{}, Genres: []string{}, Streaming: []string{}}
	if err := s.FinishRun(ctx, id, &pipeline.Run{Kind: media.Movies, Picks: []pipeline.Pick{pk}}, nil); err != nil {
		t.Fatal(err)
	}
	_, picks, err := s.GetRun(ctx, id)
	if err != nil || len(picks) != 1 {
		t.Fatalf("picks = %+v, %v", picks, err)
	}
	return picks[0]
}

func releaseStatus(t *testing.T, raw json.RawMessage) (status, profile string) {
	t.Helper()
	var c struct{ Status, Profile string }
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("release %s: %v", raw, err)
	}
	return c.Status, c.Profile
}

func TestRequestRelease(t *testing.T) {
	s, path := openTest(t)
	p := addedPick(t, s)

	checking := json.RawMessage(`{"status":"checking","profile":"Remux + WEB 2160p"}`)
	if err := s.RecordRequest(ctx, Request{PickID: p.ID, App: "radarr", TargetID: 250, QualityProfile: "Remux + WEB 2160p", Status: "added", Release: checking}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPick(ctx, p.ID)
	if err != nil || got.Request == nil || string(got.Request.Release) != string(checking) {
		t.Fatalf("request = %+v, %v", got.Request, err)
	}

	grabbed := json.RawMessage(`{"status":"grabbed","profile":"Remux + WEB 1080p","switched_from":"Remux + WEB 2160p"}`)
	if err := s.UpdateRequestRelease(ctx, p.ID, "Remux + WEB 1080p", grabbed); err != nil {
		t.Fatal(err)
	}
	picks, err := s.ListPicks(ctx, PickFilter{AllRuns: true, Added: true})
	if err != nil || len(picks) != 1 {
		t.Fatalf("added picks = %+v, %v", picks, err)
	}
	if r := picks[0].Request; r.QualityProfile != "Remux + WEB 1080p" || string(r.Release) != string(grabbed) || r.TargetID != 250 {
		t.Errorf("updated request = %+v", r)
	}
	if err := s.UpdateRequestRelease(ctx, 9999, "HD", grabbed); !errors.Is(err, ErrNotFound) {
		t.Errorf("update without a request: %v", err)
	}

	// A Sonarr request has no release.
	if err := s.RecordRequest(ctx, Request{PickID: p.ID, App: "sonarr", Status: "added"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPick(ctx, p.ID); got.Request.Release != nil {
		t.Errorf("release = %s, want none", got.Request.Release)
	}

	// A check the stopped server was still following is reported as searching.
	if err := s.UpdateRequestRelease(ctx, p.ID, "Remux + WEB 2160p", checking); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err = s2.GetPick(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status, profile := releaseStatus(t, got.Request.Release); status != "searching" || profile != "Remux + WEB 2160p" {
		t.Errorf("after restart: status %q, profile %q", status, profile)
	}
}

func TestRunOwned(t *testing.T) {
	s, _ := openTest(t)
	id, err := s.CreateRun(ctx, Run{Kind: media.Movies, Vibe: "fast and the furious movies"})
	if err != nil {
		t.Fatal(err)
	}
	if r, _, err := s.GetRun(ctx, id); err != nil || r.Owned == nil || len(r.Owned) != 0 {
		t.Fatalf("running run owned = %+v, %v", r.Owned, err)
	}
	owned := []pipeline.OwnedMatch{{TMDBID: 9799, Title: "The Fast and the Furious", Year: 2001}, {TMDBID: 584, Title: "2 Fast 2 Furious"}}
	if err := s.FinishRun(ctx, id, &pipeline.Run{Kind: media.Movies, OpenSearch: true, Owned: owned}, nil); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.GetRun(ctx, id)
	if err != nil || fmt.Sprint(r.Owned) != fmt.Sprint(owned) {
		t.Errorf("owned = %+v, %v", r.Owned, err)
	}
	runs, err := s.ListRuns(ctx, 1)
	if err != nil || len(runs) != 1 || fmt.Sprint(runs[0].Owned) != fmt.Sprint(owned) {
		t.Errorf("listed runs = %+v, %v", runs, err)
	}
	taste := finishedRun(t, s, media.Movies)
	if r, _, err := s.GetRun(ctx, taste); err != nil || r.Owned == nil || len(r.Owned) != 0 {
		t.Errorf("taste run owned = %+v, %v", r.Owned, err)
	}
}

func TestLibrarySearches(t *testing.T) {
	s, path := openTest(t)
	if got, err := s.LibrarySearches(ctx, media.Movies, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("searches for no ids = %v, %v", got, err)
	}
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	checking := json.RawMessage(`{"status":"checking","profile":"Remux + WEB 2160p"}`)
	if err := s.RecordLibrarySearch(ctx, LibrarySearch{Kind: media.Movies, TMDBID: 9693, TargetID: 250, QualityProfile: "Remux + WEB 2160p", RequestedAt: at, Release: checking}); err != nil {
		t.Fatal(err)
	}
	// The same id for the other kind is another title.
	if err := s.RecordLibrarySearch(ctx, LibrarySearch{Kind: media.Series, TMDBID: 9693, QualityProfile: "HD-1080p"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LibrarySearches(ctx, media.Movies, []int{9693, 603})
	if err != nil || len(got) != 1 {
		t.Fatalf("searches = %+v, %v", got, err)
	}
	if ls := got[9693]; ls.Kind != media.Movies || ls.TMDBID != 9693 || ls.TargetID != 250 || ls.QualityProfile != "Remux + WEB 2160p" || !ls.RequestedAt.Equal(at) || string(ls.Release) != string(checking) {
		t.Errorf("search = %+v", ls)
	}

	grabbed := json.RawMessage(`{"status":"grabbed","profile":"Remux + WEB 1080p","switched_from":"Remux + WEB 2160p"}`)
	if err := s.UpdateLibrarySearch(ctx, media.Movies, 9693, "Remux + WEB 1080p", grabbed); err != nil {
		t.Fatal(err)
	}
	got, _ = s.LibrarySearches(ctx, media.Movies, []int{9693})
	if ls := got[9693]; ls.QualityProfile != "Remux + WEB 1080p" || string(ls.Release) != string(grabbed) || !ls.RequestedAt.Equal(at) {
		t.Errorf("updated search = %+v", ls)
	}
	if series, _ := s.LibrarySearches(ctx, media.Series, []int{9693}); series[9693].QualityProfile != "HD-1080p" || series[9693].Release != nil {
		t.Errorf("series search = %+v", series[9693])
	}
	if err := s.UpdateLibrarySearch(ctx, media.Movies, 603, "HD-1080p", grabbed); !errors.Is(err, ErrNotFound) {
		t.Errorf("update without a search: %v", err)
	}

	// A new search replaces the previous one.
	later := at.Add(time.Hour)
	if err := s.RecordLibrarySearch(ctx, LibrarySearch{Kind: media.Movies, TMDBID: 9693, TargetID: 250, QualityProfile: "HD Bluray + WEB", RequestedAt: later, Release: checking}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM library_searches WHERE kind = 'movies'`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows = %d, %v", rows, err)
	}
	got, _ = s.LibrarySearches(ctx, media.Movies, []int{9693})
	if ls := got[9693]; ls.QualityProfile != "HD Bluray + WEB" || !ls.RequestedAt.Equal(later) || string(ls.Release) != string(checking) {
		t.Errorf("replaced search = %+v", ls)
	}

	// A check the stopped server was still following is reported as searching.
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err = s2.LibrarySearches(ctx, media.Movies, []int{9693})
	if err != nil {
		t.Fatal(err)
	}
	if status, profile := releaseStatus(t, got[9693].Release); status != "searching" || profile != "Remux + WEB 2160p" {
		t.Errorf("after restart: status %q, profile %q", status, profile)
	}
}

func TestMigrateFromV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proposarr.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{`CREATE TABLE schema_version (version INTEGER NOT NULL)`}
	for i, m := range migrations[:3] {
		stmts = append(stmts, m...)
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO schema_version (version) VALUES (%d)`, i+1))
	}
	stmts = append(stmts,
		`INSERT INTO runs (kind, vibe, status, started_at, finished_at) VALUES ('movies', '', 'succeeded', '2026-09-11T00:00:00.000000000Z', '2026-09-11T00:01:00.000000000Z')`,
		`INSERT INTO picks (run_id, tmdb_id, kind, title, score) VALUES (1, 9693, 'movies', 'Children of Men', 90)`,
		`INSERT INTO requests (pick_id, app, target_id, quality_profile, root_folder, status, requested_at) VALUES (1, 'radarr', 250, 'Remux + WEB 2160p', '/media/movies', 'added', '2026-09-11T17:19:47.000000000Z')`,
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
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("version = %d, %v", version, err)
	}
	if r, _, err := s.GetRun(ctx, 1); err != nil || r.Owned == nil || len(r.Owned) != 0 {
		t.Fatalf("a run stored before owned matches = %+v, %v", r.Owned, err)
	}
	p, err := s.GetPick(ctx, 1)
	if err != nil || p.Request == nil || p.Request.TargetID != 250 || p.Request.Release != nil {
		t.Fatalf("old request = %+v, %v", p.Request, err)
	}
	if err := s.UpdateRequestRelease(ctx, 1, "Remux + WEB 2160p", json.RawMessage(`{"status":"waiting","profile":"Remux + WEB 2160p"}`)); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetPick(ctx, 1); p.Request.Release == nil {
		t.Error("release not stored on an upgraded database")
	}
}
