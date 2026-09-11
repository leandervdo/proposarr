package history

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

type plexFixture struct {
	t        *testing.T
	history  func(r *http.Request) []map[string]any
	meta     map[string]map[string]any
	requests atomic.Int32
}

func (f *plexFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Plex-Token") != "tok" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/status/sessions/history/all":
		f.requests.Add(1)
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"Metadata": f.history(r)}})
	case strings.HasPrefix(r.URL.Path, plexMetaPrefix):
		if r.URL.Query().Get("includeGuids") != "1" {
			f.t.Errorf("metadata request without includeGuids: %s", r.URL)
		}
		m, ok := f.meta[strings.TrimPrefix(r.URL.Path, plexMetaPrefix)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"MediaContainer": map[string]any{"Metadata": []any{m}}})
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func byTitle(entries []Entry) map[string]Entry {
	m := map[string]Entry{}
	for _, e := range entries {
		m[e.Title] = e
	}
	return m
}

func TestPlexMovies(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour)
	recent := since.Add(24 * time.Hour).Unix()
	f := &plexFixture{t: t,
		history: func(r *http.Request) []map[string]any {
			q := r.URL.Query()
			if q.Has("type") || q.Get("sort") != "viewedAt:desc" || q.Get("viewedAt>") != strconv.FormatInt(since.Unix(), 10) {
				t.Errorf("unexpected query %s", r.URL.RawQuery)
			}
			return []map[string]any{
				{"ratingKey": "10", "title": "The Matrix", "year": 1999, "viewedAt": recent + 100},
				{"ratingKey": "10", "title": "The Matrix", "year": 1999, "viewedAt": recent},
				{"type": "movie", "ratingKey": 11, "title": "Gone", "originallyAvailableAt": "2001-05-04", "viewedAt": recent},
				// Plex returns episodes in the same history; they must not count as movies.
				{"type": "episode", "ratingKey": "2775", "title": "Blood Money", "grandparentTitle": "Breaking Bad", "grandparentKey": "/library/metadata/2713", "viewedAt": recent},
				{"ratingKey": "2776", "title": "Over", "grandparentTitle": "Breaking Bad", "viewedAt": recent},
				{"ratingKey": "12", "title": "Old", "year": 1980, "viewedAt": since.Add(-time.Hour).Unix()},
			}
		},
		meta: map[string]map[string]any{
			// Real Plex metadata carries both "guid" (string) and "Guid" (array).
			"10": {"title": "The Matrix", "year": 1999, "guid": "plex://movie/5d7768...", "Guid": []any{
				map[string]any{"id": "imdb://tt0133093"}, map[string]any{"id": "tmdb://603"},
			}},
		},
	}
	srv := httptest.NewServer(f)
	defer srv.Close()

	got, err := NewPlex(srv.URL, "tok", nil).History(context.Background(), media.Movies, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	m := byTitle(got)
	if e := m["The Matrix"]; e.TMDBID != 603 || e.Plays != 2 || e.Signal != Rewatched {
		t.Errorf("matrix = %+v", e)
	}
	if e := m["Gone"]; e.TMDBID != 0 || e.Plays != 1 || e.Signal != Watched || e.Year != 2001 {
		t.Errorf("unresolved movie = %+v", e)
	}
}

func TestPlexSeries(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour)
	at := since.Add(time.Hour).Unix()
	ep := func(show, key string, n int) map[string]any {
		title := map[string]string{"/library/metadata/100": "A", "/library/metadata/200": "B", "/library/metadata/300": "C", "/library/metadata/400": "D"}[show]
		return map[string]any{"ratingKey": key, "title": "ep", "grandparentKey": show, "grandparentTitle": title, "viewedAt": at + int64(n)}
	}
	var items []map[string]any
	items = append(items, ep("/library/metadata/100", "1001", 1), ep("/library/metadata/100", "1001", 2), ep("/library/metadata/100", "1002", 3))
	for i := range 6 {
		items = append(items, ep("/library/metadata/200", "20"+strconv.Itoa(i), 4))
	}
	items = append(items, ep("/library/metadata/300", "3001", 5))
	items = append(items, ep("/library/metadata/400", "4001", 6), ep("/library/metadata/400", "4002", 7))
	items = append(items, map[string]any{"ratingKey": "9001", "title": "ep", "grandparentTitle": "Orphan", "viewedAt": at})
	// Movies share the history endpoint and must be skipped for series.
	items = append(items, map[string]any{"type": "movie", "ratingKey": "785", "title": "The Fellowship of the Ring", "year": 2001, "viewedAt": at})

	f := &plexFixture{t: t,
		history: func(r *http.Request) []map[string]any {
			if r.URL.Query().Has("type") {
				t.Errorf("history must not filter by type server-side: %q", r.URL.RawQuery)
			}
			return items
		},
		meta: map[string]map[string]any{
			"100": {"title": "Game of Thrones", "year": 2011, "leafCount": 73, "guid": "plex://show/5d9c086c...", "Guid": []any{
				map[string]any{"id": "tmdb://1399"}, map[string]any{"id": "tvdb://121361"},
			}},
			"200": {"title": "B", "Guid": []any{map[string]any{"id": "tvdb://5"}}},
			"300": {"title": "C", "leafCount": 10},
			"400": {"title": "D", "leafCount": 4},
		},
	}
	srv := httptest.NewServer(f)
	defer srv.Close()

	got, err := NewPlex(srv.URL, "tok", nil).History(context.Background(), media.Series, since)
	if err != nil {
		t.Fatal(err)
	}
	m := byTitle(got)
	if len(m) != 5 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if e := m["Game of Thrones"]; e.TMDBID != 1399 || e.TVDBID != 121361 || e.Year != 2011 || e.Episodes != 2 || e.Plays != 3 || e.Signal != Rewatched {
		t.Errorf("A = %+v", e)
	}
	if e := m["B"]; e.TVDBID != 5 || e.Episodes != 6 || e.Signal != Watched {
		t.Errorf("B = %+v", e)
	}
	if e := m["C"]; e.Signal != Partial {
		t.Errorf("C = %+v", e)
	}
	if e := m["D"]; e.Signal != Watched {
		t.Errorf("D = %+v", e)
	}
	if e := m["Orphan"]; e.Signal != Partial || e.TMDBID != 0 {
		t.Errorf("orphan = %+v", e)
	}
}

func TestPlexPaging(t *testing.T) {
	since := time.Now().Add(-24 * time.Hour)
	at := time.Now().Unix()
	f := &plexFixture{t: t,
		history: func(r *http.Request) []map[string]any {
			start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
			if r.URL.Query().Get("X-Plex-Container-Size") != "500" {
				t.Errorf("page size = %q", r.URL.Query().Get("X-Plex-Container-Size"))
			}
			n := 500
			if start >= 500 {
				n = 3
			}
			page := make([]map[string]any, n)
			for i := range page {
				page[i] = map[string]any{"ratingKey": strconv.Itoa(start + i), "title": "m" + strconv.Itoa(start+i), "viewedAt": at}
			}
			return page
		},
	}
	srv := httptest.NewServer(f)
	defer srv.Close()

	got, err := NewPlex(srv.URL, "tok", nil).History(context.Background(), media.Movies, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 503 {
		t.Errorf("got %d entries, want 503", len(got))
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("history requests = %d, want 2", n)
	}
}
