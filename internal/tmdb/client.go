package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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

// ErrNotFound is returned (wrapping the HTTP 404) when TMDB does not know a title.
var ErrNotFound = errors.New("tmdb: title not found")

func (c *Client) getTitle(ctx context.Context, kind media.Kind, id int, appendTo string, out any) error {
	q := url.Values{"append_to_response": {appendTo}}
	err := c.hc.Get(ctx, fmt.Sprintf("/%s/%d", segment(kind), id), q, out)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}

// Details fetches one title with external ids and streaming providers for region
// (default "US").
func (c *Client) Details(ctx context.Context, kind media.Kind, id int, region string) (Details, error) {
	var r rawDetails
	if err := c.getTitle(ctx, kind, id, "watch/providers,external_ids", &r); err != nil {
		return Details{}, err
	}
	return r.details(kind, region), nil
}

func (r rawDetails) details(kind media.Kind, region string) Details {
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
	return d
}

type rawFull struct {
	rawDetails
	Tagline          string `json:"tagline"`
	BackdropPath     string `json:"backdrop_path"`
	NumberOfSeasons  int    `json:"number_of_seasons"`
	NumberOfEpisodes int    `json:"number_of_episodes"`
	CreatedBy        []struct {
		Name string `json:"name"`
	} `json:"created_by"`
	Credits struct {
		Cast []struct {
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
			Order       int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
	Videos struct {
		Results []struct {
			Name     string `json:"name"`
			Key      string `json:"key"`
			Site     string `json:"site"`
			Type     string `json:"type"`
			Official bool   `json:"official"`
		} `json:"results"`
	} `json:"videos"`
}

// FullDetails fetches one title with credits, videos, streaming providers for
// region (default "US") and external ids. An unknown id returns ErrNotFound.
func (c *Client) FullDetails(ctx context.Context, kind media.Kind, id int, region string) (FullDetails, error) {
	var r rawFull
	if err := c.getTitle(ctx, kind, id, "credits,videos,watch/providers,external_ids", &r); err != nil {
		return FullDetails{}, err
	}
	d := r.details(kind, region)
	out := FullDetails{
		ID:          d.ID,
		Kind:        kind,
		Title:       d.Title,
		Year:        d.Year,
		Tagline:     r.Tagline,
		Overview:    d.Overview,
		Genres:      nonNil(d.Genres),
		Runtime:     d.Runtime,
		ReleaseDate: r.ReleaseDate,
		Status:      d.Status,
		PosterURL:   PosterURL(d.PosterPath),
		BackdropURL: imageURL(backdropBase, r.BackdropPath),
		Directors:   []string{},
		Cast:        []CastMember{},
		Streaming:   nonNil(d.Providers),
		IMDBID:      d.IMDBID,
		TVDBID:      d.TVDBID,
		Rating:      d.Rating,
		Votes:       d.Votes,
	}
	seen := map[string]bool{}
	addDirector := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out.Directors = append(out.Directors, name)
		}
	}
	if kind == media.Series {
		out.ReleaseDate = r.FirstAirDate
		out.Seasons, out.Episodes = r.NumberOfSeasons, r.NumberOfEpisodes
		for _, p := range r.CreatedBy {
			addDirector(p.Name)
		}
	} else {
		for _, p := range r.Credits.Crew {
			if p.Job == "Director" {
				addDirector(p.Name)
			}
		}
	}

	cast := r.Credits.Cast
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })
	for _, p := range cast[:min(len(cast), MaxCast)] {
		out.Cast = append(out.Cast, CastMember{Name: p.Name, Character: p.Character, ProfileURL: imageURL(profileBase, p.ProfilePath)})
	}

	// Official trailer, then any trailer, then a teaser; YouTube only.
	best := 0
	for _, v := range r.Videos.Results {
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		rank := 0
		switch {
		case v.Type == "Trailer" && v.Official:
			rank = 3
		case v.Type == "Trailer":
			rank = 2
		case v.Type == "Teaser":
			rank = 1
		}
		if rank > best {
			best = rank
			out.Trailer = &Trailer{Name: v.Name, YouTubeKey: v.Key}
		}
	}
	return out, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
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
