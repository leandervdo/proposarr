package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/leandervdo/proposarr/internal/httpjson"
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

func TestFullDetailsMovie(t *testing.T) {
	ctx := context.Background()
	var gotAppend string
	cast := `[`
	for i := 13; i >= 0; i-- {
		if i < 13 {
			cast += ","
		}
		profile := `null`
		if i == 0 {
			profile = `"/amy.jpg"`
		}
		cast += fmt.Sprintf(`{"name":"Actor %d","character":"Role %d","order":%d,"profile_path":%s}`, i, i, i, profile)
	}
	cast += `]`
	srv := serve(t, map[string]string{
		"/movie/329865": `{"id":329865,"title":"Arrival","tagline":"Why are they here?","overview":"Linguist meets aliens","release_date":"2016-11-10",
			"genres":[{"id":18,"name":"Drama"},{"id":878,"name":"Science Fiction"}],"status":"Released","runtime":116,"imdb_id":"tt2543164",
			"poster_path":"/p.jpg","backdrop_path":"/b.jpg","vote_average":7.6,"vote_count":18000,
			"credits":{"cast":` + cast + `,"crew":[{"name":"Bradford Young","job":"Director of Photography"},{"name":"Denis Villeneuve","job":"Director"},{"name":"Denis Villeneuve","job":"Director"}]},
			"videos":{"results":[
				{"name":"Vimeo Trailer","key":"v1","site":"Vimeo","type":"Trailer","official":true},
				{"name":"Teaser","key":"t1","site":"YouTube","type":"Teaser","official":true},
				{"name":"Fan Trailer","key":"f1","site":"YouTube","type":"Trailer","official":false},
				{"name":"Featurette","key":"x1","site":"YouTube","type":"Featurette","official":true},
				{"name":"Official Trailer","key":"o1","site":"YouTube","type":"Trailer","official":true},
				{"name":"Official Trailer 2","key":"o2","site":"YouTube","type":"Trailer","official":true}]},
			"external_ids":{"imdb_id":"tt2543164"},
			"watch/providers":{"results":{"NL":{"flatrate":[{"provider_name":"Netflix"}]}}}}`,
	}, func(r *http.Request) { gotAppend = r.URL.Query().Get("append_to_response") })
	c := newClient(srv.URL, "k", nil)

	d, err := c.FullDetails(ctx, media.Movies, 329865, "nl")
	if err != nil {
		t.Fatal(err)
	}
	if gotAppend != "credits,videos,watch/providers,external_ids" {
		t.Errorf("append_to_response = %q", gotAppend)
	}
	if d.ID != 329865 || d.Kind != media.Movies || d.Title != "Arrival" || d.Year != 2016 || d.Tagline != "Why are they here?" ||
		d.Overview != "Linguist meets aliens" || d.Runtime != 116 || d.ReleaseDate != "2016-11-10" || d.Status != "Released" ||
		d.Seasons != 0 || d.Episodes != 0 || d.IMDBID != "tt2543164" || d.Rating != 7.6 || d.Votes != 18000 {
		t.Errorf("movie = %+v", d)
	}
	if d.PosterURL != "https://image.tmdb.org/t/p/w500/p.jpg" || d.BackdropURL != "https://image.tmdb.org/t/p/w1280/b.jpg" {
		t.Errorf("images = %q %q", d.PosterURL, d.BackdropURL)
	}
	if !reflect.DeepEqual(d.Genres, []string{"Drama", "Science Fiction"}) || !reflect.DeepEqual(d.Directors, []string{"Denis Villeneuve"}) ||
		!reflect.DeepEqual(d.Streaming, []string{"Netflix"}) {
		t.Errorf("genres %v directors %v streaming %v", d.Genres, d.Directors, d.Streaming)
	}
	if len(d.Cast) != MaxCast {
		t.Fatalf("cast = %d members, want %d", len(d.Cast), MaxCast)
	}
	for i, m := range d.Cast {
		if m.Name != fmt.Sprintf("Actor %d", i) || m.Character != fmt.Sprintf("Role %d", i) {
			t.Errorf("cast[%d] = %+v", i, m)
		}
	}
	if d.Cast[0].ProfileURL != "https://image.tmdb.org/t/p/w185/amy.jpg" || d.Cast[1].ProfileURL != "" {
		t.Errorf("profile urls = %q %q", d.Cast[0].ProfileURL, d.Cast[1].ProfileURL)
	}
	if d.Trailer == nil || *d.Trailer != (Trailer{Name: "Official Trailer", YouTubeKey: "o1"}) {
		t.Errorf("trailer = %+v", d.Trailer)
	}
}

func TestFullDetailsTrailerPreference(t *testing.T) {
	ctx := context.Background()
	video := func(name, key, site, typ string, official bool) string {
		return fmt.Sprintf(`{"name":%q,"key":%q,"site":%q,"type":%q,"official":%v}`, name, key, site, typ, official)
	}
	for _, tc := range []struct {
		name   string
		videos []string
		want   *Trailer
	}{
		{"unofficial trailer beats teaser", []string{video("Teaser", "t", "YouTube", "Teaser", true), video("Trailer", "tr", "YouTube", "Trailer", false)}, &Trailer{"Trailer", "tr"}},
		{"teaser when no trailer", []string{video("Clip", "c", "YouTube", "Clip", true), video("Teaser", "t", "YouTube", "Teaser", false)}, &Trailer{"Teaser", "t"}},
		{"no youtube video", []string{video("Trailer", "v", "Vimeo", "Trailer", true), video("Clip", "c", "YouTube", "Clip", true)}, nil},
		{"no videos", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := serve(t, map[string]string{
				"/movie/1": `{"id":1,"title":"X","videos":{"results":[` + strings.Join(tc.videos, ",") + `]}}`,
			}, nil)
			d, err := newClient(srv.URL, "k", nil).FullDetails(ctx, media.Movies, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.Trailer, tc.want) {
				t.Errorf("trailer = %+v, want %+v", d.Trailer, tc.want)
			}
			if d.Directors == nil || d.Cast == nil || d.Genres == nil || d.Streaming == nil {
				t.Errorf("nil slices: %+v", d)
			}
		})
	}
}

func TestFullDetailsSeries(t *testing.T) {
	ctx := context.Background()
	srv := serve(t, map[string]string{
		"/tv/95396": `{"id":95396,"name":"Severance","first_air_date":"2022-02-17","release_date":"","status":"Returning Series",
			"episode_run_time":[55,40],"number_of_seasons":2,"number_of_episodes":19,"genres":[{"id":18,"name":"Drama"}],
			"created_by":[{"name":"Dan Erickson"}],
			"credits":{"cast":[{"name":"Adam Scott","character":"Mark S.","order":0}],"crew":[{"name":"Ben Stiller","job":"Director"}]},
			"external_ids":{"imdb_id":"tt11280740","tvdb_id":371980},
			"watch/providers":{"results":{"US":{"flatrate":[{"provider_name":"Apple TV+"}]}}}}`,
	}, nil)
	d, err := newClient(srv.URL, "k", nil).FullDetails(ctx, media.Series, 95396, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Severance" || d.Kind != media.Series || d.Year != 2022 || d.ReleaseDate != "2022-02-17" || d.Runtime != 55 ||
		d.Seasons != 2 || d.Episodes != 19 || d.Status != "Returning Series" || d.IMDBID != "tt11280740" || d.TVDBID != 371980 {
		t.Errorf("series = %+v", d)
	}
	if !reflect.DeepEqual(d.Directors, []string{"Dan Erickson"}) || !reflect.DeepEqual(d.Streaming, []string{"Apple TV+"}) ||
		len(d.Cast) != 1 || d.Cast[0] != (CastMember{Name: "Adam Scott", Character: "Mark S."}) || d.Trailer != nil {
		t.Errorf("directors %v streaming %v cast %+v trailer %+v", d.Directors, d.Streaming, d.Cast, d.Trailer)
	}
	if d.PosterURL != "" || d.BackdropURL != "" {
		t.Errorf("images without paths = %q %q", d.PosterURL, d.BackdropURL)
	}
}

func TestFullDetailsNotFound(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/movie/500" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"status_code":34,"status_message":"The resource you requested could not be found."}`))
	}))
	defer srv.Close()
	c := newClient(srv.URL, "k", nil)

	_, err := c.FullDetails(ctx, media.Series, 99999999, "")
	if !errors.Is(err, ErrNotFound) || !httpjson.IsStatus(err, http.StatusNotFound) {
		t.Errorf("404 err = %v", err)
	}
	if _, err := c.Details(ctx, media.Movies, 99999999, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Details 404 err = %v", err)
	}
	if _, err := c.FullDetails(ctx, media.Movies, 500, ""); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("500 err = %v", err)
	}
}

func TestPosterURL(t *testing.T) {
	if PosterURL("") != "" || PosterURL("/a.jpg") != "https://image.tmdb.org/t/p/w500/a.jpg" {
		t.Error("PosterURL")
	}
}
