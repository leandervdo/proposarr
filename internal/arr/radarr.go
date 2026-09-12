package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/leandervdo/proposarr/internal/httpjson"
	"github.com/leandervdo/proposarr/internal/media"
)

// Radarr is a Radarr v3 API client. Release searches and grabs use search,
// which allows for slow indexers.
type Radarr struct{ c, search client }

// NewRadarr returns a client for the Radarr instance at baseURL.
func NewRadarr(baseURL, apiKey string, hc *http.Client) *Radarr {
	return &Radarr{
		c:      client{app: "radarr", hc: newHTTP(baseURL, apiKey, hc)},
		search: client{app: "radarr", hc: newHTTP(baseURL, apiKey, withTimeout(hc, searchTimeout))},
	}
}

// Status returns the system status, used as a connection test.
func (r *Radarr) Status(ctx context.Context) (SystemStatus, error) { return r.c.status(ctx) }

// QualityProfiles lists the configured quality profiles.
func (r *Radarr) QualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	return r.c.qualityProfiles(ctx)
}

// RootFolders lists the configured root folders.
func (r *Radarr) RootFolders(ctx context.Context) ([]RootFolder, error) { return r.c.rootFolders(ctx) }

// Movies returns every movie in the library.
func (r *Radarr) Movies(ctx context.Context) ([]media.Title, error) {
	var ms []struct {
		ID     int       `json:"id"`
		Title  string    `json:"title"`
		Year   int       `json:"year"`
		TMDBID int       `json:"tmdbId"`
		Genres []string  `json:"genres"`
		Added  time.Time `json:"added"`
		Images []image   `json:"images"`
	}
	if err := r.c.get(ctx, "/api/v3/movie", nil, &ms); err != nil {
		return nil, err
	}
	out := make([]media.Title, 0, len(ms))
	for _, m := range ms {
		out = append(out, media.Title{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year, Genres: m.Genres, Added: m.Added, PosterURL: posterURL(m.Images)})
	}
	return out, nil
}

// image is an *arr media cover.
type image struct {
	CoverType string `json:"coverType"`
	URL       string `json:"url"`
	RemoteURL string `json:"remoteUrl"`
}

// posterURL returns the poster's remote URL. The local /MediaCover URL needs
// the *arr's own auth, so it is only used when it is absolute.
func posterURL(images []image) string {
	for _, im := range images {
		if im.CoverType != "poster" {
			continue
		}
		if im.RemoteURL != "" {
			return sizedPoster(im.RemoteURL)
		}
		if u, err := url.Parse(im.URL); err == nil && u.IsAbs() {
			return im.URL
		}
	}
	return ""
}

// Discover returns Radarr's import-list recommendations not yet in the library.
func (r *Radarr) Discover(ctx context.Context) ([]ListMovie, error) {
	q := url.Values{
		"includeRecommendations": {"true"},
		"includeTrending":        {"false"},
		"includePopular":         {"false"},
	}
	var items []struct {
		TMDBID     int      `json:"tmdbId"`
		Title      string   `json:"title"`
		Year       int      `json:"year"`
		Overview   string   `json:"overview"`
		Genres     []string `json:"genres"`
		IsExisting bool     `json:"isExisting"`
		IsExcluded bool     `json:"isExcluded"`
		Ratings    struct {
			TMDB struct {
				Value float64 `json:"value"`
				Votes int     `json:"votes"`
			} `json:"tmdb"`
		} `json:"ratings"`
	}
	if err := r.c.get(ctx, "/api/v3/importlist/movie", q, &items); err != nil {
		return nil, err
	}
	out := make([]ListMovie, 0, len(items))
	for _, it := range items {
		if it.IsExisting || it.IsExcluded || it.TMDBID == 0 {
			continue
		}
		out = append(out, ListMovie{
			TMDBID:   it.TMDBID,
			Title:    it.Title,
			Year:     it.Year,
			Overview: it.Overview,
			Genres:   it.Genres,
			Rating:   it.Ratings.TMDB.Value,
			Votes:    it.Ratings.TMDB.Votes,
		})
	}
	return out, nil
}

// Lookup resolves a TMDB id to a Radarr movie resource ready to add.
func (r *Radarr) Lookup(ctx context.Context, tmdbID int) (*Lookup, error) {
	var raw json.RawMessage
	err := r.c.get(ctx, "/api/v3/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, &raw)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return nil, fmt.Errorf("radarr: movie tmdb:%d not found", tmdbID)
	}
	if err != nil {
		return nil, err
	}
	var typed struct {
		ID               int    `json:"id"`
		Title            string `json:"title"`
		Year             int    `json:"year"`
		TMDBID           int    `json:"tmdbId"`
		OriginalLanguage struct {
			ID int `json:"id"`
		} `json:"originalLanguage"`
		Ratings struct {
			IMDB           rating `json:"imdb"`
			RottenTomatoes rating `json:"rottenTomatoes"`
			Metacritic     rating `json:"metacritic"`
		} `json:"ratings"`
	}
	var m map[string]any
	if json.Unmarshal(raw, &typed) != nil || json.Unmarshal(raw, &m) != nil || typed.TMDBID == 0 {
		return nil, fmt.Errorf("radarr: movie tmdb:%d not found", tmdbID)
	}
	libraryID := typed.ID
	if libraryID == 0 {
		// Radarr 6 leaves the library id out of a lookup.
		if libraryID, err = r.libraryID(ctx, tmdbID); err != nil {
			return nil, err
		}
	}
	rt := typed.Ratings
	ratings := Ratings{RottenTomatoes: percent(rt.RottenTomatoes.Value), Metacritic: percent(rt.Metacritic.Value)}
	if rt.IMDB.Value > 0 {
		ratings.IMDB, ratings.IMDBVotes = rt.IMDB.Value, rt.IMDB.Votes
	}
	return &Lookup{LibraryID: libraryID, Title: typed.Title, Year: typed.Year, TMDBID: typed.TMDBID,
		OriginalLanguage: typed.OriginalLanguage.ID, Ratings: ratings, Raw: m}, nil
}

// libraryID is the Radarr id of the library movie with a TMDB id, 0 when there is none.
func (r *Radarr) libraryID(ctx context.Context, tmdbID int) (int, error) {
	m, err := r.MovieByTMDB(ctx, tmdbID)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	return m.ID, err
}

// percent rounds a 0-100 rating; anything outside that range is unknown.
func percent(v float64) int {
	if v <= 0 || v > 100 {
		return 0
	}
	return int(math.Round(v))
}

// Add adds a looked-up movie with the chosen quality profile and root folder.
func (r *Radarr) Add(ctx context.Context, l *Lookup, o AddOptions) (Added, error) {
	if err := validateAdd("radarr", l, o); err != nil {
		return Added{}, err
	}
	avail := o.MinimumAvailability
	if avail == "" {
		avail = "released"
	}
	body := copyRaw(l.Raw)
	body["qualityProfileId"] = o.QualityProfileID
	body["rootFolderPath"] = o.RootFolderPath
	body["monitored"] = true
	body["minimumAvailability"] = avail
	body["addOptions"] = map[string]any{"searchForMovie": o.Search, "monitor": "movieOnly"}

	var res added
	if err := r.c.post(ctx, "/api/v3/movie", body, &res); err != nil {
		return Added{}, alreadyAdded(err)
	}
	return Added{ID: res.ID, Title: res.Title}, nil
}
