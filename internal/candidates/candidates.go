// Package candidates gathers the titles the agent ranks.
package candidates

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

type Candidate struct {
	TMDBID     int      `json:"tmdb_id"`
	Title      string   `json:"title"`
	Year       int      `json:"year,omitempty"`
	Overview   string   `json:"overview,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Rating     float64  `json:"rating,omitempty"`
	Votes      int      `json:"votes,omitempty"`
	Popularity float64  `json:"popularity,omitempty"`
	PosterPath string   `json:"poster_path,omitempty"`
	Sources    []string `json:"sources"`
}

type Related interface {
	Recommendations(ctx context.Context, kind media.Kind, id int) ([]tmdb.Item, error)
	Similar(ctx context.Context, kind media.Kind, id int) ([]tmdb.Item, error)
}

type Discover interface {
	Discover(ctx context.Context) ([]arr.ListMovie, error)
}

type Options struct {
	Kind        media.Kind
	Seeds       int
	Cap         int
	Concurrency int
}

const DiscoverSource = "Radarr discover"

type feed struct {
	name  string
	fetch func(ctx context.Context, kind media.Kind, id int) ([]tmdb.Item, error)
}

type result struct {
	items []tmdb.Item
	err   error
}

// Build fetches every feed for the seeds, aggregates by TMDB id, filters and ranks.
// Feed failures become warnings and never fail the build.
func Build(ctx context.Context, rel Related, disc Discover, seeds []profile.Entry, excluded func(tmdbID int) bool, o Options) ([]Candidate, []string) {
	if o.Seeds <= 0 {
		o.Seeds = 15
	}
	if o.Cap <= 0 {
		o.Cap = 150
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 6
	}

	var picked []profile.Entry
	seedIDs := map[int]bool{}
	for _, s := range seeds {
		if len(picked) == o.Seeds {
			break
		}
		if s.TMDBID > 0 {
			picked = append(picked, s)
			seedIDs[s.TMDBID] = true
		}
	}

	var feeds []feed
	if rel != nil {
		feeds = append(feeds, feed{"recommendations", rel.Recommendations})
		if o.Kind == media.Series {
			feeds = append(feeds, feed{"similar", rel.Similar})
		}
	}

	results := make([][]result, len(feeds))
	for f := range feeds {
		results[f] = make([]result, len(picked))
	}

	var (
		wg        sync.WaitGroup
		sem       = make(chan struct{}, o.Concurrency)
		discItems []arr.ListMovie
		discErr   error
	)
	for f := range feeds {
		for s := range picked {
			wg.Add(1)
			go func(f, s int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				items, err := feeds[f].fetch(ctx, o.Kind, picked[s].TMDBID)
				results[f][s] = result{items, err}
			}(f, s)
		}
	}
	useDiscover := disc != nil && o.Kind == media.Movies
	if useDiscover {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			discItems, discErr = disc.Discover(ctx)
		}()
	}
	wg.Wait()

	agg := &aggregator{byID: map[int]*Candidate{}}
	var warnings []string
	for s, seed := range picked {
		label := media.Label(seed.Title, seed.Year)
		for f := range feeds {
			for _, it := range results[f][s].items {
				agg.add(fromItem(it), label)
			}
		}
	}
	for f, fd := range feeds {
		failed := 0
		var first error
		for s := range picked {
			if err := results[f][s].err; err != nil {
				failed++
				if first == nil {
					first = err
				}
			}
		}
		if failed > 0 {
			warnings = append(warnings, fmt.Sprintf("tmdb %s failed for %d of %d seeds: %v", fd.name, failed, len(picked), first))
		}
	}
	if useDiscover {
		if discErr != nil {
			warnings = append(warnings, fmt.Sprintf("Radarr discover failed: %v", discErr))
		}
		for _, m := range discItems {
			agg.add(Candidate{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year, Overview: m.Overview, Genres: m.Genres, Rating: m.Rating, Votes: m.Votes}, DiscoverSource)
		}
	}

	out := make([]Candidate, 0, len(agg.order))
	for _, c := range agg.order {
		if c.TMDBID == 0 || seedIDs[c.TMDBID] || (excluded != nil && excluded(c.TMDBID)) {
			continue
		}
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if len(a.Sources) != len(b.Sources) {
			return len(a.Sources) > len(b.Sources)
		}
		if sa, sb := quality(a), quality(b); sa != sb {
			return sa > sb
		}
		return a.TMDBID < b.TMDBID
	})
	if len(out) > o.Cap {
		out = out[:o.Cap]
	}
	return out, warnings
}

func quality(c Candidate) float64 { return c.Rating * math.Log10(float64(c.Votes)+1) }

func fromItem(it tmdb.Item) Candidate {
	return Candidate{
		TMDBID:     it.ID,
		Title:      it.Title,
		Year:       it.Year,
		Overview:   it.Overview,
		Genres:     it.Genres,
		Rating:     it.Rating,
		Votes:      it.Votes,
		Popularity: it.Popularity,
		PosterPath: it.PosterPath,
	}
}

type aggregator struct {
	byID  map[int]*Candidate
	order []*Candidate
}

func (a *aggregator) add(c Candidate, source string) {
	if c.TMDBID == 0 {
		return
	}
	cur, ok := a.byID[c.TMDBID]
	if !ok {
		c.Sources = nil
		cur = &c
		a.byID[c.TMDBID] = cur
		a.order = append(a.order, cur)
	} else {
		fillBlanks(cur, c)
	}
	for _, s := range cur.Sources {
		if s == source {
			return
		}
	}
	cur.Sources = append(cur.Sources, source)
}

func fillBlanks(dst *Candidate, src Candidate) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Year == 0 {
		dst.Year = src.Year
	}
	if dst.Overview == "" {
		dst.Overview = src.Overview
	}
	if len(dst.Genres) == 0 {
		dst.Genres = src.Genres
	}
	if dst.Rating == 0 {
		dst.Rating = src.Rating
	}
	if dst.Votes == 0 {
		dst.Votes = src.Votes
	}
	if dst.Popularity == 0 {
		dst.Popularity = src.Popularity
	}
	if dst.PosterPath == "" {
		dst.PosterPath = src.PosterPath
	}
}
