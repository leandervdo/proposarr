package profile

import (
	"math"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
)

var t0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func find(t *testing.T, p Profile, title string) Entry {
	t.Helper()
	for _, e := range p.Top {
		if e.Title == title {
			return e
		}
	}
	t.Fatalf("%q not in profile top: %+v", title, p.Top)
	return Entry{}
}

func TestBuildWeightsAndMatching(t *testing.T) {
	library := []media.Title{
		{TMDBID: 1, Title: "Arrival", Year: 2016, Genres: []string{"Sci-Fi", "Drama"}, Added: t0},
		{TVDBID: 50, Title: "Severance", Year: 2022, Genres: []string{"Thriller"}},
		{Title: "The Thing", Year: 1982, Genres: []string{"Horror"}},
		{Title: "The Thing", Year: 2011, Genres: []string{"Horror"}},
		{TMDBID: 5, Title: "Unwatched", Year: 2000, Genres: []string{"Comedy"}, Added: t0.Add(-time.Hour)},
	}
	hist := []history.Entry{
		{TMDBID: 1, Title: "Arrival", Signal: history.Watched, LastWatched: t0},
		{TMDBID: 1, Title: "Arrival", Signal: history.Rewatched, LastWatched: t0.Add(-time.Hour)},
		{TVDBID: 50, TMDBID: 95396, Title: "Severance", Signal: history.Partial, LastWatched: t0},
		{Title: "Thing", Year: 1982, Signal: history.Watched, LastWatched: t0},
		{TMDBID: 9, Title: "Dune", Year: 2021, Signal: history.Watched, LastWatched: t0.Add(time.Hour)},
	}
	p := Build(media.Movies, library, hist, map[int][]string{9: {"Sci-Fi"}, 5: {"Ignored"}}, 0, DefaultWeights)

	if p.LibraryCount != 5 || p.HistoryCount != 5 {
		t.Fatalf("counts = %d/%d", p.LibraryCount, p.HistoryCount)
	}
	if len(p.Top) != 6 {
		t.Fatalf("want 6 entries, got %d: %+v", len(p.Top), p.Top)
	}

	arrival := find(t, p, "Arrival")
	if arrival.Weight != 4 || arrival.Signal != "rewatched" || !arrival.InLibrary || !arrival.LastWatched.Equal(t0) {
		t.Errorf("arrival = %+v", arrival)
	}
	sev := find(t, p, "Severance")
	if sev.Weight != 1 || !sev.InLibrary || sev.TMDBID != 95396 {
		t.Errorf("severance matched by tvdb = %+v", sev)
	}
	dune := find(t, p, "Dune")
	if dune.InLibrary || dune.Weight != 3 || len(dune.Genres) != 1 || dune.Genres[0] != "Sci-Fi" {
		t.Errorf("dune history-only = %+v", dune)
	}
	un := find(t, p, "Unwatched")
	if un.Weight != 0.5 || un.Signal != SignalOwned || un.Genres[0] != "Comedy" {
		t.Errorf("unwatched = %+v", un)
	}

	var things []Entry
	for _, e := range p.Top {
		if e.Title == "The Thing" {
			things = append(things, e)
		}
	}
	for _, e := range things {
		want := 0.5
		if e.Year == 1982 {
			want = 3
		}
		if e.Weight != want {
			t.Errorf("The Thing (%d) weight = %v, want %v", e.Year, e.Weight, want)
		}
	}
}

func TestBuildOrderAndTopN(t *testing.T) {
	library := []media.Title{
		{TMDBID: 1, Title: "B", Added: t0},
		{TMDBID: 2, Title: "A", Added: t0},
		{TMDBID: 3, Title: "Newer", Added: t0.Add(time.Hour)},
	}
	hist := []history.Entry{
		{TMDBID: 4, Title: "Old watch", Signal: history.Watched, LastWatched: t0},
		{TMDBID: 5, Title: "New watch", Signal: history.Watched, LastWatched: t0.Add(time.Hour)},
		{TMDBID: 6, Title: "Rewatch", Signal: history.Rewatched, LastWatched: t0.Add(-time.Hour)},
	}
	p := Build(media.Movies, library, hist, nil, 5, DefaultWeights)
	want := []string{"Rewatch", "New watch", "Old watch", "Newer", "A"}
	if len(p.Top) != len(want) {
		t.Fatalf("top = %+v", p.Top)
	}
	for i, w := range want {
		if p.Top[i].Title != w {
			t.Errorf("top[%d] = %q, want %q", i, p.Top[i].Title, w)
		}
	}
}

func TestGenreShares(t *testing.T) {
	library := []media.Title{
		{TMDBID: 1, Title: "One", Genres: []string{"Drama", "Sci-Fi"}},
		{TMDBID: 2, Title: "Two", Genres: []string{"Drama"}},
	}
	hist := []history.Entry{{TMDBID: 1, Title: "One", Signal: history.Watched}}
	p := Build(media.Movies, library, hist, nil, 40, DefaultWeights)
	// One: 3 to Drama and Sci-Fi; Two: 0.5 to Drama. Total 6.5.
	if len(p.Genres) != 2 || p.Genres[0].Name != "Drama" || p.Genres[1].Name != "Sci-Fi" {
		t.Fatalf("genres = %+v", p.Genres)
	}
	if math.Abs(p.Genres[0].Share-3.5/6.5) > 1e-9 || math.Abs(p.Genres[1].Share-3/6.5) > 1e-9 {
		t.Errorf("shares = %+v", p.Genres)
	}

	var many []media.Title
	for i := 0; i < 20; i++ {
		many = append(many, media.Title{TMDBID: i + 1, Title: string(rune('a' + i)), Genres: []string{string(rune('A' + i))}})
	}
	if g := Build(media.Movies, many, nil, nil, 40, DefaultWeights).Genres; len(g) != maxGenres || g[0].Name != "A" {
		t.Errorf("capped genres = %+v", g)
	}
}

func TestTitles(t *testing.T) {
	p := Build(media.Series, []media.Title{{TMDBID: 1, Title: "The Office"}}, nil, nil, 40, DefaultWeights)
	if !p.Titles()[media.NormTitle("Office")] {
		t.Errorf("titles = %v", p.Titles())
	}
}
