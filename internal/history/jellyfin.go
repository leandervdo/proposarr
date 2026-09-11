package history

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/httpjson"
	"github.com/leandervdo/proposarr/internal/media"
)

const (
	jellyfinPageSize = 200
	jellyfinIDsBatch = 50
)

// Jellyfin reads played items for one user, or merges every user.
type Jellyfin struct {
	c      *httpjson.Client
	userID string
}

// NewJellyfin returns a Jellyfin history source. An empty userID merges all users. hc may be nil.
func NewJellyfin(baseURL, apiKey, userID string, hc *http.Client) *Jellyfin {
	return &Jellyfin{
		c: &httpjson.Client{
			BaseURL: baseURL,
			HTTP:    hc,
			Header:  http.Header{"Authorization": {`MediaBrowser Token="` + apiKey + `"`}},
		},
		userID: userID,
	}
}

func (j *Jellyfin) Name() string { return "jellyfin" }

// Ping checks the server is reachable and the key is accepted.
func (j *Jellyfin) Ping(ctx context.Context) error {
	return j.c.Get(ctx, "/System/Info", nil, nil)
}

type jfUserData struct {
	PlayCount         int    `json:"PlayCount"`
	LastPlayedDate    string `json:"LastPlayedDate"`
	Played            bool   `json:"Played"`
	UnplayedItemCount int    `json:"UnplayedItemCount"`
}

type jfItem struct {
	ID             string            `json:"Id"`
	Name           string            `json:"Name"`
	ProductionYear int               `json:"ProductionYear"`
	ProviderIds    map[string]string `json:"ProviderIds"`
	SeriesID       string            `json:"SeriesId"`
	SeriesName     string            `json:"SeriesName"`
	UserData       jfUserData        `json:"UserData"`
}

type jfItems struct {
	Items            []jfItem `json:"Items"`
	TotalRecordCount int      `json:"TotalRecordCount"`
}

// History merges the history window of the configured user, or every user.
func (j *Jellyfin) History(ctx context.Context, kind media.Kind, since time.Time) ([]Entry, error) {
	users, err := j.users(ctx)
	if err != nil {
		return nil, err
	}
	var lists [][]Entry
	for _, uid := range users {
		var entries []Entry
		if kind == media.Series {
			entries, err = j.series(ctx, uid, since)
		} else {
			entries, err = j.movies(ctx, uid, since)
		}
		if err != nil {
			return nil, err
		}
		lists = append(lists, entries)
	}
	return Merge(lists...), nil
}

func (j *Jellyfin) users(ctx context.Context) ([]string, error) {
	if j.userID != "" {
		return []string{j.userID}, nil
	}
	var users []struct {
		ID string `json:"Id"`
	}
	if err := j.c.Get(ctx, "/Users", nil, &users); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(users))
	for _, u := range users {
		if u.ID != "" {
			ids = append(ids, u.ID)
		}
	}
	return ids, nil
}

func (j *Jellyfin) movies(ctx context.Context, uid string, since time.Time) ([]Entry, error) {
	played, err := j.recent(ctx, uid, "Movie", "IsPlayed", since)
	if err != nil {
		return nil, err
	}
	resumable, err := j.recent(ctx, uid, "Movie", "IsResumable", since)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(played)+len(resumable))
	for _, it := range played {
		plays := max(it.UserData.PlayCount, 1)
		entries = append(entries, j.movieEntry(it, plays, movieSignal(plays)))
	}
	for _, it := range resumable {
		entries = append(entries, j.movieEntry(it, 0, Partial))
	}
	return entries, nil
}

func (j *Jellyfin) movieEntry(it jfItem, plays int, sig Signal) Entry {
	return Entry{
		Kind:        media.Movies,
		TMDBID:      providerID(it.ProviderIds, "tmdb"),
		TVDBID:      providerID(it.ProviderIds, "tvdb"),
		Title:       it.Name,
		Year:        it.ProductionYear,
		Plays:       plays,
		LastWatched: parseJellyfinTime(it.UserData.LastPlayedDate),
		Signal:      sig,
	}
}

type jfSeriesAgg struct {
	title    string
	episodes map[string]int
	plays    int
	last     time.Time
}

func (j *Jellyfin) series(ctx context.Context, uid string, since time.Time) ([]Entry, error) {
	episodes, err := j.recent(ctx, uid, "Episode", "IsPlayed", since)
	if err != nil {
		return nil, err
	}
	var order []string
	aggs := map[string]*jfSeriesAgg{}
	for _, ep := range episodes {
		if ep.SeriesID == "" {
			continue
		}
		a := aggs[ep.SeriesID]
		if a == nil {
			a = &jfSeriesAgg{title: ep.SeriesName, episodes: map[string]int{}}
			aggs[ep.SeriesID] = a
			order = append(order, ep.SeriesID)
		}
		plays := max(ep.UserData.PlayCount, 1)
		key := ep.ID
		if key == "" {
			key = ep.Name
		}
		a.episodes[key] += plays
		a.plays += plays
		if t := parseJellyfinTime(ep.UserData.LastPlayedDate); t.After(a.last) {
			a.last = t
		}
	}

	info := j.seriesInfo(ctx, uid, order)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(order))
	for _, id := range order {
		a := aggs[id]
		maxPlays := 0
		for _, n := range a.episodes {
			maxPlays = max(maxPlays, n)
		}
		e := Entry{
			Kind:        media.Series,
			Title:       a.title,
			Plays:       a.plays,
			Episodes:    len(a.episodes),
			LastWatched: a.last,
		}
		total := 0
		if s, ok := info[id]; ok {
			e.TMDBID = providerID(s.ProviderIds, "tmdb")
			e.TVDBID = providerID(s.ProviderIds, "tvdb")
			e.Year = s.ProductionYear
			if s.Name != "" {
				e.Title = s.Name
			}
			total = e.Episodes + s.UserData.UnplayedItemCount
		}
		e.Signal = seriesSignal(e.Episodes, total, maxPlays)
		entries = append(entries, e)
	}
	return entries, nil
}

// seriesInfo looks up series ids and unplayed counts. A failed batch leaves
// those series without ids rather than failing the history.
func (j *Jellyfin) seriesInfo(ctx context.Context, uid string, ids []string) map[string]jfItem {
	out := map[string]jfItem{}
	for start := 0; start < len(ids); start += jellyfinIDsBatch {
		batch := ids[start:min(start+jellyfinIDsBatch, len(ids))]
		q := url.Values{"Ids": {strings.Join(batch, ",")}, "Fields": {"ProviderIds"}}
		var page jfItems
		if err := j.c.Get(ctx, itemsPath(uid), q, &page); err != nil {
			continue
		}
		for _, it := range page.Items {
			out[it.ID] = it
		}
	}
	return out
}

// recent pages through items sorted by play date until one predates since.
func (j *Jellyfin) recent(ctx context.Context, uid, itemType, filter string, since time.Time) ([]jfItem, error) {
	var out []jfItem
	for start := 0; ; {
		q := url.Values{
			"IncludeItemTypes": {itemType},
			"Recursive":        {"true"},
			"Filters":          {filter},
			"SortBy":           {"DatePlayed"},
			"SortOrder":        {"Descending"},
			"Fields":           {"ProviderIds"},
			"StartIndex":       {strconv.Itoa(start)},
			"Limit":            {strconv.Itoa(jellyfinPageSize)},
		}
		var page jfItems
		if err := j.c.Get(ctx, itemsPath(uid), q, &page); err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			t := parseJellyfinTime(it.UserData.LastPlayedDate)
			if t.IsZero() || t.Before(since) {
				return out, nil
			}
			out = append(out, it)
		}
		start += len(page.Items)
		if len(page.Items) < jellyfinPageSize || (page.TotalRecordCount > 0 && start >= page.TotalRecordCount) {
			return out, nil
		}
	}
}

func itemsPath(uid string) string {
	return "/Users/" + url.PathEscape(uid) + "/Items"
}

// providerID reads a provider id regardless of key case ("Tmdb", "tmdb").
func providerID(ids map[string]string, name string) int {
	for k, v := range ids {
		if strings.EqualFold(k, name) {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

func parseJellyfinTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02T15:04:05.9999999", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
