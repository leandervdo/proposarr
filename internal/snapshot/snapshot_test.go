package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

type fakeMovies struct {
	titles []media.Title
	calls  int
}

func (f *fakeMovies) Movies(context.Context) ([]media.Title, error) {
	f.calls++
	return append([]media.Title(nil), f.titles...), nil
}

type fakeSeries struct{ titles []media.Title }

func (f *fakeSeries) Series(context.Context) ([]media.Title, error) {
	return append([]media.Title(nil), f.titles...), nil
}

type fakeFinder struct {
	mu    sync.Mutex
	ids   map[int]int
	calls int
}

func (f *fakeFinder) FindByTVDB(_ context.Context, tvdb int) (int, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if id, ok := f.ids[tvdb]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func TestCacheHitExpiryRefresh(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	src := &fakeMovies{titles: []media.Title{{TMDBID: 1, Title: "Arrival"}}}
	lib := &Library{Dir: dir, TTL: 6 * time.Hour, Radarr: src, Now: func() time.Time { return now }}
	ctx := context.Background()

	if _, err := lib.Titles(ctx, media.Movies); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cache", "library-movies.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %v", info.Mode().Perm())
	}
	if d, _ := os.Stat(filepath.Dir(path)); d.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %v", d.Mode().Perm())
	}

	src.titles = append(src.titles, media.Title{TMDBID: 2, Title: "Dune"})
	now = now.Add(5 * time.Hour)
	got, _ := lib.Titles(ctx, media.Movies)
	if src.calls != 1 || len(got) != 1 {
		t.Fatalf("expected cache hit: calls=%d titles=%v", src.calls, got)
	}

	now = now.Add(2 * time.Hour)
	got, _ = lib.Titles(ctx, media.Movies)
	if src.calls != 2 || len(got) != 2 {
		t.Fatalf("expected refetch after ttl: calls=%d titles=%v", src.calls, got)
	}

	lib.Refresh = true
	if _, err := lib.Titles(ctx, media.Movies); err != nil || src.calls != 3 {
		t.Fatalf("expected refetch on refresh: calls=%d err=%v", src.calls, err)
	}
}

func TestCorruptCacheRefetches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "library-movies.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeMovies{titles: []media.Title{{TMDBID: 1, Title: "Arrival"}}}
	lib := &Library{Dir: dir, TTL: time.Hour, Radarr: src}
	got, err := lib.Titles(context.Background(), media.Movies)
	if err != nil || src.calls != 1 || len(got) != 1 {
		t.Fatalf("calls=%d got=%v err=%v", src.calls, got, err)
	}
	if c, ok := readCache(path); !ok || len(c.Titles) != 1 {
		t.Errorf("cache not rewritten: %+v %v", c, ok)
	}
}

// A fresh cache written by an older build (no version, no poster_url) must be
// refetched rather than served for the rest of its TTL.
func TestOldCacheVersionRefetches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "library-movies.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"fetched_at":"` + time.Now().Add(-time.Minute).Format(time.RFC3339Nano) + `","titles":[{"tmdb_id":1,"title":"Arrival"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeMovies{titles: []media.Title{{TMDBID: 1, Title: "Arrival", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg"}}}
	lib := &Library{Dir: dir, TTL: time.Hour, Radarr: src}
	got, err := lib.Titles(context.Background(), media.Movies)
	if err != nil || src.calls != 1 || len(got) != 1 || got[0].PosterURL == "" {
		t.Fatalf("calls=%d got=%+v err=%v", src.calls, got, err)
	}
	if c, ok := readCache(path); !ok || c.Version != cacheVersion {
		t.Errorf("cache not rewritten with version %d: %+v %v", cacheVersion, c, ok)
	}
}

func TestSeriesTVDBResolutionCached(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSeries{titles: []media.Title{
		{TVDBID: 100, Title: "Has mapping"},
		{TVDBID: 200, Title: "No mapping"},
		{TMDBID: 7, TVDBID: 300, Title: "Already known"},
	}}
	finder := &fakeFinder{ids: map[int]int{100: 1000}}
	lib := &Library{Dir: dir, TTL: time.Hour, Sonarr: src, TMDB: finder}

	got, err := lib.Titles(context.Background(), media.Series)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TMDBID != 1000 || got[1].TMDBID != 0 || got[2].TMDBID != 7 {
		t.Errorf("resolved = %+v", got)
	}
	if finder.calls != 2 {
		t.Errorf("finder calls = %d", finder.calls)
	}

	c, ok := readCache(filepath.Join(dir, "cache", "library-series.json"))
	if !ok || c.Titles[0].TMDBID != 1000 {
		t.Errorf("resolved id not cached: %+v", c)
	}
	if _, err := lib.Titles(context.Background(), media.Series); err != nil || finder.calls != 2 {
		t.Errorf("cache hit should not resolve again: calls=%d err=%v", finder.calls, err)
	}
}

func TestNotConfigured(t *testing.T) {
	lib := &Library{Dir: t.TempDir(), TTL: time.Hour}
	for _, k := range []media.Kind{media.Movies, media.Series} {
		_, err := lib.Titles(context.Background(), k)
		if err == nil || !strings.Contains(err.Error(), k.App()+" is not configured") {
			t.Errorf("%s: err = %v", k, err)
		}
	}
}

func TestNoDirSkipsCache(t *testing.T) {
	src := &fakeMovies{titles: []media.Title{{TMDBID: 1}}}
	lib := &Library{TTL: time.Hour, Radarr: src}
	lib.Titles(context.Background(), media.Movies)
	lib.Titles(context.Background(), media.Movies)
	if src.calls != 2 {
		t.Errorf("calls = %d", src.calls)
	}
}
