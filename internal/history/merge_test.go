package history

import (
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

func TestMerge(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1, t2, t3 := t0.Add(time.Hour), t0.Add(2*time.Hour), t0.Add(3*time.Hour)

	a := []Entry{
		{Kind: media.Movies, TMDBID: 603, Title: "The Matrix", Plays: 1, Signal: Watched, LastWatched: t1},
		{Kind: media.Movies, Title: "Heat", Year: 1995, Plays: 1, Signal: Partial, LastWatched: t1},
	}
	b := []Entry{
		{Kind: media.Movies, TMDBID: 603, Title: "The Matrix", Year: 1999, Plays: 1, Signal: Rewatched, LastWatched: t3},
		{Kind: media.Movies, Title: "heat!", Year: 1995, Plays: 2, Signal: Watched, LastWatched: t2},
		{Kind: media.Series, TVDBID: 5, Title: "X", Episodes: 3, Signal: Partial, LastWatched: t0},
		{Kind: media.Series, TVDBID: 5, TMDBID: 0, Episodes: 7, Signal: Watched},
	}

	got := Merge(a, b)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	m, h, x := got[0], got[1], got[2]
	if m.TMDBID != 603 || m.Plays != 2 || m.Signal != Rewatched || m.Year != 1999 || !m.LastWatched.Equal(t3) {
		t.Errorf("matrix = %+v", m)
	}
	if h.Title != "Heat" || h.Plays != 3 || h.Signal != Watched || !h.LastWatched.Equal(t2) {
		t.Errorf("heat = %+v", h)
	}
	if x.Title != "X" || x.Episodes != 7 || x.Signal != Watched {
		t.Errorf("series = %+v", x)
	}
}

func TestSignals(t *testing.T) {
	if movieSignal(1) != Watched || movieSignal(2) != Rewatched {
		t.Error("movie signal")
	}
	cases := []struct {
		distinct, total, maxPlays int
		want                      Signal
	}{
		{3, 10, 2, Rewatched},
		{6, 0, 1, Watched},
		{2, 4, 1, Watched},
		{2, 5, 1, Partial},
		{1, 0, 1, Partial},
	}
	for _, c := range cases {
		if got := seriesSignal(c.distinct, c.total, c.maxPlays); got != c.want {
			t.Errorf("seriesSignal(%d,%d,%d) = %s, want %s", c.distinct, c.total, c.maxPlays, got, c.want)
		}
	}
}
