package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

// Sonarr is a Sonarr v3 API client.
type Sonarr struct{ c client }

// NewSonarr returns a client for the Sonarr instance at baseURL.
func NewSonarr(baseURL, apiKey string, hc *http.Client) *Sonarr {
	return &Sonarr{client{app: "sonarr", hc: newHTTP(baseURL, apiKey, hc)}}
}

// Status returns the system status, used as a connection test.
func (s *Sonarr) Status(ctx context.Context) (SystemStatus, error) { return s.c.status(ctx) }

// QualityProfiles lists the configured quality profiles.
func (s *Sonarr) QualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	return s.c.qualityProfiles(ctx)
}

// RootFolders lists the configured root folders.
func (s *Sonarr) RootFolders(ctx context.Context) ([]RootFolder, error) { return s.c.rootFolders(ctx) }

// Series returns every series in the library. TMDBID may be 0.
func (s *Sonarr) Series(ctx context.Context) ([]media.Title, error) {
	var ss []struct {
		ID     int       `json:"id"`
		Title  string    `json:"title"`
		Year   int       `json:"year"`
		TVDBID int       `json:"tvdbId"`
		TMDBID int       `json:"tmdbId"`
		Genres []string  `json:"genres"`
		Added  time.Time `json:"added"`
		Images []image   `json:"images"`
	}
	if err := s.c.get(ctx, "/api/v3/series", nil, &ss); err != nil {
		return nil, err
	}
	out := make([]media.Title, 0, len(ss))
	for _, x := range ss {
		out = append(out, media.Title{TMDBID: x.TMDBID, TVDBID: x.TVDBID, Title: x.Title, Year: x.Year, Genres: x.Genres, Added: x.Added, PosterURL: posterURL(x.Images)})
	}
	return out, nil
}

// Lookup resolves a TVDB id to a Sonarr series resource ready to add.
func (s *Sonarr) Lookup(ctx context.Context, tvdbID int) (*Lookup, error) {
	var raws []json.RawMessage
	if err := s.c.get(ctx, "/api/v3/series/lookup", url.Values{"term": {fmt.Sprintf("tvdb:%d", tvdbID)}}, &raws); err != nil {
		return nil, err
	}
	type typed struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Year    int    `json:"year"`
		TVDBID  int    `json:"tvdbId"`
		TMDBID  int    `json:"tmdbId"`
		Ratings rating `json:"ratings"` // the IMDb rating
	}
	pick := -1
	var picked typed
	for i, raw := range raws {
		var t typed
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		if pick < 0 || t.TVDBID == tvdbID {
			pick, picked = i, t
			if t.TVDBID == tvdbID {
				break
			}
		}
	}
	if pick < 0 {
		return nil, fmt.Errorf("sonarr: series tvdb:%d not found", tvdbID)
	}
	var m map[string]any
	if err := json.Unmarshal(raws[pick], &m); err != nil {
		return nil, fmt.Errorf("sonarr: decode lookup tvdb:%d: %w", tvdbID, err)
	}
	var ratings Ratings
	if picked.Ratings.Value > 0 {
		ratings.IMDB, ratings.IMDBVotes = picked.Ratings.Value, picked.Ratings.Votes
	}
	return &Lookup{LibraryID: picked.ID, Title: picked.Title, Year: picked.Year, TMDBID: picked.TMDBID, TVDBID: picked.TVDBID, Ratings: ratings, Raw: m}, nil
}

// Add adds a looked-up series with the chosen quality profile and root folder.
func (s *Sonarr) Add(ctx context.Context, l *Lookup, o AddOptions) (Added, error) {
	if err := validateAdd("sonarr", l, o); err != nil {
		return Added{}, err
	}
	body := copyRaw(l.Raw)
	body["qualityProfileId"] = o.QualityProfileID
	body["rootFolderPath"] = o.RootFolderPath
	body["monitored"] = true
	body["seasonFolder"] = true
	body["addOptions"] = map[string]any{"monitor": "all", "searchForMissingEpisodes": o.Search}

	var res added
	if err := s.c.post(ctx, "/api/v3/series", body, &res); err != nil {
		return Added{}, err
	}
	return Added{ID: res.ID, Title: res.Title}, nil
}
