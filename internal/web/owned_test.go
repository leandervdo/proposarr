package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/store"
)

// fakeRadarr is a Radarr library read live. It records how many movies were read at once.
type fakeRadarr struct {
	mu        sync.Mutex
	movies    map[int]arr.Movie       // by TMDB id
	queue     map[int][]arr.QueueItem // by Radarr id
	errs      map[int]error           // by TMDB id
	queueErrs map[int]error           // by Radarr id
	active    int
	maxActive int
}

func newRadarr() *fakeRadarr {
	return &fakeRadarr{
		movies: map[int]arr.Movie{
			603:   {ID: 1, TMDBID: 603, Title: "The Matrix", Year: 1999, QualityProfileID: 4, Monitored: true, Available: true, HasFile: true, SizeOnDisk: 9_000_000_000, FileQuality: "Bluray-1080p"},
			9693:  {ID: 250, TMDBID: 9693, Title: "Children of Men", Year: 2006, QualityProfileID: 5, Monitored: true, Available: true},
			27205: {ID: 3, TMDBID: 27205, Title: "Inception", Year: 2010, QualityProfileID: 4},
			11:    {ID: 4, TMDBID: 11, Title: "Star Wars", Year: 1977, QualityProfileID: 4, Available: true},
			78:    {ID: 5, TMDBID: 78, Title: "Blade Runner", Year: 1982, QualityProfileID: 99, Monitored: true, Available: true},
			949:   {ID: 6, TMDBID: 949, Title: "Heat", Year: 1995, QualityProfileID: 4, Monitored: true, Available: true},
		},
		queue:     map[int][]arr.QueueItem{250: {{Status: "downloading", Title: "Children of Men 2006 BluRay 1080p REMUX", Quality: "Remux-1080p", Size: 400, SizeLeft: 100}}},
		errs:      map[int]error{2: errors.New("radarr: GET /api/v3/movie: HTTP 500")},
		queueErrs: map[int]error{6: errors.New("radarr: GET /api/v3/queue: HTTP 503")},
	}
}

func (f *fakeRadarr) MovieByTMDB(_ context.Context, tmdbID int) (arr.Movie, error) {
	f.mu.Lock()
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	f.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active--
	if err := f.errs[tmdbID]; err != nil {
		return arr.Movie{}, err
	}
	m, ok := f.movies[tmdbID]
	if !ok {
		return arr.Movie{}, arr.ErrNotFound
	}
	return m, nil
}

func (f *fakeRadarr) Queue(_ context.Context, movieID int) ([]arr.QueueItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queue[movieID], f.queueErrs[movieID]
}

func (f *fakeRadarr) QualityProfiles(context.Context) ([]arr.QualityProfile, error) {
	return testCatalog.profiles, nil
}

func withRadarr(radarr *fakeRadarr) func(o *Options) {
	return func(o *Options) { o.Radarr = func() RadarrLibrary { return radarr } }
}

func searchCheck(t *testing.T, o OwnedTitle) request.ReleaseCheck {
	t.Helper()
	if o.Search == nil {
		t.Fatalf("%d has no search: %+v", o.TMDBID, o)
	}
	var c request.ReleaseCheck
	if err := json.Unmarshal(o.Search.Release, &c); err != nil {
		t.Fatalf("release %s: %v", o.Search.Release, err)
	}
	return c
}

func nextEvent(t *testing.T, events <-chan event) event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return event{}
	}
}

func TestRunOwned(t *testing.T) {
	radarr := newRadarr()
	env := newEnv(t, withRadarr(radarr))
	env.store.addRun(store.Run{ID: 1, Kind: media.Movies, Status: store.RunSucceeded, Owned: []pipeline.OwnedMatch{
		{TMDBID: 603, Title: "The Matrix", Year: 1999},
		{TMDBID: 9693, Title: "Children of Men", Year: 2006},
		{TMDBID: 27205, Title: "Inception", Year: 2010},
		{TMDBID: 11, Title: "Star Wars", Year: 1977},
		{TMDBID: 78, Title: "Blade Runner", Year: 1982},
		{TMDBID: 1, Title: "Gone", Year: 2001},
		{TMDBID: 2, Title: "Unreadable", Year: 2002},
		{TMDBID: 949, Title: "Heat", Year: 1995},
	}})
	env.store.addRun(store.Run{ID: 2, Kind: media.Series, Status: store.RunSucceeded, Owned: []pipeline.OwnedMatch{{TMDBID: 95396, Title: "Severance", Year: 2022}}})
	env.store.addRun(store.Run{ID: 3, Kind: media.Movies, Status: store.RunSucceeded})
	requested := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	if err := env.store.RecordLibrarySearch(context.Background(), store.LibrarySearch{Kind: media.Movies, TMDBID: 9693, TargetID: 250,
		QualityProfile: "HD-1080p", RequestedAt: requested, Release: releaseJSON(*waitingCheck())}); err != nil {
		t.Fatal(err)
	}

	rec := env.do(t, "GET", "/api/runs/1/owned", "")
	wantStatus(t, rec, http.StatusOK)
	got := decode[[]OwnedTitle](t, rec)
	type row struct {
		id          int
		status, err string
	}
	var rows []row
	for _, o := range got {
		rows = append(rows, row{o.TMDBID, o.Status, o.Error})
	}
	want := []row{
		{603, OwnedDownloaded, ""}, {9693, OwnedDownloading, ""}, {27205, OwnedUnreleased, ""}, {11, OwnedUnmonitored, ""}, {78, OwnedMissing, ""},
		{1, OwnedUnknown, "not in Radarr any more"},
		{2, OwnedUnknown, "radarr: GET /api/v3/movie: HTTP 500"},
		{949, OwnedUnknown, "read Radarr's queue: radarr: GET /api/v3/queue: HTTP 503"},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Fatalf("owned = %v\nwant    %v", rows, want)
	}
	if radarr.maxActive > ownedConcurrency {
		t.Errorf("%d movies read at once, want at most %d", radarr.maxActive, ownedConcurrency)
	}

	matrix := got[0]
	wantMatrix := RadarrMovie{ID: 1, Monitored: true, HasFile: true, Available: true, QualityProfileID: 4, QualityProfile: "HD-1080p", FileQuality: "Bluray-1080p", SizeOnDisk: 9_000_000_000}
	if matrix.Kind != media.Movies || matrix.Title != "The Matrix" || matrix.Year != 1999 || matrix.PosterURL != "https://img/p.jpg" || matrix.Radarr == nil || *matrix.Radarr != wantMatrix || matrix.Search != nil {
		t.Errorf("matrix = %+v, radarr %+v", matrix, matrix.Radarr)
	}
	children := got[1]
	if q := children.Radarr.Queue; q == nil || q.Status != "downloading" || q.Progress == nil || *q.Progress != 75 || q.Quality != "Remux-1080p" || q.Title == "" {
		t.Errorf("queue = %+v", q)
	}
	if c := searchCheck(t, children); children.Search.QualityProfile != "HD-1080p" || !children.Search.RequestedAt.Equal(requested) || c.Status != request.CheckWaiting || len(c.Alternatives) != 1 {
		t.Errorf("search = %+v, release %+v", children.Search, c)
	}
	if got[4].Radarr.QualityProfile != "" || got[5].Radarr != nil || got[7].Radarr == nil {
		t.Errorf("unknown profile %+v, gone %+v, queue failure %+v", got[4].Radarr, got[5].Radarr, got[7].Radarr)
	}
	body := rec.Body.String()
	for _, s := range []string{`"search":{"quality_profile":"HD-1080p","requested_at":"2026-09-12T10:00:00Z","release":{"status":"waiting"`, `"progress":75`} {
		if !strings.Contains(body, s) {
			t.Errorf("body misses %s: %s", s, body)
		}
	}
	if strings.Contains(body, "target_id") {
		t.Errorf("search leaks its key fields: %s", body)
	}

	rec = env.do(t, "GET", "/api/runs/2/owned", "")
	wantStatus(t, rec, http.StatusOK)
	if series := decode[[]OwnedTitle](t, rec); len(series) != 1 || series[0].Status != OwnedInLibrary || series[0].Kind != media.Series || series[0].Radarr != nil || series[0].Error != "" {
		t.Errorf("series = %+v", series)
	}
	rec = env.do(t, "GET", "/api/runs/3/owned", "")
	wantStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("a run without owned matches = %s", rec.Body.String())
	}
	wantStatus(t, env.do(t, "GET", "/api/runs/999/owned", ""), http.StatusNotFound)
	wantStatus(t, env.do(t, "GET", "/api/runs/abc/owned", ""), http.StatusBadRequest)

	noRadarr := newEnv(t, nil)
	noRadarr.store.addRun(store.Run{ID: 1, Kind: media.Movies, Owned: []pipeline.OwnedMatch{{TMDBID: 603, Title: "The Matrix", Year: 1999}}})
	rec = noRadarr.do(t, "GET", "/api/runs/1/owned", "")
	wantStatus(t, rec, http.StatusOK)
	if o := decode[[]OwnedTitle](t, rec); len(o) != 1 || o[0].Status != OwnedUnknown || o[0].Error != "Radarr is not configured" || o[0].PosterURL != "https://img/p.jpg" {
		t.Errorf("without Radarr = %+v", o)
	}
}

func TestRunOwnedInRunJSON(t *testing.T) {
	env := newEnv(t, nil)
	env.store.addRun(store.Run{ID: 1, Kind: media.Movies, Status: store.RunSucceeded})
	for _, target := range []string{"/api/runs/1", "/api/runs"} {
		rec := env.do(t, "GET", target, "")
		wantStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"owned":[]`) {
			t.Errorf("%s: owned should be an empty array: %s", target, rec.Body.String())
		}
	}
}

func TestSearchLibraryMovie(t *testing.T) {
	env := newEnv(t, withRadarr(newRadarr()))
	events, unsubscribe := env.srv.events.subscribe()
	defer unsubscribe()

	for _, tc := range []struct{ target, body string }{
		{"/api/library/movies/abc/search", `{"quality_profile_id":4}`},
		{"/api/library/movies/0/search", `{"quality_profile_id":4}`},
		{"/api/library/movies/9693/search", `{}`},
		{"/api/library/movies/9693/search", `{"quality_profile_id":4,"if_nothing_fits":"grab"}`},
		{"/api/library/movies/9693/search", `not json`},
	} {
		wantStatus(t, env.do(t, "POST", tc.target, tc.body), http.StatusBadRequest)
	}
	if len(env.adder.searched) != 0 {
		t.Fatalf("searched without a valid request: %v", env.adder.searched)
	}

	grabbed := request.Checking("HD-1080p")
	grabbed.Status, grabbed.SwitchedFrom = request.CheckGrabbed, "Ultra-HD"
	grabbed.Release = &request.ReleaseInfo{Title: "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", Quality: "Remux-1080p"}
	env.adder.release, env.adder.block = &grabbed, make(chan struct{})
	rec := env.do(t, "POST", "/api/library/movies/9693/search", `{"quality_profile_id":5,"if_nothing_fits":"switch"}`)
	wantStatus(t, rec, http.StatusOK)
	started := decode[OwnedTitle](t, rec)
	if started.TMDBID != 9693 || started.Kind != media.Movies || started.Title != "Children of Men" || started.Year != 2006 || started.Status != OwnedDownloading || started.Radarr == nil || started.Radarr.QualityProfile != "Ultra-HD" {
		t.Fatalf("response = %+v", started)
	}
	if c := searchCheck(t, started); c.Status != request.CheckChecking || c.Profile != "Ultra-HD" || started.Search.QualityProfile != "Ultra-HD" {
		t.Errorf("search = %+v, release %+v", started.Search, c)
	}
	if env.adder.fallback != request.FallbackSwitch || fmt.Sprint(env.adder.searched) != "[[9693 5]]" {
		t.Errorf("fallback %q, searched %v", env.adder.fallback, env.adder.searched)
	}
	if ev := nextEvent(t, events); ev.name != "owned.updated" || !strings.Contains(string(ev.data), `"status":"checking"`) {
		t.Errorf("event %s: %s", ev.name, ev.data)
	}

	rec = env.do(t, "POST", "/api/library/movies/9693/search", `{"quality_profile_id":4}`)
	wantStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "still searching") || len(env.adder.searched) != 1 {
		t.Errorf("body %s, searched %v", rec.Body.String(), env.adder.searched)
	}
	close(env.adder.block)
	env.adder.block = nil

	ev := nextEvent(t, events)
	var done OwnedTitle
	if err := json.Unmarshal(ev.data, &done); err != nil || ev.name != "owned.updated" {
		t.Fatalf("event %s: %s (%v)", ev.name, ev.data, err)
	}
	if c := searchCheck(t, done); c.Status != request.CheckGrabbed || c.SwitchedFrom != "Ultra-HD" || done.Search.QualityProfile != "HD-1080p" || !done.Search.RequestedAt.Equal(started.Search.RequestedAt) {
		t.Errorf("finished search = %+v, release %+v", done.Search, c)
	}
	// Once the outcome is stored, the movie can be searched again.
	wantStatus(t, env.do(t, "POST", "/api/library/movies/9693/search", `{"quality_profile_id":4}`), http.StatusOK)

	cases := []struct {
		name string
		err  error
		body string
		code int
	}{
		{name: "not in Radarr", err: fmt.Errorf("movie tmdb:603: %w", request.ErrNotInLibrary), body: `{"quality_profile_id":4}`, code: http.StatusNotFound},
		{name: "has a file", err: fmt.Errorf("The Matrix (1999): %w", request.ErrHasFile), body: `{"quality_profile_id":4}`, code: http.StatusConflict},
		{name: "unknown profile", body: `{"quality_profile_id":77}`, code: http.StatusBadRequest},
		{name: "not configured", err: fmt.Errorf("Radarr: %w", request.ErrNotConfigured), body: `{"quality_profile_id":4}`, code: http.StatusBadRequest},
		{name: "Radarr down", err: errors.New("radarr: PUT /api/v3/movie/editor: HTTP 500"), body: `{"quality_profile_id":4}`, code: http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env.adder.searchErr = tc.err
			wantStatus(t, env.do(t, "POST", "/api/library/movies/603/search", tc.body), tc.code)
			env.store.mu.Lock()
			_, recorded := env.store.searches[603]
			env.store.mu.Unlock()
			env.srv.mu.Lock()
			claimed := env.srv.searching[603]
			env.srv.mu.Unlock()
			if recorded || claimed {
				t.Errorf("a failed search was recorded %v, still claimed %v", recorded, claimed)
			}
		})
	}
}
