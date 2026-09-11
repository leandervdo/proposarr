package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/leandervdo/proposarr/internal/httpjson"
	"github.com/leandervdo/proposarr/internal/media"
)

const baseURL = "https://api.themoviedb.org/3"

// Client talks to the TMDB v3 API. It is safe for concurrent use.
type Client struct {
	hc *httpjson.Client

	mu     sync.Mutex
	genres map[media.Kind]map[int]string
}

// New builds a client. A key starting with "eyJ" is treated as a v4 read
// access token and sent as a bearer header; anything else is a v3 api_key.
func New(apiKey string, hc *http.Client) *Client {
	return newClient(baseURL, apiKey, hc)
}

func newClient(base, apiKey string, hc *http.Client) *Client {
	c := &httpjson.Client{BaseURL: base, HTTP: hc, MaxRetries: 3}
	if strings.HasPrefix(apiKey, "eyJ") {
		c.Header = http.Header{"Authorization": {"Bearer " + apiKey}}
	} else if apiKey != "" {
		c.Query = url.Values{"api_key": {apiKey}}
	}
	return &Client{hc: c, genres: map[media.Kind]map[int]string{}}
}

func segment(kind media.Kind) string {
	if kind == media.Series {
		return "tv"
	}
	return "movie"
}

// rawItem covers both the movie and tv result shapes.
type rawItem struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	Overview     string  `json:"overview"`
	GenreIDs     []int   `json:"genre_ids"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Popularity   float64 `json:"popularity"`
	PosterPath   string  `json:"poster_path"`
}

func (r rawItem) item(kind media.Kind) Item {
	title, date := r.Title, r.ReleaseDate
	if kind == media.Series {
		title, date = r.Name, r.FirstAirDate
	}
	return Item{
		ID:         r.ID,
		Kind:       kind,
		Title:      title,
		Year:       yearOf(date),
		Overview:   r.Overview,
		Rating:     r.VoteAverage,
		Votes:      r.VoteCount,
		Popularity: r.Popularity,
		PosterPath: r.PosterPath,
	}
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

type page struct {
	Results []rawItem `json:"results"`
}

// Ping checks the credentials against /configuration.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Get(ctx, "/configuration", nil, nil)
}

// Recommendations returns page 1 of TMDB's recommendations for a title.
func (c *Client) Recommendations(ctx context.Context, kind media.Kind, id int) ([]Item, error) {
	return c.list(ctx, kind, fmt.Sprintf("/%s/%d/recommendations", segment(kind), id), nil)
}

// Similar returns page 1 of TMDB's similar titles.
func (c *Client) Similar(ctx context.Context, kind media.Kind, id int) ([]Item, error) {
	return c.list(ctx, kind, fmt.Sprintf("/%s/%d/similar", segment(kind), id), nil)
}

// Search resolves a title to TMDB results, optionally narrowed by year.
func (c *Client) Search(ctx context.Context, kind media.Kind, query string, year int) ([]Item, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year > 0 {
		if kind == media.Series {
			q.Set("first_air_date_year", strconv.Itoa(year))
		} else {
			q.Set("year", strconv.Itoa(year))
		}
	}
	return c.list(ctx, kind, "/search/"+segment(kind), q)
}

func (c *Client) list(ctx context.Context, kind media.Kind, path string, q url.Values) ([]Item, error) {
	var p page
	if err := c.hc.Get(ctx, path, q, &p); err != nil {
		return nil, err
	}
	names := c.genreNames(ctx, kind)
	items := make([]Item, 0, len(p.Results))
	for _, r := range p.Results {
		it := r.item(kind)
		for _, gid := range r.GenreIDs {
			if n, ok := names[gid]; ok {
				it.Genres = append(it.Genres, n)
			}
		}
		items = append(items, it)
	}
	return items, nil
}

// genreNames loads the genre list for kind once. A failed load returns nil
// and is retried on the next call.
func (c *Client) genreNames(ctx context.Context, kind media.Kind) map[int]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.genres[kind]; ok {
		return m
	}
	var resp struct {
		Genres []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
	}
	if err := c.hc.Get(ctx, "/genre/"+segment(kind)+"/list", nil, &resp); err != nil {
		return nil
	}
	m := make(map[int]string, len(resp.Genres))
	for _, g := range resp.Genres {
		m[g.ID] = g.Name
	}
	c.genres[kind] = m
	return m
}

type rawDetails struct {
	rawItem
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Status         string `json:"status"`
	Runtime        int    `json:"runtime"`
	EpisodeRunTime []int  `json:"episode_run_time"`
	IMDBID         string `json:"imdb_id"`
	ExternalIDs    struct {
		IMDBID string `json:"imdb_id"`
		TVDBID *int   `json:"tvdb_id"`
	} `json:"external_ids"`
	WatchProviders struct {
		Results map[string]struct {
			Flatrate []struct {
				ProviderName string `json:"provider_name"`
			} `json:"flatrate"`
		} `json:"results"`
	} `json:"watch/providers"`
}

// Details fetches one title with external ids and streaming providers for region
// (default "US").
func (c *Client) Details(ctx context.Context, kind media.Kind, id int, region string) (Details, error) {
	var r rawDetails
	q := url.Values{"append_to_response": {"watch/providers,external_ids"}}
	if err := c.hc.Get(ctx, fmt.Sprintf("/%s/%d", segment(kind), id), q, &r); err != nil {
		return Details{}, err
	}
	d := Details{Item: r.item(kind), Status: r.Status}
	for _, g := range r.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	if kind == media.Series {
		if len(r.EpisodeRunTime) > 0 {
			d.Runtime = r.EpisodeRunTime[0]
		}
	} else {
		d.Runtime = r.Runtime
	}
	d.IMDBID = r.IMDBID
	if d.IMDBID == "" {
		d.IMDBID = r.ExternalIDs.IMDBID
	}
	if r.ExternalIDs.TVDBID != nil {
		d.TVDBID = *r.ExternalIDs.TVDBID
	}
	if region == "" {
		region = "US"
	}
	seen := map[string]bool{}
	for _, p := range r.WatchProviders.Results[strings.ToUpper(region)].Flatrate {
		if p.ProviderName != "" && !seen[p.ProviderName] {
			seen[p.ProviderName] = true
			d.Providers = append(d.Providers, p.ProviderName)
		}
	}
	return d, nil
}

// FindByTVDB maps a TVDB series id to a TMDB tv id. It returns 0 when TMDB
// has no match.
func (c *Client) FindByTVDB(ctx context.Context, tvdbID int) (int, error) {
	var resp struct {
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	q := url.Values{"external_source": {"tvdb_id"}}
	if err := c.hc.Get(ctx, fmt.Sprintf("/find/%d", tvdbID), q, &resp); err != nil {
		return 0, err
	}
	if len(resp.TVResults) == 0 {
		return 0, nil
	}
	return resp.TVResults[0].ID, nil
}

// TVDBID returns the TVDB id of a TMDB tv series, or 0 when TMDB has none.
func (c *Client) TVDBID(ctx context.Context, tmdbID int) (int, error) {
	var resp struct {
		TVDBID *int `json:"tvdb_id"`
	}
	if err := c.hc.Get(ctx, fmt.Sprintf("/tv/%d/external_ids", tmdbID), nil, &resp); err != nil {
		return 0, err
	}
	if resp.TVDBID == nil {
		return 0, nil
	}
	return *resp.TVDBID, nil
}
