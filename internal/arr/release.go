package arr

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/httpjson"
)

// ErrNotFound is a title the app's library does not have.
var ErrNotFound = errors.New("not found")

// searchTimeout bounds an interactive search, which waits on every indexer.
const searchTimeout = 3 * time.Minute

// Radarr language ids with a special meaning in a quality profile.
const (
	LanguageAny      = -1
	LanguageOriginal = -2
)

// ProfileRules are the parts of a quality profile that decide whether a release is grabbed.
type ProfileRules struct {
	Qualities      map[int]int // allowed quality id -> preference, higher is preferred; a group shares one
	FormatScores   map[int]int // custom format id -> score
	MinFormatScore int
	Language       int // a language id, LanguageAny or LanguageOriginal
}

// Score is the custom format score of a release matching formats.
func (r *ProfileRules) Score(formats []int) int {
	total := 0
	for _, f := range formats {
		total += r.FormatScores[f]
	}
	return total
}

type profileItem struct {
	Quality *struct {
		ID int `json:"id"`
	} `json:"quality"`
	Allowed bool          `json:"allowed"`
	Items   []profileItem `json:"items"`
}

// UnmarshalJSON reads a quality profile with its rules. Rules stay nil when
// the resource lists no quality items.
func (p *QualityProfile) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID          int           `json:"id"`
		Name        string        `json:"name"`
		Items       []profileItem `json:"items"`
		FormatItems []struct {
			Format int `json:"format"`
			Score  int `json:"score"`
		} `json:"formatItems"`
		MinFormatScore int `json:"minFormatScore"`
		Language       *struct {
			ID int `json:"id"`
		} `json:"language"` // Radarr only
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*p = QualityProfile{ID: raw.ID, Name: raw.Name}
	if len(raw.Items) == 0 {
		return nil
	}
	rules := &ProfileRules{Qualities: map[int]int{}, FormatScores: map[int]int{}, MinFormatScore: raw.MinFormatScore, Language: LanguageAny}
	// Items are listed least preferred first; for a group only its own allowed flag counts.
	for pref, it := range raw.Items {
		if !it.Allowed {
			continue
		}
		if it.Quality != nil {
			rules.Qualities[it.Quality.ID] = pref
		}
		for _, sub := range it.Items {
			if sub.Quality != nil {
				rules.Qualities[sub.Quality.ID] = pref
			}
		}
	}
	for _, f := range raw.FormatItems {
		if f.Score != 0 {
			rules.FormatScores[f.Format] = f.Score
		}
	}
	if raw.Language != nil {
		rules.Language = raw.Language.ID
	}
	p.Rules = rules
	return nil
}

// Movie is a movie in the Radarr library.
type Movie struct {
	ID               int
	TMDBID           int
	Title            string
	Year             int
	QualityProfileID int
	OriginalLanguage int  // language id, 0 when unknown
	Available        bool // released as far as the movie's minimum availability requires
}

// Movie returns the library movie with Radarr id id, or ErrNotFound.
func (r *Radarr) Movie(ctx context.Context, id int) (Movie, error) {
	var m struct {
		ID               int    `json:"id"`
		TMDBID           int    `json:"tmdbId"`
		Title            string `json:"title"`
		Year             int    `json:"year"`
		QualityProfileID int    `json:"qualityProfileId"`
		IsAvailable      bool   `json:"isAvailable"`
		OriginalLanguage struct {
			ID int `json:"id"`
		} `json:"originalLanguage"`
	}
	err := r.c.get(ctx, "/api/v3/movie/"+strconv.Itoa(id), nil, &m)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return Movie{}, ErrNotFound
	}
	if err != nil {
		return Movie{}, err
	}
	return Movie{ID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year, QualityProfileID: m.QualityProfileID,
		OriginalLanguage: m.OriginalLanguage.ID, Available: m.IsAvailable}, nil
}

// SetQualityProfile moves a library movie to another quality profile.
func (r *Radarr) SetQualityProfile(ctx context.Context, movieID, profileID int) error {
	return r.c.put(ctx, "/api/v3/movie/editor", map[string]any{"movieIds": []int{movieID}, "qualityProfileId": profileID}, nil)
}

// Command is a Radarr command, such as a movie search.
type Command struct {
	ID       int
	Name     string
	Status   string // queued, started, completed, failed, aborted, cancelled or orphaned
	MovieIDs []int
}

// Done reports whether the command has stopped running.
func (c Command) Done() bool { return c.Status != "queued" && c.Status != "started" }

type commandJSON struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Body   struct {
		MovieIDs []int `json:"movieIds"`
	} `json:"body"`
}

func (c commandJSON) command() Command {
	return Command{ID: c.ID, Name: c.Name, Status: c.Status, MovieIDs: c.Body.MovieIDs}
}

// SearchMovie queues Radarr's automatic search for a library movie, like its
// Search Movie button: Radarr grabs the best release that fits the profile.
func (r *Radarr) SearchMovie(ctx context.Context, movieID int) (Command, error) {
	var c commandJSON
	err := r.c.post(ctx, "/api/v3/command", map[string]any{"name": "MoviesSearch", "movieIds": []int{movieID}}, &c)
	return c.command(), err
}

// Command returns a queued or recently finished command.
func (r *Radarr) Command(ctx context.Context, id int) (Command, error) {
	var c commandJSON
	err := r.c.get(ctx, "/api/v3/command/"+strconv.Itoa(id), nil, &c)
	return c.command(), err
}

// MovieSearches lists the queued and recently finished searches for a movie, newest first.
func (r *Radarr) MovieSearches(ctx context.Context, movieID int) ([]Command, error) {
	var cs []commandJSON
	if err := r.c.get(ctx, "/api/v3/command", nil, &cs); err != nil {
		return nil, err
	}
	var out []Command
	for _, c := range cs {
		if c.Name == "MoviesSearch" && slices.Contains(c.Body.MovieIDs, movieID) {
			out = append(out, c.command())
		}
	}
	slices.SortFunc(out, func(a, b Command) int { return cmp.Compare(b.ID, a.ID) })
	return out, nil
}

type qualityJSON struct {
	Quality struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"quality"`
}

// Grab is a release Radarr sent to the download client, or holds for a delay profile.
type Grab struct {
	Title    string
	Quality  string
	Size     int64 // 0 when unknown
	Indexer  string
	Protocol string // torrent | usenet
}

type historyJSON struct {
	ID          int               `json:"id"`
	EventType   string            `json:"eventType"`
	SourceTitle string            `json:"sourceTitle"`
	Quality     qualityJSON       `json:"quality"`
	Data        map[string]string `json:"data"`
}

// Grabs lists the releases Radarr grabbed for a movie, newest first.
func (r *Radarr) Grabs(ctx context.Context, movieID int) ([]Grab, error) {
	var hs []historyJSON
	q := url.Values{"movieId": {strconv.Itoa(movieID)}, "eventType": {"grabbed"}}
	if err := r.c.get(ctx, "/api/v3/history/movie", q, &hs); err != nil {
		return nil, err
	}
	slices.SortFunc(hs, func(a, b historyJSON) int { return cmp.Compare(b.ID, a.ID) })
	out := []Grab{}
	for _, h := range hs {
		if h.EventType != "grabbed" {
			continue
		}
		size, _ := strconv.ParseInt(h.Data["size"], 10, 64)
		out = append(out, Grab{Title: h.SourceTitle, Quality: h.Quality.Quality.Name, Size: size, Indexer: h.Data["indexer"], Protocol: historyProtocol(h.Data["protocol"])})
	}
	return out, nil
}

// historyProtocol reads the protocol of a history item, stored as Radarr's enum number.
func historyProtocol(p string) string {
	switch p {
	case "1", "usenet":
		return "usenet"
	case "2", "torrent":
		return "torrent"
	}
	return ""
}

// Pending lists the releases Radarr holds for a movie because of a delay profile.
func (r *Radarr) Pending(ctx context.Context, movieID int) ([]Grab, error) {
	var page struct {
		Records []struct {
			MovieID  int         `json:"movieId"`
			Status   string      `json:"status"`
			Title    string      `json:"title"`
			Size     float64     `json:"size"`
			Indexer  string      `json:"indexer"`
			Protocol string      `json:"protocol"`
			Quality  qualityJSON `json:"quality"`
		} `json:"records"`
	}
	q := url.Values{"movieIds": {strconv.Itoa(movieID)}, "pageSize": {"100"}}
	if err := r.c.get(ctx, "/api/v3/queue", q, &page); err != nil {
		return nil, err
	}
	var out []Grab
	for _, rec := range page.Records {
		if rec.MovieID == movieID && strings.EqualFold(rec.Status, "delay") {
			out = append(out, Grab{Title: rec.Title, Quality: rec.Quality.Quality.Name, Size: int64(rec.Size), Indexer: rec.Indexer, Protocol: rec.Protocol})
		}
	}
	return out, nil
}

// Release is one result of an interactive search, judged by Radarr against
// the movie's current quality profile.
type Release struct {
	GUID          string
	IndexerID     int
	Indexer       string
	Title         string
	Size          int64
	Protocol      string // torrent | usenet
	Seeders       int    // torrents only
	QualityID     int
	Quality       string
	CustomFormats []int
	Languages     []int
	Approved      bool // Radarr would grab it for the movie as it is now
	Rejections    []string
}

// Releases runs an interactive search for a library movie: Radarr queries the
// indexers and judges each release against the movie's quality profile, but
// grabs nothing. Releases come in Radarr's order of preference.
func (r *Radarr) Releases(ctx context.Context, movieID int) ([]Release, error) {
	type id struct {
		ID int `json:"id"`
	}
	var rs []struct {
		GUID            string      `json:"guid"`
		IndexerID       int         `json:"indexerId"`
		Indexer         string      `json:"indexer"`
		Title           string      `json:"title"`
		Size            int64       `json:"size"`
		Protocol        string      `json:"protocol"`
		Seeders         *int        `json:"seeders"`
		Quality         qualityJSON `json:"quality"`
		CustomFormats   []id        `json:"customFormats"`
		Languages       []id        `json:"languages"`
		Approved        bool        `json:"approved"`
		DownloadAllowed *bool       `json:"downloadAllowed"`
		Rejections      []string    `json:"rejections"`
	}
	if err := r.search.get(ctx, "/api/v3/release", url.Values{"movieId": {strconv.Itoa(movieID)}}, &rs); err != nil {
		return nil, err
	}
	ids := func(xs []id) []int {
		out := make([]int, len(xs))
		for i, x := range xs {
			out[i] = x.ID
		}
		return out
	}
	out := make([]Release, 0, len(rs))
	for _, x := range rs {
		q := x.Quality.Quality
		rel := Release{
			GUID: x.GUID, IndexerID: x.IndexerID, Indexer: x.Indexer, Title: x.Title, Size: x.Size, Protocol: x.Protocol,
			QualityID: q.ID, Quality: q.Name,
			CustomFormats: ids(x.CustomFormats), Languages: ids(x.Languages),
			Approved:   x.Approved && (x.DownloadAllowed == nil || *x.DownloadAllowed),
			Rejections: x.Rejections,
		}
		if x.Seeders != nil {
			rel.Seeders = *x.Seeders
		}
		out = append(out, rel)
	}
	return out, nil
}

// withTimeout copies hc with a timeout of at least d.
func withTimeout(hc *http.Client, d time.Duration) *http.Client {
	if hc == nil {
		return &http.Client{Timeout: d}
	}
	c := *hc
	if c.Timeout != 0 && c.Timeout < d {
		c.Timeout = d
	}
	return &c
}
