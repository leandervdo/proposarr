package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

// fakeTitles serves TMDB details and ratings; id 404 is unknown, id 500 fails,
// and id 7 has no ratings.
type fakeTitles struct {
	titleCalls, ratingsCalls atomic.Int32

	mu      sync.Mutex
	regions []string
	kinds   []media.Kind
}

func (f *fakeTitles) title(_ context.Context, kind media.Kind, id int, region string) (tmdb.FullDetails, error) {
	f.titleCalls.Add(1)
	f.mu.Lock()
	f.regions = append(f.regions, region)
	f.kinds = append(f.kinds, kind)
	f.mu.Unlock()
	switch id {
	case 404:
		return tmdb.FullDetails{}, fmt.Errorf("GET /movie/404: %w", tmdb.ErrNotFound)
	case 500:
		return tmdb.FullDetails{}, errors.New("GET /movie/500: HTTP 500")
	}
	return tmdb.FullDetails{ID: id, Kind: kind, Title: "The Matrix", Year: 1999, Runtime: 136, TVDBID: 99,
		Trailer: &tmdb.Trailer{Name: "Trailer", YouTubeKey: "vKQi3bBA1y8"}, IMDBID: "tt0133093", Rating: 8.2, Votes: 26000}, nil
}

func (f *fakeTitles) ratings(_ context.Context, kind media.Kind, id int) (*pipeline.Ratings, error) {
	f.ratingsCalls.Add(1)
	if id == 7 {
		return nil, errors.New("radarr: movie tmdb:7 not found")
	}
	return &pipeline.Ratings{IMDB: &pipeline.IMDBRating{Value: 8.7, Votes: 2000000}, RottenTomatoes: 83}, nil
}

func (f *fakeTitles) lastRegion() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.regions[len(f.regions)-1]
}

func TestTitleDetails(t *testing.T) {
	titles := &fakeTitles{}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	env := newEnv(t, func(o *Options) {
		o.Now = func() time.Time { return now }
		o.Title = titles.title
		o.Ratings = titles.ratings
	})

	for _, target := range []string{"/api/titles/books/603", "/api/titles/movie/603", "/api/titles/movies/abc", "/api/titles/movies/0", "/api/titles/movies/-1"} {
		wantStatus(t, env.do(t, "GET", target, ""), http.StatusBadRequest)
	}
	if n := titles.titleCalls.Load(); n != 0 {
		t.Fatalf("TMDB called %d times for invalid requests", n)
	}

	rec := env.do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{
		`"tmdb_id":603`, `"kind":"movies"`, `"title":"The Matrix"`, `"year":1999`, `"runtime":136`,
		`"genres":[]`, `"directors":[]`, `"cast":[]`, `"streaming":[]`,
		`"trailer":{"name":"Trailer","youtube_key":"vKQi3bBA1y8"}`, `"imdb_id":"tt0133093"`, `"tmdb_rating":8.2`, `"tmdb_votes":26000`,
		`"ratings":{"imdb":{"value":8.7,"votes":2000000},"rotten_tomatoes":83}`, `"in_library":true`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	for _, unwanted := range []string{"tvdb", `"tagline"`, `"seasons"`, `"backdrop_url"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected %s in %s", unwanted, body)
		}
	}
	if got := titles.lastRegion(); got != "US" {
		t.Errorf("region = %q", got)
	}

	rec = env.do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != body || titles.titleCalls.Load() != 1 || titles.ratingsCalls.Load() != 1 {
		t.Errorf("cache miss: TMDB calls %d, ratings calls %d", titles.titleCalls.Load(), titles.ratingsCalls.Load())
	}
	now = now.Add(time.Hour)
	wantStatus(t, env.do(t, "GET", "/api/titles/movies/603", ""), http.StatusOK)
	if titles.titleCalls.Load() != 2 {
		t.Errorf("expired entry not refetched: TMDB calls %d", titles.titleCalls.Load())
	}

	rec = env.do(t, "GET", "/api/titles/movies/7", "")
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"ratings"`) || !strings.Contains(rec.Body.String(), `"in_library":false`) {
		t.Errorf("ratings failure / not in library: %s", rec.Body.String())
	}

	for i := 0; i < 2; i++ {
		wantStatus(t, env.do(t, "GET", "/api/titles/movies/404", ""), http.StatusNotFound)
	}
	wantStatus(t, env.do(t, "GET", "/api/titles/movies/500", ""), http.StatusBadGateway)
	if n := titles.titleCalls.Load(); n != 6 {
		t.Errorf("errors must not be cached: TMDB calls %d, want 6", n)
	}

	rec = env.do(t, "GET", "/api/titles/series/1396", "")
	wantStatus(t, rec, http.StatusOK)
	titles.mu.Lock()
	lastKind := titles.kinds[len(titles.kinds)-1]
	titles.mu.Unlock()
	if !strings.Contains(rec.Body.String(), `"kind":"series"`) || lastKind != media.Series {
		t.Errorf("series: kind %q body %s", lastKind, rec.Body.String())
	}
}

func TestTitleDetailsSettings(t *testing.T) {
	titles := &fakeTitles{}
	var (
		cfg        config.Config
		libraryErr error
	)
	env := newEnv(t, func(o *Options) {
		cfg = o.Config()
		cfg.TMDB.Region = "nl"
		o.Config = func() config.Config { return cfg }
		o.Title = titles.title
		o.Library = func(context.Context, media.Kind) ([]media.Title, error) {
			if libraryErr != nil {
				return nil, libraryErr
			}
			return []media.Title{{TMDBID: 603, Title: "The Matrix"}}, nil
		}
	})

	rec := env.do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusOK)
	if titles.lastRegion() != "NL" || !strings.Contains(rec.Body.String(), `"in_library":true`) || strings.Contains(rec.Body.String(), `"ratings"`) {
		t.Errorf("region %q body %s", titles.lastRegion(), rec.Body.String())
	}

	// A saved region is a different cache entry; in_library is not cached.
	cfg.TMDB.Region = "GB"
	libraryErr = errors.New("Radarr is not configured")
	rec = env.do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusOK)
	if titles.titleCalls.Load() != 2 || titles.lastRegion() != "GB" || !strings.Contains(rec.Body.String(), `"in_library":false`) {
		t.Errorf("calls %d region %q body %s", titles.titleCalls.Load(), titles.lastRegion(), rec.Body.String())
	}

	cfg.TMDB.APIKey = ""
	rec = env.do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "tmdb.api_key") {
		t.Errorf("not configured body = %s", rec.Body.String())
	}

	rec = newEnv(t, nil).do(t, "GET", "/api/titles/movies/603", "")
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestTitleCacheEvictsOldest(t *testing.T) {
	c := newTitleCache(time.Hour, 2)
	t0 := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	key := func(id int) titleKey { return titleKey{kind: media.Movies, id: id, region: "US"} }
	has := func(id int, at time.Time) bool {
		_, ok := c.get(key(id), at)
		return ok
	}

	c.put(key(1), titleEntry{at: t0})
	c.put(key(2), titleEntry{at: t0.Add(time.Minute)})
	c.put(key(2), titleEntry{at: t0.Add(2 * time.Minute)}) // replacing does not evict
	if !has(1, t0) || !has(2, t0) {
		t.Fatal("entries missing before the cache is full")
	}
	c.put(key(3), titleEntry{at: t0.Add(3 * time.Minute)})
	if has(1, t0) || !has(2, t0) || !has(3, t0) || len(c.entries) != 2 {
		t.Errorf("after eviction: 1=%v 2=%v 3=%v len=%d", has(1, t0), has(2, t0), has(3, t0), len(c.entries))
	}
	if has(3, t0.Add(3*time.Minute+time.Hour)) || !has(3, t0.Add(3*time.Minute+time.Hour-time.Second)) {
		t.Error("ttl not applied")
	}
	if titleCacheSize != 500 || titleCacheTTL != time.Hour {
		t.Error("cache defaults")
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c.put(key(g*1000+i), titleEntry{at: t0.Add(time.Duration(i) * time.Second)})
				c.get(key(i), t0)
			}
		}(g)
	}
	wg.Wait()
	if len(c.entries) != 2 {
		t.Errorf("len = %d after concurrent puts", len(c.entries))
	}
}
