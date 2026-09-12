package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

// TitleDetails is the GET /api/titles/{kind}/{tmdb_id} response.
type TitleDetails struct {
	tmdb.FullDetails
	Ratings   *pipeline.Ratings `json:"ratings,omitempty"`
	InLibrary bool              `json:"in_library"`
}

const (
	titleCacheTTL  = time.Hour
	titleCacheSize = 500
)

func (s *Server) titleDetails(w http.ResponseWriter, r *http.Request) {
	kind := media.Kind(r.PathValue("kind"))
	if kind != media.Movies && kind != media.Series {
		writeError(w, http.StatusBadRequest, `kind must be "movies" or "series"`)
		return
	}
	id, err := strconv.Atoi(r.PathValue("tmdb_id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid tmdb_id")
		return
	}
	cfg := s.cfg()
	if s.o.Title == nil || cfg.TMDB.APIKey == "" {
		writeError(w, http.StatusBadRequest, "TMDB is not configured: set tmdb.api_key")
		return
	}
	region := strings.ToUpper(strings.TrimSpace(cfg.TMDB.Region))
	if region == "" {
		region = "US"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	inLibrary := make(chan bool, 1)
	go func() { inLibrary <- s.inLibrary(ctx, kind, id) }()

	key := titleKey{kind: kind, id: id, region: region}
	e, ok := s.titles.get(key, s.now())
	if !ok {
		e, err = s.fetchTitle(ctx, key)
		switch {
		case errors.Is(err, tmdb.ErrNotFound):
			noun := "movie"
			if kind == media.Series {
				noun = "series"
			}
			writeError(w, http.StatusNotFound, fmt.Sprintf("TMDB has no %s with id %d", noun, id))
			return
		case err != nil:
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.titles.put(key, e)
	}

	out := TitleDetails{FullDetails: e.details, Ratings: e.ratings, InLibrary: <-inLibrary}
	out.Genres = nonNil(out.Genres)
	out.Directors = nonNil(out.Directors)
	out.Cast = nonNil(out.Cast)
	out.Streaming = nonNil(out.Streaming)
	writeJSON(w, http.StatusOK, out)
}

// fetchTitle loads the TMDB details and the ratings concurrently.
func (s *Server) fetchTitle(ctx context.Context, k titleKey) (titleEntry, error) {
	ratings := make(chan *pipeline.Ratings, 1)
	go func() {
		if s.o.Ratings == nil {
			ratings <- nil
			return
		}
		r, err := s.o.Ratings(ctx, k.kind, k.id)
		if err != nil {
			s.log.Warn("title ratings", "kind", k.kind, "tmdb_id", k.id, "err", err)
			r = nil
		}
		ratings <- r
	}()
	d, err := s.o.Title(ctx, k.kind, k.id, k.region)
	if err != nil {
		return titleEntry{}, err
	}
	return titleEntry{details: d, ratings: <-ratings, at: s.now()}, nil
}

// inLibrary reports whether the library snapshot holds the title; false when
// the library is unavailable.
func (s *Server) inLibrary(ctx context.Context, kind media.Kind, id int) bool {
	if s.o.Library == nil {
		return false
	}
	titles, err := s.o.Library(ctx, kind)
	if err != nil {
		return false
	}
	for _, t := range titles {
		if t.TMDBID == id {
			return true
		}
	}
	return false
}

type titleKey struct {
	kind   media.Kind
	id     int
	region string
}

type titleEntry struct {
	details tmdb.FullDetails
	ratings *pipeline.Ratings
	at      time.Time
}

// titleCache keeps title lookups for ttl. When it holds max entries, adding
// one evicts the oldest. It is safe for concurrent use.
type titleCache struct {
	ttl time.Duration
	max int

	mu      sync.Mutex
	entries map[titleKey]titleEntry
}

func newTitleCache(ttl time.Duration, max int) *titleCache {
	return &titleCache{ttl: ttl, max: max, entries: map[titleKey]titleEntry{}}
}

func (c *titleCache) get(k titleKey, now time.Time) (titleEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || now.Sub(e.at) >= c.ttl {
		return titleEntry{}, false
	}
	return e, true
}

func (c *titleCache) put(k titleKey, e titleEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; !ok && len(c.entries) >= c.max {
		var (
			oldest titleKey
			found  bool
		)
		for key, v := range c.entries {
			if !found || v.at.Before(c.entries[oldest].at) {
				oldest, found = key, true
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[k] = e
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
