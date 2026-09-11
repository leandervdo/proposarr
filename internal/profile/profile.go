// Package profile builds the weighted taste profile the agent sees inline.
package profile

import (
	"sort"
	"time"

	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
)

type Weights struct{ Rewatched, Watched, Partial, Owned float64 }

var DefaultWeights = Weights{Rewatched: 4, Watched: 3, Partial: 1, Owned: 0.5}

func (w Weights) signal(s history.Signal) float64 {
	switch s {
	case history.Rewatched:
		return w.Rewatched
	case history.Watched:
		return w.Watched
	case history.Partial:
		return w.Partial
	}
	return 0
}

const SignalOwned = "owned"

type Entry struct {
	TMDBID      int       `json:"tmdb_id,omitempty"`
	Title       string    `json:"title"`
	Year        int       `json:"year,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	Weight      float64   `json:"weight"`
	Signal      string    `json:"signal"`
	InLibrary   bool      `json:"in_library"`
	LastWatched time.Time `json:"last_watched,omitempty"`
	Added       time.Time `json:"added,omitempty"`
}

// Label renders "Title (Year)".
func (e Entry) Label() string { return media.Label(e.Title, e.Year) }

type GenreShare struct {
	Name  string  `json:"name"`
	Share float64 `json:"share"`
}

type Profile struct {
	Kind         media.Kind   `json:"kind"`
	Top          []Entry      `json:"top"`
	Genres       []GenreShare `json:"genres"`
	LibraryCount int          `json:"library_count"`
	HistoryCount int          `json:"history_count"`
}

const maxGenres = 12

// Build merges the library and watch history into one weighted entry per title.
func Build(kind media.Kind, library []media.Title, hist []history.Entry, extraGenres map[int][]string, topN int, w Weights) Profile {
	b := newIndex()

	for _, t := range library {
		i := b.find(t.TMDBID, t.TVDBID, t.Title, t.Year)
		if i < 0 {
			i = b.add(&Entry{TMDBID: t.TMDBID, Title: t.Title, Year: t.Year}, t.TVDBID)
		}
		e := b.entries[i]
		e.InLibrary = true
		if len(e.Genres) == 0 {
			e.Genres = t.Genres
		}
		if e.Added.IsZero() || t.Added.After(e.Added) {
			e.Added = t.Added
		}
		b.fill(i, t.TMDBID, t.TVDBID, t.Year)
	}

	hasHistory := map[int]bool{}
	for _, h := range hist {
		i := b.find(h.TMDBID, h.TVDBID, h.Title, h.Year)
		if i < 0 {
			i = b.add(&Entry{TMDBID: h.TMDBID, Title: h.Title, Year: h.Year}, h.TVDBID)
		}
		e := b.entries[i]
		if hw := w.signal(h.Signal); !hasHistory[i] || hw > e.Weight {
			e.Weight = hw
			e.Signal = string(h.Signal)
		}
		hasHistory[i] = true
		if h.LastWatched.After(e.LastWatched) {
			e.LastWatched = h.LastWatched
		}
		b.fill(i, h.TMDBID, h.TVDBID, h.Year)
	}

	all := make([]Entry, 0, len(b.entries))
	for i, e := range b.entries {
		if !hasHistory[i] {
			e.Weight = w.Owned
			e.Signal = SignalOwned
		}
		if len(e.Genres) == 0 && e.TMDBID > 0 {
			e.Genres = extraGenres[e.TMDBID]
		}
		all = append(all, *e)
	}

	sort.SliceStable(all, func(i, j int) bool {
		a, c := all[i], all[j]
		if a.Weight != c.Weight {
			return a.Weight > c.Weight
		}
		if !a.LastWatched.Equal(c.LastWatched) {
			return a.LastWatched.After(c.LastWatched)
		}
		if !a.Added.Equal(c.Added) {
			return a.Added.After(c.Added)
		}
		return a.Title < c.Title
	})

	top := all
	if topN > 0 && len(top) > topN {
		top = top[:topN]
	}

	return Profile{
		Kind:         kind,
		Top:          top,
		Genres:       genreShares(all),
		LibraryCount: len(library),
		HistoryCount: len(hist),
	}
}

// Titles returns the normalized titles of Top, for related_to validation.
func (p Profile) Titles() map[string]bool {
	out := make(map[string]bool, len(p.Top))
	for _, e := range p.Top {
		out[media.NormTitle(e.Title)] = true
	}
	return out
}

// genreShares gives each entry's weight to every genre it has; shares sum to 1
// before truncation.
func genreShares(entries []Entry) []GenreShare {
	sums := map[string]float64{}
	var total float64
	for _, e := range entries {
		for _, g := range e.Genres {
			sums[g] += e.Weight
			total += e.Weight
		}
	}
	if total == 0 {
		return nil
	}
	out := make([]GenreShare, 0, len(sums))
	for name, s := range sums {
		out = append(out, GenreShare{Name: name, Share: s / total})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Share != out[j].Share {
			return out[i].Share > out[j].Share
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxGenres {
		out = out[:maxGenres]
	}
	return out
}

type index struct {
	entries []*Entry
	tvdb    []int
	byTMDB  map[int]int
	byTVDB  map[int]int
	byTitle map[string][]int
}

func newIndex() *index {
	return &index{byTMDB: map[int]int{}, byTVDB: map[int]int{}, byTitle: map[string][]int{}}
}

func (x *index) add(e *Entry, tvdb int) int {
	i := len(x.entries)
	x.entries = append(x.entries, e)
	x.tvdb = append(x.tvdb, 0)
	if n := media.NormTitle(e.Title); n != "" {
		x.byTitle[n] = append(x.byTitle[n], i)
	}
	x.fill(i, e.TMDBID, tvdb, e.Year)
	return i
}

// fill records ids learned for entry i so later sources can match on them.
func (x *index) fill(i, tmdbID, tvdbID, year int) {
	e := x.entries[i]
	if tmdbID > 0 {
		if e.TMDBID == 0 {
			e.TMDBID = tmdbID
		}
		if _, ok := x.byTMDB[tmdbID]; !ok {
			x.byTMDB[tmdbID] = i
		}
	}
	if tvdbID > 0 {
		if x.tvdb[i] == 0 {
			x.tvdb[i] = tvdbID
		}
		if _, ok := x.byTVDB[tvdbID]; !ok {
			x.byTVDB[tvdbID] = i
		}
	}
	if e.Year == 0 && year > 0 {
		e.Year = year
	}
}

func (x *index) find(tmdbID, tvdbID int, title string, year int) int {
	if tmdbID > 0 {
		if i, ok := x.byTMDB[tmdbID]; ok {
			return i
		}
	}
	if tvdbID > 0 {
		if i, ok := x.byTVDB[tvdbID]; ok {
			return i
		}
	}
	n := media.NormTitle(title)
	if n == "" {
		return -1
	}
	loose := -1
	for _, i := range x.byTitle[n] {
		e := x.entries[i]
		// An id conflict means a different title with the same name.
		if tmdbID > 0 && e.TMDBID > 0 && e.TMDBID != tmdbID {
			continue
		}
		if tvdbID > 0 && x.tvdb[i] > 0 && x.tvdb[i] != tvdbID {
			continue
		}
		if year > 0 && e.Year > 0 {
			if year == e.Year {
				return i
			}
			continue
		}
		if loose < 0 {
			loose = i
		}
	}
	return loose
}
