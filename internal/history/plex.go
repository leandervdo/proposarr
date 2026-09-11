package history

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/httpjson"
	"github.com/leandervdo/proposarr/internal/media"
)

const (
	plexPageSize    = 500
	plexConcurrency = 6
	plexMetaPrefix  = "/library/metadata/"
)

// Plex reads the server-wide watch history of every account.
type Plex struct {
	c *httpjson.Client
}

// NewPlex returns a Plex history source. hc may be nil.
func NewPlex(baseURL, token string, hc *http.Client) *Plex {
	return &Plex{c: &httpjson.Client{
		BaseURL: baseURL,
		HTTP:    hc,
		Header:  http.Header{"X-Plex-Token": {token}},
	}}
}

func (p *Plex) Name() string { return "plex" }

// Ping checks the server is reachable and the token is accepted.
func (p *Plex) Ping(ctx context.Context) error {
	return p.c.Get(ctx, "/identity", nil, nil)
}

// flexString decodes a JSON string or number, since Plex is inconsistent about keys.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flexString(b)
	return nil
}

type plexItem struct {
	RatingKey flexString `json:"ratingKey"`
	Key       string     `json:"key"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Year      int        `json:"year"`
	// History items carry a release date instead of year.
	OriginallyAvailableAt string `json:"originallyAvailableAt"`
	GrandparentTitle      string `json:"grandparentTitle"`
	GrandparentKey        string `json:"grandparentKey"`
	ViewedAt              int64  `json:"viewedAt"`
	LeafCount             int    `json:"leafCount"`
	// Metadata has both "guid" (a plex:// string) and "Guid" (external ids).
	// encoding/json matches names case-insensitively, so without this field the
	// string lands in Guid and the whole decode fails.
	PlexGUID string `json:"guid"`
	Guid     []struct {
		ID string `json:"id"`
	} `json:"Guid"`
}

func (it plexItem) year() int {
	if it.Year > 0 {
		return it.Year
	}
	if len(it.OriginallyAvailableAt) >= 4 {
		if y, err := strconv.Atoi(it.OriginallyAvailableAt[:4]); err == nil {
			return y
		}
	}
	return 0
}

type plexContainer struct {
	MediaContainer struct {
		Metadata []plexItem `json:"Metadata"`
	} `json:"MediaContainer"`
}

// plexAgg accumulates one title's history before ids are resolved.
type plexAgg struct {
	entry    Entry
	metaPath string
	episodes map[string]int // series: plays per episode
	total    int            // series: leafCount from metadata
}

// History aggregates the history window into one entry per title.
func (p *Plex) History(ctx context.Context, kind media.Kind, since time.Time) ([]Entry, error) {
	items, err := p.fetch(ctx, kind, since)
	if err != nil {
		return nil, err
	}

	var aggs []*plexAgg
	byKey := map[string]*plexAgg{}
	for _, it := range items {
		viewed := time.Unix(it.ViewedAt, 0).UTC()
		var key, title, metaPath string
		if kind == media.Series {
			key, title = it.GrandparentKey, it.GrandparentTitle
			if key == "" {
				key = "title:" + media.NormTitle(title)
			} else if strings.HasPrefix(key, plexMetaPrefix) {
				metaPath = key
			}
		} else {
			key, title = string(it.RatingKey), it.Title
			if key == "" {
				key = "title:" + media.NormTitle(title) + ":" + strconv.Itoa(it.Year)
			} else {
				metaPath = plexMetaPrefix + url.PathEscape(key)
			}
		}
		if title == "" {
			continue
		}
		a := byKey[key]
		if a == nil {
			a = &plexAgg{entry: Entry{Kind: kind, Title: title}, metaPath: metaPath}
			if kind == media.Movies {
				a.entry.Year = it.year()
			} else {
				a.episodes = map[string]int{}
			}
			byKey[key] = a
			aggs = append(aggs, a)
		}
		a.entry.Plays++
		if viewed.After(a.entry.LastWatched) {
			a.entry.LastWatched = viewed
		}
		if kind == media.Series {
			ep := string(it.RatingKey)
			if ep == "" {
				ep = it.Key
			}
			if ep == "" {
				ep = it.Title
			}
			a.episodes[ep]++
		}
	}

	p.resolve(ctx, aggs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(aggs))
	for _, a := range aggs {
		e := a.entry
		if kind == media.Series {
			maxPlays := 0
			for _, n := range a.episodes {
				maxPlays = max(maxPlays, n)
			}
			e.Episodes = len(a.episodes)
			e.Signal = seriesSignal(e.Episodes, a.total, maxPlays)
		} else {
			e.Signal = movieSignal(e.Plays)
		}
		entries = append(entries, e)
	}
	return Merge(entries), nil
}

// plexIsKind filters history client-side: the history endpoint ignores its
// type parameter (type=1 returns episodes too, type=4 returns nothing).
func plexIsKind(kind media.Kind, it plexItem) bool {
	switch it.Type {
	case "movie":
		return kind == media.Movies
	case "episode":
		return kind == media.Series
	case "":
		return (it.GrandparentTitle != "") == (kind == media.Series)
	}
	return false
}

func (p *Plex) fetch(ctx context.Context, kind media.Kind, since time.Time) ([]plexItem, error) {
	cutoff := since.Unix()
	var out []plexItem
	for start := 0; ; {
		q := url.Values{
			"sort":                   {"viewedAt:desc"},
			"viewedAt>":              {strconv.FormatInt(cutoff, 10)},
			"X-Plex-Container-Start": {strconv.Itoa(start)},
			"X-Plex-Container-Size":  {strconv.Itoa(plexPageSize)},
		}
		var page plexContainer
		if err := p.c.Get(ctx, "/status/sessions/history/all", q, &page); err != nil {
			return nil, err
		}
		items := page.MediaContainer.Metadata
		older := false
		for _, it := range items {
			if !since.IsZero() && it.ViewedAt < cutoff {
				older = true
				continue
			}
			if plexIsKind(kind, it) {
				out = append(out, it)
			}
		}
		start += len(items)
		// A page larger than requested means the server ignored paging.
		if len(items) != plexPageSize || older {
			return out, nil
		}
	}
}

// resolve fills ids, year and episode totals from item metadata. Failures
// leave the entry as history reported it.
func (p *Plex) resolve(ctx context.Context, aggs []*plexAgg) {
	sem := make(chan struct{}, plexConcurrency)
	var wg sync.WaitGroup
	for _, a := range aggs {
		if a.metaPath == "" {
			continue
		}
		wg.Add(1)
		go func(a *plexAgg) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			var mc plexContainer
			if err := p.c.Get(ctx, a.metaPath, url.Values{"includeGuids": {"1"}}, &mc); err != nil {
				return
			}
			if len(mc.MediaContainer.Metadata) == 0 {
				return
			}
			m := mc.MediaContainer.Metadata[0]
			if m.Title != "" {
				a.entry.Title = m.Title
			}
			if m.Year > 0 {
				a.entry.Year = m.Year
			}
			a.total = m.LeafCount
			for _, g := range m.Guid {
				scheme, id, ok := strings.Cut(g.ID, "://")
				if !ok {
					continue
				}
				n, err := strconv.Atoi(id)
				if err != nil {
					continue
				}
				switch scheme {
				case "tmdb":
					a.entry.TMDBID = n
				case "tvdb":
					a.entry.TVDBID = n
				}
			}
		}(a)
	}
	wg.Wait()
}
