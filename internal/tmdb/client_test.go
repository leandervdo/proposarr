package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/leandervdo/proposarr/internal/media"
)

func serve(t *testing.T, routes map[string]string, check func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const movieGenres = `{"genres":[{"id":18,"name":"Drama"},{"id":878,"name":"Science Fiction"}]}`
const tvGenres = `{"genres":[{"id":18,"name":"Drama"},{"id":10765,"name":"Sci-Fi & Fantasy"}]}`

func TestAuthModes(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		key        string
		wantHeader string
		wantQuery  string
	}{
		{key: "eyJhbGciOi.token", wantHeader: "Bearer eyJhbGciOi.token"},
		{key: "abc123", wantQuery: "abc123"},
	} {
		var gotHeader, gotQuery string
		srv := serve(t, map[string]string{"/configuration": `{}`}, func(r *http.Request) {
			gotHeader = r.Header.Get("Authorization")
			gotQuery = r.URL.Query().Get("api_key")
		})
		if err := newClient(srv.URL, tc.key, nil).Ping(ctx); err != nil {
			t.Fatalf("ping: %v", err)
		}
		if gotHeader != tc.wantHeader || gotQuery != tc.wantQuery {
			t.Errorf("key %q: header %q query %q", tc.key, gotHeader, gotQuery)
		}
	}
}

func TestRecommendationsNormalises(t *testing.T) {
	ctx := context.Background()
	srv := serve(t, map[string]string{
		"/genre/movie/list":          movieGenres,
		"/genre/tv/list":             tvGenres,
		"/movie/603/recommendations": `{"results":[{"id":1,"title":"Arrival","release_date":"2016-11-10","overview":"o","genre_ids":[18,878,99],"vote_average":7.6,"vote_count":18000,"popularity":40.5,"poster_path":"/a.jpg"},{"id":2,"title":"Untitled","release_date":""}]}`,
		"/tv/1399/similar":           `{"results":[{"id":3,"name":"The Expanse","first_air_date":"2015-12-14","genre_ids":[10765]},{"id":4,"name":"Pilot","first_air_date":""}]}`,
	}, nil)
	c := newClient(srv.URL, "k", nil)

	got, err := c.Recommendations(ctx, media.Movies, 603)
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{
		{ID: 1, Kind: media.Movies, Title: "Arrival", Year: 2016, Overview: "o", Genres: []string{"Drama", "Science Fiction"}, Rating: 7.6, Votes: 18000, Popularity: 40.5, PosterPath: "/a.jpg"},
		{ID: 2, Kind: media.Movies, Title: "Untitled"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("movies:\n got %+v\nwant %+v", got, want)
	}

	tv, err := c.Similar(ctx, media.Series, 1399)
	if err != nil {
		t.Fatal(err)
	}
	wantTV := []Item{
		{ID: 3, Kind: media.Series, Title: "The Expanse", Year: 2015, Genres: []string{"Sci-Fi & Fantasy"}},
		{ID: 4, Kind: media.Series, Title: "Pilot"},
	}
	if !reflect.DeepEqual(tv, wantTV) {
		t.Errorf("tv:\n got %+v\nwant %+v", tv, wantTV)
	}
}

func TestGenreLoadRetriesAfterFailure(t *testing.T) {
	ctx := context.Background()
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/genre/movie/list":
			if fail.Load() {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(movieGenres))
		case "/movie/1/similar":
			_, _ = w.Write([]byte(`{"results":[{"id":5,"title":"X","genre_ids":[18]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newClient(srv.URL, "k", nil)

	items, err := c.Similar(ctx, media.Movies, 1)
	if err != nil || len(items[0].Genres) != 0 {
		t.Fatalf("first call: %v %+v", err, items)
	}
	fail.Store(false)
	items, err = c.Similar(ctx, media.Movies, 1)
	if err != nil || !reflect.DeepEqual(items[0].Genres, []string{"Drama"}) {
		t.Fatalf("second call: %v %+v", err, items)
	}
}

func TestDetails(t *testing.T) {
	ctx := context.Background()
	var gotAppend string
	srv := serve(t, map[string]string{
		"/movie/329865": `{"id":329865,"title":"Arrival","release_date":"2016-11-10","genres":[{"id":18,"name":"Drama"}],"status":"Released","runtime":116,"imdb_id":"tt2543164",
			"external_ids":{"imdb_id":"tt2543164","tvdb_id":null},
			"watch/providers":{"results":{"US":{"flatrate":[{"provider_name":"Netflix"},{"provider_name":"Hulu"},{"provider_name":"Netflix"}]},"NL":{"flatrate":[{"provider_name":"Videoland"}]}}}}`,
		"/tv/63639": `{"id":63639,"name":"The Expanse","first_air_date":"2015-12-14","status":"Ended","episode_run_time":[45,50],
			"external_ids":{"imdb_id":"tt3230854","tvdb_id":280619},"watch/providers":{"results":{}}}`,
	}, func(r *http.Request) { gotAppend = r.URL.Query().Get("append_to_response") })
	c := newClient(srv.URL, "k", nil)

	d, err := c.Details(ctx, media.Movies, 329865, "")
	if err != nil {
		t.Fatal(err)
	}
	if gotAppend != "watch/providers,external_ids" {
		t.Errorf("append_to_response = %q", gotAppend)
	}
	if d.Title != "Arrival" || d.Year != 2016 || d.Runtime != 116 || d.Status != "Released" || d.IMDBID != "tt2543164" || d.TVDBID != 0 {
		t.Errorf("movie details: %+v", d)
	}
	if !reflect.DeepEqual(d.Providers, []string{"Netflix", "Hulu"}) || !reflect.DeepEqual(d.Genres, []string{"Drama"}) {
		t.Errorf("providers %v genres %v", d.Providers, d.Genres)
	}

	nl, err := c.Details(ctx, media.Movies, 329865, "nl")
	if err != nil || !reflect.DeepEqual(nl.Providers, []string{"Videoland"}) {
		t.Errorf("NL providers: %v %v", err, nl.Providers)
	}

	tv, err := c.Details(ctx, media.Series, 63639, "US")
	if err != nil {
		t.Fatal(err)
	}
	if tv.Title != "The Expanse" || tv.Runtime != 45 || tv.TVDBID != 280619 || tv.IMDBID != "tt3230854" || tv.Providers != nil {
		t.Errorf("tv details: %+v", tv)
	}
}

func TestSearchParams(t *testing.T) {
	ctx := context.Background()
	var got []*http.Request
	srv := serve(t, map[string]string{
		"/search/movie":     `{"results":[{"id":1,"title":"Heat","release_date":"1995-12-15"}]}`,
		"/search/tv":        `{"results":[]}`,
		"/genre/movie/list": movieGenres,
		"/genre/tv/list":    tvGenres,
	}, func(r *http.Request) {
		if r.URL.Path == "/search/movie" || r.URL.Path == "/search/tv" {
			got = append(got, r)
		}
	})
	c := newClient(srv.URL, "k", nil)

	items, err := c.Search(ctx, media.Movies, "Heat", 1995)
	if err != nil || len(items) != 1 || items[0].Year != 1995 {
		t.Fatalf("movie search: %v %+v", err, items)
	}
	if _, err := c.Search(ctx, media.Series, "Dark", 2017); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Search(ctx, media.Movies, "Heat", 0); err != nil {
		t.Fatal(err)
	}
	m, tv, noYear := got[0].URL.Query(), got[1].URL.Query(), got[2].URL.Query()
	if m.Get("query") != "Heat" || m.Get("year") != "1995" || m.Get("include_adult") != "false" {
		t.Errorf("movie query %v", m)
	}
	if tv.Get("first_air_date_year") != "2017" || tv.Has("year") {
		t.Errorf("tv query %v", tv)
	}
	if noYear.Has("year") {
		t.Errorf("year sent when 0: %v", noYear)
	}
}

func TestFindAndTVDB(t *testing.T) {
	ctx := context.Background()
	var source string
	srv := serve(t, map[string]string{
		"/find/280619":           `{"tv_results":[{"id":63639}]}`,
		"/find/1":                `{"tv_results":[]}`,
		"/tv/63639/external_ids": `{"tvdb_id":280619}`,
		"/tv/2/external_ids":     `{"tvdb_id":null}`,
	}, func(r *http.Request) {
		if v := r.URL.Query().Get("external_source"); v != "" {
			source = v
		}
	})
	c := newClient(srv.URL, "k", nil)

	if id, err := c.FindByTVDB(ctx, 280619); err != nil || id != 63639 || source != "tvdb_id" {
		t.Errorf("find: %d %v source %q", id, err, source)
	}
	if id, err := c.FindByTVDB(ctx, 1); err != nil || id != 0 {
		t.Errorf("find none: %d %v", id, err)
	}
	if id, err := c.TVDBID(ctx, 63639); err != nil || id != 280619 {
		t.Errorf("tvdb: %d %v", id, err)
	}
	if id, err := c.TVDBID(ctx, 2); err != nil || id != 0 {
		t.Errorf("tvdb null: %d %v", id, err)
	}
}

func TestRetriesOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if err := newClient(srv.URL, "k", nil).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestPosterURL(t *testing.T) {
	if PosterURL("") != "" || PosterURL("/a.jpg") != "https://image.tmdb.org/t/p/w500/a.jpg" {
		t.Error("PosterURL")
	}
}
