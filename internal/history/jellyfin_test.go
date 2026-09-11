package history

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

func jfTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.0000000Z") }

// jellyfinServer routes /Users/{uid}/Items to items(uid, query).
func jellyfinServer(t *testing.T, users []string, items func(uid string, r *http.Request) []map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != `MediaBrowser Token="key"` {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/Users":
			if users == nil {
				t.Error("/Users called with a configured user id")
			}
			var out []map[string]any
			for _, u := range users {
				out = append(out, map[string]any{"Id": u})
			}
			writeJSON(w, out)
		case strings.HasPrefix(r.URL.Path, "/Users/") && strings.HasSuffix(r.URL.Path, "/Items"):
			uid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/Users/"), "/Items")
			list := items(uid, r)
			writeJSON(w, map[string]any{"Items": list, "TotalRecordCount": len(list)})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestJellyfinMergesUsers(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	t1, t2 := since.Add(time.Hour), since.Add(2*time.Hour)
	srv := jellyfinServer(t, []string{"u1", "u2"}, func(uid string, r *http.Request) []map[string]any {
		q := r.URL.Query()
		if q.Get("IncludeItemTypes") != "Movie" || q.Get("Filters") != "IsPlayed" {
			return nil
		}
		if uid == "u1" {
			return []map[string]any{
				{"Name": "The Matrix", "ProductionYear": 1999, "ProviderIds": map[string]string{"Tmdb": "603"}, "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": jfTime(t1)}},
			}
		}
		return []map[string]any{
			{"Name": "Arrival", "ProductionYear": 2016, "ProviderIds": map[string]string{"tmdb": "329865"}, "UserData": map[string]any{"PlayCount": 2, "LastPlayedDate": jfTime(t2)}},
			{"Name": "The Matrix", "ProductionYear": 1999, "ProviderIds": map[string]string{"Tmdb": "603"}, "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": jfTime(t2)}},
		}
	})
	defer srv.Close()

	got, err := NewJellyfin(srv.URL, "key", "", nil).History(context.Background(), media.Movies, since)
	if err != nil {
		t.Fatal(err)
	}
	m := byTitle(got)
	if len(m) != 2 {
		t.Fatalf("got %+v", got)
	}
	if e := m["The Matrix"]; e.TMDBID != 603 || e.Plays != 2 || e.Signal != Watched || !e.LastWatched.Equal(t2) {
		t.Errorf("matrix = %+v", e)
	}
	if e := m["Arrival"]; e.TMDBID != 329865 || e.Signal != Rewatched {
		t.Errorf("arrival = %+v", e)
	}
}

func TestJellyfinPartialAndCutoff(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour)
	var played atomic.Int32
	srv := jellyfinServer(t, nil, func(uid string, r *http.Request) []map[string]any {
		if uid != "u1" {
			t.Errorf("uid = %q", uid)
		}
		switch r.URL.Query().Get("Filters") {
		case "IsPlayed":
			played.Add(1)
			return []map[string]any{
				{"Name": "Recent", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": jfTime(since.Add(time.Hour))}},
				{"Name": "Old", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": jfTime(since.Add(-time.Hour))}},
			}
		case "IsResumable":
			return []map[string]any{
				{"Name": "Half", "ProviderIds": map[string]string{"Tmdb": "42"}, "UserData": map[string]any{"LastPlayedDate": jfTime(since.Add(time.Minute))}},
			}
		}
		return nil
	})
	defer srv.Close()

	got, err := NewJellyfin(srv.URL, "key", "u1", nil).History(context.Background(), media.Movies, since)
	if err != nil {
		t.Fatal(err)
	}
	m := byTitle(got)
	if _, ok := m["Old"]; ok || len(m) != 2 {
		t.Fatalf("got %+v", got)
	}
	if e := m["Half"]; e.Signal != Partial || e.Plays != 0 || e.TMDBID != 42 {
		t.Errorf("half = %+v", e)
	}
	if n := played.Load(); n != 1 {
		t.Errorf("played pages = %d, want 1", n)
	}
}

func TestJellyfinSeries(t *testing.T) {
	since := time.Now().Add(-30 * 24 * time.Hour)
	at := jfTime(since.Add(time.Hour))
	srv := jellyfinServer(t, nil, func(uid string, r *http.Request) []map[string]any {
		q := r.URL.Query()
		if ids := q.Get("Ids"); ids != "" {
			if ids != "s1,s2,s3" {
				t.Errorf("Ids = %q", ids)
			}
			return []map[string]any{
				{"Id": "s1", "Name": "Breaking Bad", "ProductionYear": 2008, "ProviderIds": map[string]string{"Tvdb": "81189", "Tmdb": "1396"}, "UserData": map[string]any{"UnplayedItemCount": 1}},
				{"Id": "s2", "Name": "Game of Thrones", "ProviderIds": map[string]string{"tmdb": "1399"}, "UserData": map[string]any{"UnplayedItemCount": 9}},
			}
		}
		if q.Get("IncludeItemTypes") != "Episode" || q.Get("Filters") != "IsPlayed" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
			return nil
		}
		return []map[string]any{
			{"Id": "e1", "SeriesId": "s1", "SeriesName": "BB", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": at}},
			{"Id": "e2", "SeriesId": "s1", "SeriesName": "BB", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": at}},
			{"Id": "e3", "SeriesId": "s2", "SeriesName": "GoT", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": at}},
			{"Id": "e4", "SeriesId": "s3", "SeriesName": "Rewatch", "UserData": map[string]any{"PlayCount": 2, "LastPlayedDate": at}},
			{"Id": "e5", "Name": "loose", "UserData": map[string]any{"PlayCount": 1, "LastPlayedDate": at}},
		}
	})
	defer srv.Close()

	got, err := NewJellyfin(srv.URL, "key", "u1", nil).History(context.Background(), media.Series, since)
	if err != nil {
		t.Fatal(err)
	}
	m := byTitle(got)
	if len(m) != 3 {
		t.Fatalf("got %+v", got)
	}
	if e := m["Breaking Bad"]; e.TMDBID != 1396 || e.TVDBID != 81189 || e.Year != 2008 || e.Episodes != 2 || e.Signal != Watched {
		t.Errorf("s1 = %+v", e)
	}
	if e := m["Game of Thrones"]; e.TMDBID != 1399 || e.Signal != Partial {
		t.Errorf("s2 = %+v", e)
	}
	if e := m["Rewatch"]; e.Signal != Rewatched || e.TMDBID != 0 {
		t.Errorf("s3 = %+v", e)
	}
}
