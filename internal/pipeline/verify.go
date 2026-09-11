package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/candidates"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

const (
	sourceCandidate = "candidate"
	sourceFree      = "free"
)

type rawPick struct {
	TMDBID    int      `json:"tmdb_id"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Reason    string   `json:"reason"`
	RelatedTo []string `json:"related_to"`
	Score     float64  `json:"score"`
	Source    string   `json:"source"`
}

func decodePicks(res agent.Result) ([]rawPick, error) {
	b, err := agent.StructuredJSON(res)
	if err != nil {
		return nil, fmt.Errorf("agent output: %w", err)
	}
	var out struct {
		Picks []rawPick `json:"picks"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("agent output: %w", err)
	}
	return out.Picks, nil
}

type keptPick struct {
	pick    Pick
	free    bool
	details *tmdb.Details // set when resolution already fetched details
}

// verify turns the agent's raw picks into Picks, rejecting anything excluded,
// duplicated, unresolvable or over the limits.
func (p *Pipeline) verify(ctx context.Context, req Request, run *Run, raw []rawPick, cands []candidates.Candidate,
	excluded map[int]bool, untracked map[string][]int, prof profile.Profile) {
	byID := make(map[int]candidates.Candidate, len(cands))
	for _, c := range cands {
		byID[c.TMDBID] = c
	}
	reject := func(id int, title, reason string) {
		run.Rejected = append(run.Rejected, Rejected{TMDBID: id, Title: title, Reason: reason})
	}

	seen := map[int]bool{}
	free := 0
	var kept []keptPick
	for _, r := range raw {
		title := strings.TrimSpace(r.Title)
		if r.TMDBID > 0 && seen[r.TMDBID] {
			reject(r.TMDBID, title, "duplicate")
			continue
		}

		var k keptPick
		if c, ok := byID[r.TMDBID]; ok && r.TMDBID > 0 {
			k.pick = Pick{TMDBID: c.TMDBID, Title: c.Title, Year: c.Year, Overview: c.Overview, Genres: c.Genres, Rating: c.Rating, PosterURL: tmdb.PosterURL(c.PosterPath)}
		} else {
			if free >= req.FreePicks {
				reject(r.TMDBID, title, "free pick limit reached")
				continue
			}
			it, det, ok := p.resolve(ctx, req, r)
			if !ok {
				reject(r.TMDBID, title, "could not resolve on TMDB")
				continue
			}
			k.free = true
			k.details = det
			k.pick = Pick{TMDBID: it.ID, Title: it.Title, Year: it.Year, Overview: it.Overview, Genres: it.Genres, Rating: it.Rating, PosterURL: tmdb.PosterURL(it.PosterPath)}
		}

		switch {
		case seen[k.pick.TMDBID]:
			reject(k.pick.TMDBID, k.pick.Title, "duplicate")
			continue
		case excluded[k.pick.TMDBID] || matchesUntracked(untracked, k.pick.Title, k.pick.Year):
			reject(k.pick.TMDBID, k.pick.Title, "already in library or watch history")
			continue
		case len(kept) >= req.Picks:
			reject(k.pick.TMDBID, k.pick.Title, "over pick count")
			continue
		}
		seen[k.pick.TMDBID] = true
		k.pick.Source = sourceCandidate
		if k.free {
			free++
			k.pick.Source = sourceFree
		}
		k.pick.Kind = req.Kind
		k.pick.Reason = strings.TrimSpace(r.Reason)
		k.pick.RelatedTo = relatedTo(r.RelatedTo, prof)
		k.pick.Score = clampScore(r.Score)
		kept = append(kept, k)
	}

	errs := p.enrich(ctx, req, kept)
	var failed int
	var first error
	for i, k := range kept {
		if errs[i] != nil {
			if k.free {
				reject(k.pick.TMDBID, k.pick.Title, "id does not resolve on TMDB")
				continue
			}
			failed++
			if first == nil {
				first = errs[i]
			}
		} else {
			applyDetails(&k.pick, k.details)
		}
		run.Picks = append(run.Picks, k.pick)
	}
	if failed > 0 {
		run.Warnings = append(run.Warnings, fmt.Sprintf("tmdb details failed for %d of %d picks: %v", failed, len(kept), first))
	}

	sort.SliceStable(run.Picks, func(i, j int) bool { return run.Picks[i].Score > run.Picks[j].Score })
}

// resolve finds a free pick on TMDB. A model-supplied id is only trusted when
// its title matches; otherwise the title and year are searched.
func (p *Pipeline) resolve(ctx context.Context, req Request, r rawPick) (tmdb.Item, *tmdb.Details, bool) {
	want := media.NormTitle(r.Title)
	if want == "" {
		return tmdb.Item{}, nil, false
	}
	if r.TMDBID > 0 {
		if d, err := p.d.Meta.Details(ctx, req.Kind, r.TMDBID, req.Region); err == nil && media.NormTitle(d.Title) == want {
			if d.ID == 0 {
				d.ID = r.TMDBID
			}
			return d.Item, &d, true
		}
	}
	years := []int{r.Year}
	if r.Year > 0 {
		years = append(years, 0) // TMDB's year filter misses off-by-one release dates
	}
	for _, y := range years {
		items, err := p.d.Meta.Search(ctx, req.Kind, r.Title, y)
		if err != nil {
			return tmdb.Item{}, nil, false
		}
		for _, it := range items {
			if it.ID > 0 && media.NormTitle(it.Title) == want && yearClose(it.Year, r.Year) {
				return it, nil, true
			}
		}
	}
	return tmdb.Item{}, nil, false
}

// enrich fetches details for kept picks that do not have them yet.
func (p *Pipeline) enrich(ctx context.Context, req Request, kept []keptPick) []error {
	errs := make([]error, len(kept))
	var wg sync.WaitGroup
	sem := make(chan struct{}, detailConcurrency)
	for i := range kept {
		if kept[i].details != nil {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := p.d.Meta.Details(ctx, req.Kind, kept[i].pick.TMDBID, req.Region)
			if err != nil {
				errs[i] = err
				return
			}
			kept[i].details = &d
		}(i)
	}
	wg.Wait()
	return errs
}

func applyDetails(pk *Pick, d *tmdb.Details) {
	if d == nil {
		return
	}
	pk.Streaming = d.Providers
	if d.PosterPath != "" {
		pk.PosterURL = tmdb.PosterURL(d.PosterPath)
	}
	if d.Overview != "" {
		pk.Overview = d.Overview
	}
	if len(d.Genres) > 0 {
		pk.Genres = d.Genres
	}
	if d.Rating > 0 {
		pk.Rating = d.Rating
	}
}

var yearSuffix = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// relatedTo keeps the names that refer to taste-profile titles, spelled as the profile spells them.
func relatedTo(names []string, prof profile.Profile) []string {
	labels := map[string]string{}
	for _, e := range prof.Top {
		n := media.NormTitle(e.Title)
		if _, ok := labels[n]; !ok {
			labels[n] = e.Label()
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		l, ok := labels[media.NormTitle(yearSuffix.ReplaceAllString(name, ""))]
		if !ok || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

func clampScore(s float64) int {
	return int(math.Max(0, math.Min(100, math.Round(s))))
}

func yearClose(got, want int) bool {
	if want == 0 {
		return true
	}
	d := got - want
	return d >= -1 && d <= 1
}

// untrackedTitles indexes library and history titles without a TMDB id, the
// last-resort title guard for picks that id-based exclusion cannot catch.
func untrackedTitles(lib []media.Title, hist []history.Entry) map[string][]int {
	out := map[string][]int{}
	add := func(title string, year int) {
		if n := media.NormTitle(title); n != "" {
			out[n] = append(out[n], year)
		}
	}
	for _, t := range lib {
		if t.TMDBID == 0 {
			add(t.Title, t.Year)
		}
	}
	for _, h := range hist {
		if h.TMDBID == 0 {
			add(h.Title, h.Year)
		}
	}
	return out
}

func matchesUntracked(untracked map[string][]int, title string, year int) bool {
	for _, y := range untracked[media.NormTitle(title)] {
		if y == 0 || year == 0 || y == year {
			return true
		}
	}
	return false
}
