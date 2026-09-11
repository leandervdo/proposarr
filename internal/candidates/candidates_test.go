package candidates

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
	"github.com/leandervdo/proposarr/internal/tmdb"
)

type fakeRelated struct {
	mu      sync.Mutex
	recs    map[int][]tmdb.Item
	similar map[int][]tmdb.Item
	fail    map[int]error
	calls   []string
}

func (f *fakeRelated) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeRelated) Recommendations(_ context.Context, _ media.Kind, id int) ([]tmdb.Item, error) {
	f.record("recs")
	if err := f.fail[id]; err != nil {
		return nil, err
	}
	return f.recs[id], nil
}

func (f *fakeRelated) Similar(_ context.Context, _ media.Kind, id int) ([]tmdb.Item, error) {
	f.record("similar")
	return f.similar[id], nil
}

type fakeDiscover struct {
	items []arr.ListMovie
	err   error
}

func (f fakeDiscover) Discover(context.Context) ([]arr.ListMovie, error) { return f.items, f.err }

func ids(cs []Candidate) []int {
	out := make([]int, len(cs))
	for i, c := range cs {
		out[i] = c.TMDBID
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildMovies(t *testing.T) {
	seeds := []profile.Entry{
		{TMDBID: 1, Title: "Arrival", Year: 2016},
		{Title: "No id"},
		{TMDBID: 2, Title: "Dune", Year: 2021},
	}
	rel := &fakeRelated{recs: map[int][]tmdb.Item{
		1: {
			{ID: 10, Title: "Contact", Rating: 7, Votes: 1000},
			{ID: 11, Title: "Low", Rating: 5, Votes: 10},
			{ID: 2, Title: "Dune"},   // a seed
			{ID: 99, Title: "Owned"}, // excluded
			{ID: 0, Title: "No id"},
		},
		2: {
			{ID: 10, Title: "Contact", Overview: "filled later", PosterPath: "/p.jpg"},
			{ID: 12, Title: "Strong", Rating: 9, Votes: 100000},
		},
	}}
	disc := fakeDiscover{items: []arr.ListMovie{{TMDBID: 11, Title: "Low"}, {TMDBID: 13, Title: "Discovered", Rating: 1, Votes: 1}}}
	excluded := func(id int) bool { return id == 99 }

	got, warns := Build(context.Background(), rel, disc, seeds, excluded, Options{Kind: media.Movies})
	if len(warns) != 0 {
		t.Fatalf("warnings = %v", warns)
	}
	if want := []int{10, 11, 12, 13}; !equalInts(ids(got), want) {
		t.Fatalf("ids = %v, want %v", ids(got), want)
	}
	contact := got[0]
	if strings.Join(contact.Sources, "|") != "Arrival (2016)|Dune (2021)" {
		t.Errorf("sources = %v", contact.Sources)
	}
	if contact.Overview != "filled later" || contact.PosterPath != "/p.jpg" || contact.Rating != 7 {
		t.Errorf("blanks not filled: %+v", contact)
	}
	if strings.Join(got[1].Sources, "|") != "Arrival (2016)|"+DiscoverSource {
		t.Errorf("low sources = %v", got[1].Sources)
	}
	for _, c := range rel.calls {
		if c == "similar" {
			t.Errorf("similar called for movies")
		}
	}
}

func TestBuildSeriesUsesSimilarAndCaps(t *testing.T) {
	seeds := []profile.Entry{{TMDBID: 1, Title: "A"}, {TMDBID: 2, Title: "B"}, {TMDBID: 3, Title: "C"}}
	rel := &fakeRelated{
		recs:    map[int][]tmdb.Item{1: {{ID: 20, Rating: 8, Votes: 10}}, 2: {{ID: 21}}, 3: {{ID: 99}}},
		similar: map[int][]tmdb.Item{1: {{ID: 21}, {ID: 22, Rating: 9, Votes: 10}}},
	}
	disc := fakeDiscover{items: []arr.ListMovie{{TMDBID: 50}}}
	got, _ := Build(context.Background(), rel, disc, seeds, nil, Options{Kind: media.Series, Seeds: 2, Cap: 2})
	if want := []int{21, 22}; !equalInts(ids(got), want) {
		t.Fatalf("ids = %v, want %v (seed C beyond Seeds, discover ignored for series)", ids(got), want)
	}
}

func TestBuildWarnings(t *testing.T) {
	seeds := []profile.Entry{{TMDBID: 1, Title: "A"}, {TMDBID: 2, Title: "B"}, {TMDBID: 3, Title: "C"}}
	rel := &fakeRelated{
		recs: map[int][]tmdb.Item{2: {{ID: 30}}},
		fail: map[int]error{1: errors.New("boom 1"), 3: errors.New("boom 3")},
	}
	got, warns := Build(context.Background(), rel, fakeDiscover{err: errors.New("down")}, seeds, nil, Options{Kind: media.Movies})
	if !equalInts(ids(got), []int{30}) {
		t.Errorf("ids = %v", ids(got))
	}
	want := []string{
		"tmdb recommendations failed for 2 of 3 seeds: boom 1",
		"Radarr discover failed: down",
	}
	if strings.Join(warns, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings = %q", warns)
	}
}
