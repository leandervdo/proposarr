package arr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/leandervdo/proposarr/internal/httpjson"
)

const key = "secret-key"

// server routes "METHOD /path" to a handler and fails on a missing API key.
func server(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("X-Api-Key"); got != key {
			t.Errorf("X-Api-Key = %q", got)
		}
		if r.URL.Query().Get("apikey") != "" || r.URL.Query().Get("apiKey") != "" {
			t.Errorf("api key leaked into query string")
		}
		h, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func jsonBody(v string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, v) }
}

func TestRadarrMovies(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/movie": jsonBody(`[{"id":1,"title":"Arrival","year":2016,"tmdbId":329865,"genres":["Drama","Science Fiction"],"added":"2023-04-01T10:00:00Z",
			"images":[{"coverType":"fanart","remoteUrl":"https://image.tmdb.org/t/p/original/fan.jpg"},{"coverType":"poster","url":"/MediaCover/1/poster.jpg","remoteUrl":"https://image.tmdb.org/t/p/original/poster.jpg"}]},
			{"id":2,"title":"Local Only","year":2001,"tmdbId":7,"images":[{"coverType":"poster","url":"/MediaCover/2/poster.jpg"}]}]`),
	})
	ms, err := NewRadarr(srv.URL+"/", key, nil).Movies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].TMDBID != 329865 || ms[0].Title != "Arrival" || ms[0].Year != 2016 || len(ms[0].Genres) != 2 || ms[0].Added.Year() != 2023 {
		t.Fatalf("movies = %+v", ms)
	}
	if ms[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Errorf("poster = %q, want the remote poster at w500", ms[0].PosterURL)
	}
	if ms[1].PosterURL != "" {
		t.Errorf("relative poster url should be dropped, got %q", ms[1].PosterURL)
	}
}

func TestSonarrSeries(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/series": jsonBody(`[{"id":3,"title":"Severance","year":2022,"tvdbId":371980,"tmdbId":95396,"genres":["Drama"],"images":[{"coverType":"poster","remoteUrl":"https://artworks.thetvdb.com/poster.jpg"}]},{"id":4,"title":"Old Show","year":1999,"tvdbId":1234}]`),
	})
	ss, err := NewSonarr(srv.URL, key, nil).Series(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 || ss[0].TVDBID != 371980 || ss[0].TMDBID != 95396 || ss[1].TMDBID != 0 || ss[1].TVDBID != 1234 {
		t.Fatalf("series = %+v", ss)
	}
	if ss[0].PosterURL != "https://artworks.thetvdb.com/poster.jpg" || ss[1].PosterURL != "" {
		t.Fatalf("posters = %q, %q", ss[0].PosterURL, ss[1].PosterURL)
	}
}

func TestRadarrDiscoverFilters(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/importlist/movie": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("includeRecommendations") != "true" || r.URL.Query().Get("includePopular") != "false" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			io.WriteString(w, `[
				{"tmdbId":1,"title":"Keep","year":2020,"overview":"o","genres":["Drama"],"ratings":{"tmdb":{"value":7.9,"votes":1200}}},
				{"tmdbId":2,"title":"Owned","isExisting":true},
				{"tmdbId":3,"title":"Excluded","isExcluded":true},
				{"tmdbId":0,"title":"No id"}
			]`)
		},
	})
	ms, err := NewRadarr(srv.URL, key, nil).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].TMDBID != 1 || ms[0].Rating != 7.9 || ms[0].Votes != 1200 || ms[0].Overview != "o" {
		t.Fatalf("discover = %+v", ms)
	}
}

func TestRadarrLookup(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/movie/lookup/tmdb": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("tmdbId") {
			case "10":
				io.WriteString(w, `{"title":"New","year":2021,"tmdbId":10,"titleSlug":"new-10"}`)
			case "11":
				io.WriteString(w, `{"id":42,"title":"Owned","year":2019,"tmdbId":11}`)
			default:
				http.NotFound(w, r)
			}
		},
	})
	r := NewRadarr(srv.URL, key, nil)
	ctx := context.Background()

	l, err := r.Lookup(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if l.LibraryID != 0 || l.Title != "New" || l.Raw["titleSlug"] != "new-10" {
		t.Fatalf("lookup = %+v", l)
	}
	if l, err = r.Lookup(ctx, 11); err != nil || l.LibraryID != 42 {
		t.Fatalf("in-library lookup = %+v, %v", l, err)
	}
	if _, err = r.Lookup(ctx, 99); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing lookup err = %v", err)
	}
}

func TestSonarrLookupPicksMatchingTVDB(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/series/lookup": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("term") {
			case "tvdb:200":
				io.WriteString(w, `[{"title":"Other","tvdbId":100},{"id":7,"title":"Match","year":2020,"tvdbId":200,"tmdbId":55,"seasons":[{"seasonNumber":1}]}]`)
			default:
				io.WriteString(w, `[]`)
			}
		},
	})
	s := NewSonarr(srv.URL, key, nil)
	l, err := s.Lookup(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if l.Title != "Match" || l.LibraryID != 7 || l.TVDBID != 200 || l.TMDBID != 55 || l.Raw["seasons"] == nil {
		t.Fatalf("lookup = %+v", l)
	}
	if _, err := s.Lookup(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("empty lookup err = %v", err)
	}
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRadarrAddPayload(t *testing.T) {
	var got map[string]any
	srv, _ := server(t, map[string]http.HandlerFunc{
		"POST /api/v3/movie": func(w http.ResponseWriter, r *http.Request) {
			got = decodeBody(t, r)
			io.WriteString(w, `{"id":99,"title":"New"}`)
		},
	})
	l := &Lookup{Title: "New", TMDBID: 10, Raw: map[string]any{"title": "New", "tmdbId": 10, "titleSlug": "new-10"}}
	res, err := NewRadarr(srv.URL, key, nil).Add(context.Background(), l, AddOptions{QualityProfileID: 4, RootFolderPath: "/movies", Search: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 99 {
		t.Fatalf("added = %+v", res)
	}
	opts, _ := got["addOptions"].(map[string]any)
	if got["qualityProfileId"] != float64(4) || got["rootFolderPath"] != "/movies" || got["monitored"] != true ||
		got["minimumAvailability"] != "released" || got["titleSlug"] != "new-10" ||
		opts["searchForMovie"] != true || opts["monitor"] != "movieOnly" {
		t.Fatalf("payload = %v", got)
	}
	if _, ok := l.Raw["qualityProfileId"]; ok {
		t.Fatal("Add mutated the lookup's Raw map")
	}
}

func TestSonarrAddPayload(t *testing.T) {
	var got map[string]any
	srv, _ := server(t, map[string]http.HandlerFunc{
		"POST /api/v3/series": func(w http.ResponseWriter, r *http.Request) {
			got = decodeBody(t, r)
			io.WriteString(w, `{"id":5,"title":"Match"}`)
		},
	})
	l := &Lookup{Title: "Match", TVDBID: 200, Raw: map[string]any{"title": "Match", "tvdbId": 200}}
	if _, err := NewSonarr(srv.URL, key, nil).Add(context.Background(), l, AddOptions{QualityProfileID: 2, RootFolderPath: "/tv"}); err != nil {
		t.Fatal(err)
	}
	opts, _ := got["addOptions"].(map[string]any)
	if got["qualityProfileId"] != float64(2) || got["rootFolderPath"] != "/tv" || got["seasonFolder"] != true ||
		got["monitored"] != true || opts["monitor"] != "all" || opts["searchForMissingEpisodes"] != false {
		t.Fatalf("payload = %v", got)
	}
}

func TestAddRefusesMissingChoices(t *testing.T) {
	srv, hits := server(t, nil)
	l := &Lookup{Title: "X", Raw: map[string]any{"title": "X"}}
	ctx := context.Background()

	if _, err := NewRadarr(srv.URL, key, nil).Add(ctx, l, AddOptions{RootFolderPath: "/movies"}); err == nil || !strings.Contains(err.Error(), "quality profile") {
		t.Fatalf("radarr err = %v", err)
	}
	if _, err := NewSonarr(srv.URL, key, nil).Add(ctx, l, AddOptions{QualityProfileID: 1}); err == nil || !strings.Contains(err.Error(), "root folder") {
		t.Fatalf("sonarr err = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("made %d HTTP calls before validating", hits.Load())
	}
}

func TestValidationErrorsSurface(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"POST /api/v3/movie": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `[{"propertyName":"TmdbId","errorMessage":"This movie has already been added"}]`)
		},
	})
	l := &Lookup{Title: "Dup", Raw: map[string]any{"title": "Dup"}}
	_, err := NewRadarr(srv.URL, key, nil).Add(context.Background(), l, AddOptions{QualityProfileID: 1, RootFolderPath: "/m"})
	if err == nil || !strings.Contains(err.Error(), "This movie has already been added") {
		t.Fatalf("err = %v", err)
	}
	var se *httpjson.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest {
		t.Fatalf("errors.As StatusError failed: %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || len(ae.Messages) != 1 {
		t.Fatalf("errors.As APIError failed: %v", err)
	}
}

func TestValidationMessagesTruncatedBody(t *testing.T) {
	got := validationMessages(`[{"propertyName":"Path","errorMessage":"Path is \"taken\""},{"propertyName":"X","errorMess`)
	if len(got) != 1 || got[0] != `Path is "taken"` {
		t.Fatalf("messages = %q", got)
	}
}

func TestProfilesAndFolders(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/qualityprofile": jsonBody(`[{"id":1,"name":"Any"},{"id":4,"name":"HD-1080p"}]`),
		"GET /api/v3/rootfolder":     jsonBody(`[{"id":1,"path":"/movies","accessible":true,"freeSpace":1000}]`),
		"GET /api/v3/system/status":  jsonBody(`{"appName":"Radarr","version":"5.9.1"}`),
	})
	r := NewRadarr(srv.URL, key, nil)
	ctx := context.Background()
	ps, err := r.QualityProfiles(ctx)
	if err != nil || len(ps) != 2 || ps[1].Name != "HD-1080p" {
		t.Fatalf("profiles = %+v, %v", ps, err)
	}
	fs, err := r.RootFolders(ctx)
	if err != nil || len(fs) != 1 || fs[0].Path != "/movies" || !fs[0].Accessible {
		t.Fatalf("folders = %+v, %v", fs, err)
	}
	st, err := r.Status(ctx)
	if err != nil || st.AppName != "Radarr" {
		t.Fatalf("status = %+v, %v", st, err)
	}
}
