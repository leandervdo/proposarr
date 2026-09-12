package arr

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"testing"
)

func TestQualityProfileRules(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/qualityprofile": jsonBody(`[
			{"id":7,"name":"Remux + WEB 2160p","minFormatScore":10,"language":{"id":1,"name":"English"},
			 "items":[
				{"quality":{"id":0,"name":"Unknown"},"items":[],"allowed":false},
				{"name":"WEB 1080p","id":1001,"allowed":false,"items":[{"quality":{"id":3,"name":"WEBDL-1080p"},"items":[],"allowed":true}]},
				{"name":"WEB 2160p","id":1000,"allowed":true,"items":[{"quality":{"id":18,"name":"WEBDL-2160p"},"items":[],"allowed":false},{"quality":{"id":17,"name":"WEBRip-2160p"},"items":[],"allowed":false}]},
				{"quality":{"id":31,"name":"Remux-2160p"},"items":[],"allowed":true}],
			 "formatItems":[{"format":1,"name":"HDR","score":500},{"format":2,"name":"LQ","score":-10000},{"format":3,"name":"Unscored","score":0}]},
			{"id":1,"name":"Any","items":[{"quality":{"id":1,"name":"SDTV"},"items":[],"allowed":true}]},
			{"id":2,"name":"Bare"}
		]`),
	})
	ps, err := NewRadarr(srv.URL, key, nil).QualityProfiles(context.Background())
	if err != nil || len(ps) != 3 {
		t.Fatalf("profiles = %+v, %v", ps, err)
	}
	r := ps[0].Rules
	if ps[0].Name != "Remux + WEB 2160p" || r == nil {
		t.Fatalf("profile = %+v", ps[0])
	}
	// Only a group's own allowed flag counts; later items are preferred.
	if want := map[int]int{18: 2, 17: 2, 31: 3}; !maps.Equal(r.Qualities, want) {
		t.Errorf("qualities = %v, want %v", r.Qualities, want)
	}
	if r.MinFormatScore != 10 || r.Language != 1 || len(r.FormatScores) != 2 || r.Score([]int{1, 2, 3}) != -9500 {
		t.Errorf("rules = %+v", r)
	}
	if ps[1].Rules == nil || ps[1].Rules.Language != LanguageAny {
		t.Errorf("profile without a language = %+v", ps[1].Rules)
	}
	if ps[2].Rules != nil {
		t.Errorf("profile without items has rules %+v", ps[2].Rules)
	}
}

func TestRadarrReleases(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/release": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("movieId"); got != "250" {
				t.Errorf("movieId = %q", got)
			}
			io.WriteString(w, `[
				{"guid":"a","indexerId":1,"indexer":"TorrentLeech","title":"Children of Men 2006 BluRay 1080p REMUX-FraMeSToR","size":32400000000,
				 "protocol":"torrent","seeders":249,"quality":{"quality":{"id":30,"name":"Remux-1080p","source":"bluray","resolution":1080,"modifier":"remux"}},
				 "customFormats":[{"id":7,"name":"Remux Tier 01"},{"id":47,"name":"DTS-HD MA"}],"languages":[{"id":1,"name":"English"}],
				 "approved":false,"downloadAllowed":true,"rejections":["Remux-1080p is not wanted in profile"]},
				{"guid":"b","indexerId":2,"title":"Children of Men 2006 2160p WEB-DL","protocol":"usenet","seeders":null,
				 "quality":{"quality":{"id":18,"name":"WEBDL-2160p"}},"approved":true,"downloadAllowed":true,"rejections":[]},
				{"guid":"c","indexerId":2,"title":"Unmapped","protocol":"usenet","quality":{"quality":{"id":18,"name":"WEBDL-2160p"}},
				 "approved":true,"downloadAllowed":false},
				{"guid":"d","indexerId":3,"title":"Older Radarr","protocol":"usenet","quality":{"quality":{"id":18,"name":"WEBDL-2160p"}},"approved":true}
			]`)
		},
	})
	rs, err := NewRadarr(srv.URL, key, nil).Releases(context.Background(), 250)
	if err != nil || len(rs) != 4 {
		t.Fatalf("releases = %+v, %v", rs, err)
	}
	a := rs[0]
	if a.GUID != "a" || a.IndexerID != 1 || a.Indexer != "TorrentLeech" || a.Size != 32_400_000_000 || a.Protocol != "torrent" || a.Seeders != 249 ||
		a.QualityID != 30 || a.Quality != "Remux-1080p" ||
		!slices.Equal(a.CustomFormats, []int{7, 47}) || !slices.Equal(a.Languages, []int{1}) || a.Approved || len(a.Rejections) != 1 {
		t.Errorf("release = %+v", a)
	}
	if !rs[1].Approved || rs[1].Seeders != 0 {
		t.Errorf("approved usenet release = %+v", rs[1])
	}
	if rs[2].Approved {
		t.Error("a release Radarr cannot download counts as approved")
	}
	if !rs[3].Approved {
		t.Error("a release without downloadAllowed should keep Radarr's approval")
	}
}

func TestRadarrMovieAndProfile(t *testing.T) {
	var editor map[string]any
	srv, _ := server(t, map[string]http.HandlerFunc{
		"PUT /api/v3/movie/editor": func(w http.ResponseWriter, r *http.Request) {
			editor = decodeBody(t, r)
			w.WriteHeader(http.StatusAccepted)
		},
		"GET /api/v3/movie/250": jsonBody(`{"id":250,"tmdbId":9693,"title":"Children of Men","year":2006,"qualityProfileId":7,"isAvailable":true,"originalLanguage":{"id":1,"name":"English"}}`),
	})
	r := NewRadarr(srv.URL, key, nil)
	ctx := context.Background()

	if err := r.SetQualityProfile(ctx, 250, 8); err != nil || editor["qualityProfileId"] != float64(8) || !slices.Equal(editor["movieIds"].([]any), []any{float64(250)}) {
		t.Errorf("set profile = %v, body %v", err, editor)
	}
	m, err := r.Movie(ctx, 250)
	if err != nil || m != (Movie{ID: 250, TMDBID: 9693, Title: "Children of Men", Year: 2006, QualityProfileID: 7, OriginalLanguage: 1, Available: true}) {
		t.Errorf("movie = %+v, %v", m, err)
	}
	if _, err := r.Movie(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing movie err = %v", err)
	}
}

func TestRadarrSearchCommands(t *testing.T) {
	var queued map[string]any
	srv, _ := server(t, map[string]http.HandlerFunc{
		"POST /api/v3/command": func(w http.ResponseWriter, r *http.Request) {
			queued = decodeBody(t, r)
			io.WriteString(w, `{"id":9,"name":"MoviesSearch","status":"queued","body":{"movieIds":[250],"name":"MoviesSearch"}}`)
		},
		"GET /api/v3/command/9": jsonBody(`{"id":9,"name":"MoviesSearch","status":"completed","body":{"movieIds":[250]}}`),
		"GET /api/v3/command": jsonBody(`[
			{"id":4,"name":"RefreshMonitoredDownloads","status":"completed","body":{}},
			{"id":5,"name":"MoviesSearch","status":"completed","body":{"movieIds":[250]}},
			{"id":6,"name":"MoviesSearch","status":"started","body":{"movieIds":[251]}},
			{"id":9,"name":"MoviesSearch","status":"started","body":{"movieIds":[12,250]}}
		]`),
	})
	r := NewRadarr(srv.URL, key, nil)
	ctx := context.Background()

	c, err := r.SearchMovie(ctx, 250)
	if err != nil || c.ID != 9 || c.Done() || queued["name"] != "MoviesSearch" || !slices.Equal(queued["movieIds"].([]any), []any{float64(250)}) {
		t.Errorf("search = %+v, %v, body %v", c, err, queued)
	}
	if c, err := r.Command(ctx, 9); err != nil || !c.Done() || c.Status != "completed" {
		t.Errorf("command = %+v, %v", c, err)
	}
	cs, err := r.MovieSearches(ctx, 250)
	if err != nil || len(cs) != 2 || cs[0].ID != 9 || cs[1].ID != 5 {
		t.Errorf("searches = %+v, %v", cs, err)
	}
}

func TestRadarrGrabsAndPending(t *testing.T) {
	srv, _ := server(t, map[string]http.HandlerFunc{
		"GET /api/v3/history/movie": func(w http.ResponseWriter, r *http.Request) {
			if q := r.URL.Query(); q.Get("movieId") != "250" || q.Get("eventType") != "grabbed" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			io.WriteString(w, `[
				{"id":3,"eventType":"downloadFolderImported","sourceTitle":"Imported","quality":{"quality":{"name":"Remux-1080p"}},"data":{}},
				{"id":1,"eventType":"grabbed","sourceTitle":"Old","quality":{"quality":{"name":"Bluray-1080p"}},"data":{"size":"15000000000","protocol":"1","indexer":"NZBgeek"}},
				{"id":2,"eventType":"grabbed","sourceTitle":"Children of Men 2006 BluRay 1080p REMUX-FraMeSToR","quality":{"quality":{"id":30,"name":"Remux-1080p"}},
				 "data":{"indexer":"TorrentLeech (Prowlarr)","size":"32390607569","protocol":"2","releaseGroup":"FraMeSToR","downloadClient":null}}
			]`)
		},
		"GET /api/v3/queue": jsonBody(`{"page":1,"totalRecords":3,"records":[
			{"movieId":250,"status":"downloading","title":"Downloading","quality":{"quality":{"name":"Remux-1080p"}}},
			{"movieId":250,"status":"delay","title":"Children of Men 2006 2160p WEB-DL","size":21500000000.0,"indexer":"NZBgeek","protocol":"usenet","quality":{"quality":{"name":"WEBDL-2160p"}}},
			{"movieId":251,"status":"delay","title":"Another movie","quality":{"quality":{"name":"WEBDL-1080p"}}}
		]}`),
	})
	r := NewRadarr(srv.URL, key, nil)
	ctx := context.Background()

	gs, err := r.Grabs(ctx, 250)
	if err != nil || len(gs) != 2 {
		t.Fatalf("grabs = %+v, %v", gs, err)
	}
	if want := (Grab{Title: "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", Quality: "Remux-1080p", Size: 32390607569, Indexer: "TorrentLeech (Prowlarr)", Protocol: "torrent"}); gs[0] != want {
		t.Errorf("newest grab = %+v, want %+v", gs[0], want)
	}
	if gs[1].Title != "Old" || gs[1].Protocol != "usenet" {
		t.Errorf("older grab = %+v", gs[1])
	}
	ps, err := r.Pending(ctx, 250)
	if err != nil || len(ps) != 1 || ps[0] != (Grab{Title: "Children of Men 2006 2160p WEB-DL", Quality: "WEBDL-2160p", Size: 21_500_000_000, Indexer: "NZBgeek", Protocol: "usenet"}) {
		t.Errorf("pending = %+v, %v", ps, err)
	}
}

func TestAddAlreadyAdded(t *testing.T) {
	refuse := func(msg string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `[{"propertyName":"TmdbId","errorMessage":"`+msg+`"}]`)
		}
	}
	srv, _ := server(t, map[string]http.HandlerFunc{
		"POST /api/v3/movie":  refuse("This movie has already been added"),
		"POST /api/v3/series": refuse("This series has already been added"),
	})
	l := &Lookup{Title: "Dup", Raw: map[string]any{"title": "Dup"}}
	opts := AddOptions{QualityProfileID: 1, RootFolderPath: "/m"}
	ctx := context.Background()

	_, err := NewRadarr(srv.URL, key, nil).Add(ctx, l, opts)
	var ae *APIError
	if !errors.Is(err, ErrAlreadyAdded) || !errors.As(err, &ae) {
		t.Errorf("radarr err = %v", err)
	}
	if _, err := NewSonarr(srv.URL, key, nil).Add(ctx, l, opts); !errors.Is(err, ErrAlreadyAdded) {
		t.Errorf("sonarr err = %v", err)
	}
}

func TestSearchTimeout(t *testing.T) {
	if c := withTimeout(&http.Client{Timeout: 30e9}, searchTimeout); c.Timeout != searchTimeout {
		t.Errorf("timeout = %v", c.Timeout)
	}
	if c := withTimeout(&http.Client{}, searchTimeout); c.Timeout != 0 {
		t.Errorf("no timeout became %v", c.Timeout)
	}
	if c := withTimeout(nil, searchTimeout); c.Timeout != searchTimeout {
		t.Errorf("nil client timeout = %v", c.Timeout)
	}
}
