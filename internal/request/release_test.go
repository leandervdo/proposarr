package request

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
)

// fakeSearcher is a Radarr that searches when asked: after finishAfter polls a
// search completes, and Radarr grabs what grabOn holds for the current profile.
type fakeSearcher struct {
	movie       arr.Movie
	profileID   int
	finishAfter int    // -1: the search never finishes
	endStatus   string // default completed
	grabOn      map[int]arr.Grab
	history     []arr.Grab // newest first
	pending     []arr.Grab
	releases    []arr.Release
	movieErr    error
	setErr      error
	searchErr   error

	cmdID, polls int
	applied      bool
	interactive  int
	switched     []int
	searched     []int
}

func newSearcher() *fakeSearcher {
	return &fakeSearcher{
		movie:       arr.Movie{ID: 42, TMDBID: 9693, Title: "Children of Men", Year: 2006, OriginalLanguage: english, Available: true},
		finishAfter: 1, grabOn: map[int]arr.Grab{}, cmdID: 1,
	}
}

func (f *fakeSearcher) poll() arr.Command {
	f.polls++
	cmd := arr.Command{ID: f.cmdID, Name: "MoviesSearch", Status: "started", MovieIDs: []int{f.movie.ID}}
	if f.finishAfter < 0 || f.polls <= f.finishAfter {
		return cmd
	}
	if !f.applied {
		f.applied = true
		if g, ok := f.grabOn[f.profileID]; ok {
			f.history = append([]arr.Grab{g}, f.history...)
		}
	}
	cmd.Status = cmp.Or(f.endStatus, "completed")
	return cmd
}

func (f *fakeSearcher) Movie(context.Context, int) (arr.Movie, error) { return f.movie, f.movieErr }
func (f *fakeSearcher) MovieSearches(context.Context, int) ([]arr.Command, error) {
	return []arr.Command{f.poll()}, nil
}
func (f *fakeSearcher) Command(_ context.Context, id int) (arr.Command, error) {
	if id != f.cmdID {
		return arr.Command{}, fmt.Errorf("no command %d", id)
	}
	return f.poll(), nil
}
func (f *fakeSearcher) SearchMovie(_ context.Context, id int) (arr.Command, error) {
	if f.searchErr != nil {
		return arr.Command{}, f.searchErr
	}
	f.searched = append(f.searched, id)
	f.cmdID, f.polls, f.applied = f.cmdID+1, 0, false
	return arr.Command{ID: f.cmdID, Name: "MoviesSearch", Status: "queued"}, nil
}
func (f *fakeSearcher) Grabs(context.Context, int) ([]arr.Grab, error)   { return f.history, nil }
func (f *fakeSearcher) Pending(context.Context, int) ([]arr.Grab, error) { return f.pending, nil }
func (f *fakeSearcher) Releases(context.Context, int) ([]arr.Release, error) {
	f.interactive++
	return f.releases, nil
}
func (f *fakeSearcher) SetQualityProfile(_ context.Context, _, profileID int) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.switched = append(f.switched, profileID)
	f.profileID = profileID
	return nil
}

// Radarr quality and custom format ids.
const (
	qBluray1080 = 7
	qBRDisk     = 22
	qRemux1080  = 30
	qWEBDL2160  = 18
	qRemux2160  = 31
	cfRemuxTier = 7
	cfBRDisk    = 11
	cfLQ        = 25
	english     = 1
	french      = 2
)

func rules(language int, qualities ...int) *arr.ProfileRules {
	q := map[int]int{}
	for i, id := range qualities {
		q[id] = i
	}
	return &arr.ProfileRules{Qualities: q, FormatScores: map[int]int{cfRemuxTier: 1950, cfBRDisk: -10000, cfLQ: -10000}, Language: language}
}

var radarrProfiles = []arr.QualityProfile{
	{ID: 7, Name: "Remux + WEB 2160p", Rules: rules(english, qWEBDL2160, qRemux2160)},
	{ID: 8, Name: "Remux + WEB 1080p", Rules: rules(english, qRemux1080)},
	{ID: 10, Name: "HD Bluray + WEB", Rules: rules(english, qBluray1080)},
	{ID: 12, Name: "Without rules"},
}

// childrenOfMen is a search for a 1080p-only movie, judged for "Remux + WEB 2160p".
func childrenOfMen() []arr.Release {
	notWanted := func(q string) string { return q + " is not wanted in profile" }
	remux := func(guid, title string, seeders int, rejections ...string) arr.Release {
		return arr.Release{GUID: guid, Title: title, Quality: "Remux-1080p", QualityID: qRemux1080, Resolution: 1080, Source: "bluray", Modifier: "remux",
			Languages: []int{english}, Protocol: "torrent", Seeders: seeders, Rejections: append([]string{notWanted("Remux-1080p")}, rejections...)}
	}
	framestor := remux("framestor", "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", 249)
	framestor.CustomFormats, framestor.Size, framestor.Indexer = []int{cfRemuxTier}, 32_400_000_000, "TorrentLeech"
	lq := remux("lq", "Children of Men (2006) 1080p BluRay REMUX-d3g", 8, "Custom Formats LQ have score -10000 below Movie's profile minimum 0")
	lq.CustomFormats = []int{cfLQ}
	frenchRemux := remux("french", "Les Fils de l'homme 2006 1080p REMUX FRENCH", 400, "Language English is wanted, but found French")
	frenchRemux.Languages = []int{french}
	return []arr.Release{
		{GUID: "disc", Title: "Children of Men 2006 1080p Blu-ray AVC", Quality: "BR-DISK", QualityID: qBRDisk, Resolution: 1080, Source: "bluray", Modifier: "brdisk",
			CustomFormats: []int{cfBRDisk}, Languages: []int{english}, Protocol: "torrent", Seeders: 15,
			Rejections: []string{"Custom Formats BR-DISK have score -10000 below Movie's profile minimum 0", notWanted("BR-DISK")}},
		framestor,
		remux("kyubi", "Children of Men 2006 1080p BluRay REMUX-KYUBI", 27),
		lq,
		remux("dead", "Children of Men 2006 Bluray 1080p REMUX-EFPG", 0, "Not enough seeders: 0. Minimum seeders: 1"),
		{GUID: "grym", Title: "Children of Men 2006 Bluray 1080p x264-Grym", Quality: "Bluray-1080p", QualityID: qBluray1080, Resolution: 1080, Source: "bluray", Modifier: "none",
			Languages: []int{english}, Protocol: "torrent", Seeders: 22, Rejections: []string{notWanted("Bluray-1080p")}},
		frenchRemux,
		{GUID: "upscale", Title: "Children of Men BluRay Upscale 2160p", Quality: "BR-DISK", QualityID: qBRDisk, Protocol: "torrent", Seeders: 46,
			Rejections: []string{"Unable to parse release"}},
	}
}

var framestorGrab = arr.Grab{Title: "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", Quality: "Remux-1080p", Size: 32_390_607_569, Indexer: "TorrentLeech", Protocol: "torrent"}

func fastPolling(t *testing.T) {
	interval, wait := pollInterval, searchWait
	pollInterval, searchWait = time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { pollInterval, searchWait = interval, wait })
}

// addMovie adds Children of Men with a profile and a fallback, then follows Radarr's search.
func addMovie(t *testing.T, s *fakeSearcher, profile arr.QualityProfile, fallback Fallback) (Result, ReleaseCheck) {
	t.Helper()
	fastPolling(t)
	target := movieTarget()
	target.profiles = radarrProfiles
	s.profileID = profile.ID
	ch := &fakeChooser{quality: profile, fallback: fallback}
	res, err := (&Requester{Radarr: target, RadarrSearch: s}).Add(context.Background(), Item{Kind: media.Movies, TMDBID: 9693}, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !target.added[0].Search || ch.fallbackAsk != 1 || res.Follow == nil {
		t.Fatalf("add = %+v, fallback asked %d, follow %v", target.added[0], ch.fallbackAsk, res.Follow != nil)
	}
	return res, res.Follow(context.Background())
}

func TestFollowRadarrGrabs(t *testing.T) {
	s := newSearcher()
	s.grabOn[8] = framestorGrab
	res, c := addMovie(t, s, radarrProfiles[1], FallbackSwitch)
	if c.Status != CheckGrabbed || c.Profile != "Remux + WEB 1080p" || c.SwitchedFrom != "" || c.Release == nil || *c.Release != (ReleaseInfo{
		Title: framestorGrab.Title, Quality: "Remux-1080p", Size: 32_390_607_569, Indexer: "TorrentLeech", Protocol: "torrent"}) {
		t.Fatalf("check = %+v, release %+v", c, c.Release)
	}
	if res.QualityProfile != "Remux + WEB 1080p" || s.interactive != 0 || len(s.switched) != 0 || len(s.searched) != 0 {
		t.Errorf("result %+v, interactive %d, switched %v, searched %v", res, s.interactive, s.switched, s.searched)
	}
}

func TestFollowNothingFitsWaits(t *testing.T) {
	s := newSearcher()
	s.releases = childrenOfMen()
	_, c := addMovie(t, s, radarrProfiles[0], FallbackWait)
	if c.Status != CheckWaiting || c.Found != 8 || c.Release != nil || len(s.switched) != 0 || s.interactive != 1 {
		t.Fatalf("check = %+v, switched %v, interactive %d", c, s.switched, s.interactive)
	}
	if want := []QualityCount{{"Remux-1080p", 5}, {"BR-DISK", 2}, {"Bluray-1080p", 1}}; !slices.Equal(c.Qualities, want) {
		t.Errorf("qualities = %v, want %v", c.Qualities, want)
	}
	type alt struct {
		id, count int
		best      string
	}
	var got []alt
	for _, o := range c.Alternatives {
		got = append(got, alt{o.ID, o.Count, o.Best.Title})
	}
	// The LQ, dead, French and disc releases fit no profile.
	want := []alt{{8, 2, "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR"}, {10, 1, "Children of Men 2006 Bluray 1080p x264-Grym"}}
	if !slices.Equal(got, want) {
		t.Errorf("alternatives = %+v, want %+v", got, want)
	}
}

func TestFollowSwitchesWhenNothingFits(t *testing.T) {
	s := newSearcher()
	s.releases = childrenOfMen()
	s.grabOn[8] = framestorGrab
	_, c := addMovie(t, s, radarrProfiles[0], FallbackSwitch)
	if c.Status != CheckGrabbed || c.SwitchedFrom != "Remux + WEB 2160p" || c.Profile != "Remux + WEB 1080p" || c.Release == nil || c.Release.Title != framestorGrab.Title {
		t.Fatalf("check = %+v", c)
	}
	if !slices.Equal(s.switched, []int{8}) || !slices.Equal(s.searched, []int{42}) || s.interactive != 1 {
		t.Errorf("switched %v, searched %v, interactive %d", s.switched, s.searched, s.interactive)
	}
}

func TestFollowSwitchesOnlyOnce(t *testing.T) {
	s := newSearcher()
	s.releases = childrenOfMen()
	_, c := addMovie(t, s, radarrProfiles[0], FallbackSwitch)
	if c.Status != CheckWaiting || c.SwitchedFrom != "Remux + WEB 2160p" || c.Profile != "Remux + WEB 1080p" {
		t.Fatalf("check = %+v", c)
	}
	if !slices.Equal(s.switched, []int{8}) || s.interactive != 2 || len(c.Alternatives) != 1 || c.Alternatives[0].ID != 10 {
		t.Errorf("switched %v, interactive %d, alternatives %+v", s.switched, s.interactive, c.Alternatives)
	}
}

func TestFollowNoSwitchWithoutAlternatives(t *testing.T) {
	s := newSearcher()
	s.releases = childrenOfMen()[:1] // only a disc image
	_, c := addMovie(t, s, radarrProfiles[0], FallbackSwitch)
	if c.Status != CheckWaiting || c.Found != 1 || len(c.Alternatives) != 0 || len(s.switched) != 0 {
		t.Errorf("check = %+v, switched %v", c, s.switched)
	}
}

func TestFollowOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(s *fakeSearcher)
		status CheckStatus
		check  func(t *testing.T, s *fakeSearcher, c ReleaseCheck)
	}{
		{name: "still searching", setup: func(s *fakeSearcher) { s.finishAfter = -1 }, status: CheckSearching,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if s.interactive != 0 || s.polls < 2 {
					t.Errorf("interactive %d, polls %d", s.interactive, s.polls)
				}
			}},
		{name: "not released", setup: func(s *fakeSearcher) { s.movie.Available = false }, status: CheckUnavailable,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if s.polls != 0 || s.interactive != 0 {
					t.Errorf("polls %d, interactive %d", s.polls, s.interactive)
				}
			}},
		{name: "delay profile", setup: func(s *fakeSearcher) { s.pending = []arr.Grab{framestorGrab} }, status: CheckPending,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if c.Release == nil || c.Release.Title != framestorGrab.Title || s.interactive != 0 {
					t.Errorf("release %+v, interactive %d", c.Release, s.interactive)
				}
			}},
		{name: "search failed", setup: func(s *fakeSearcher) { s.endStatus = "failed" }, status: CheckFailed,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if !strings.Contains(c.Error, "ended as failed") {
					t.Errorf("error = %q", c.Error)
				}
			}},
		{name: "movie unreadable", setup: func(s *fakeSearcher) { s.movieErr = errors.New("radarr: HTTP 500") }, status: CheckFailed,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if !strings.Contains(c.Error, "HTTP 500") {
					t.Errorf("error = %q", c.Error)
				}
			}},
		{name: "nothing on the indexers", status: CheckWaiting,
			check: func(t *testing.T, s *fakeSearcher, c ReleaseCheck) {
				if c.Found != 0 || c.Qualities == nil || c.Alternatives == nil {
					t.Errorf("check = %+v (qualities and alternatives must be empty lists, not null)", c)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSearcher()
			if tc.setup != nil {
				tc.setup(s)
			}
			_, c := addMovie(t, s, radarrProfiles[0], FallbackSwitch)
			if c.Status != tc.status {
				t.Fatalf("status = %s, want %s (%+v)", c.Status, tc.status, c)
			}
			tc.check(t, s, c)
		})
	}
}

func TestFollowStopped(t *testing.T) {
	fastPolling(t)
	s := newSearcher()
	s.movieErr = context.Canceled
	r := &Requester{Radarr: movieTarget(), RadarrSearch: s}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c := r.followNewMovie(ctx, 42, radarrProfiles[0], radarrProfiles, FallbackSwitch); c.Status != CheckSearching || c.Error != "" {
		t.Errorf("a check stopped by its context = %+v, want searching", c)
	}
}

func TestAddSeriesIsNotFollowed(t *testing.T) {
	target := &fakeTarget{lookup: &arr.Lookup{Title: "Severance", TVDBID: 371980}, profiles: profiles, folders: []arr.RootFolder{{Path: "/tv"}}}
	ch := &fakeChooser{quality: profiles[0]}
	r := &Requester{Sonarr: target, TMDB: fakeTVDB{95396: 371980}, RadarrSearch: newSearcher()}
	res, err := r.Add(context.Background(), Item{Kind: media.Series, TMDBID: 95396}, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !target.added[0].Search || res.Follow != nil || ch.fallbackAsk != 0 {
		t.Errorf("series add = %+v, follow %v, fallback asked %d", target.added[0], res.Follow != nil, ch.fallbackAsk)
	}
}

// refusingTarget is a Radarr that already has the movie although its lookup did not say so.
type refusingTarget struct{ *fakeTarget }

func (refusingTarget) Add(context.Context, *arr.Lookup, arr.AddOptions) (arr.Added, error) {
	return arr.Added{}, fmt.Errorf("%w: radarr: POST /api/v3/movie: HTTP 400: This movie has already been added", arr.ErrAlreadyAdded)
}

func TestAddAlreadyAddedByApp(t *testing.T) {
	r := &Requester{Radarr: refusingTarget{movieTarget()}, RadarrSearch: newSearcher()}
	if _, err := r.Add(context.Background(), Item{Kind: media.Movies, TMDBID: 1}, &fakeChooser{quality: profiles[0]}); !errors.Is(err, ErrAlreadyInLibrary) {
		t.Fatalf("err = %v", err)
	}
}

func TestSwitchProfile(t *testing.T) {
	fastPolling(t)
	ctx := context.Background()
	item := Item{Kind: media.Movies, TMDBID: 9693, Title: "Children of Men", Year: 2006}
	older := arr.Grab{Title: "Children of Men 2006 720p HDTV", Quality: "HDTV-720p"}
	requester := func(s *fakeSearcher) *Requester {
		target := movieTarget()
		target.profiles = radarrProfiles
		return &Requester{Radarr: target, RadarrSearch: s}
	}

	s := newSearcher()
	s.profileID, s.history, s.grabOn[8] = 7, []arr.Grab{older}, framestorGrab
	res, err := requester(s).SwitchProfile(ctx, item, 42, 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 42 || res.QualityProfile != "Remux + WEB 1080p" || res.Follow == nil || !slices.Equal(s.switched, []int{8}) || !slices.Equal(s.searched, []int{42}) {
		t.Fatalf("result = %+v, switched %v, searched %v", res, s.switched, s.searched)
	}
	if c := res.Follow(ctx); c.Status != CheckGrabbed || c.Release.Title != framestorGrab.Title || c.SwitchedFrom != "" {
		t.Errorf("check = %+v", c)
	}

	s = newSearcher()
	s.profileID, s.history, s.releases = 7, []arr.Grab{older}, childrenOfMen()
	res, _ = requester(s).SwitchProfile(ctx, item, 42, 8)
	if c := res.Follow(ctx); c.Status != CheckWaiting {
		t.Errorf("an older grab counted as a new one: %+v", c)
	}

	s = newSearcher()
	s.searchErr = errors.New("radarr: HTTP 503")
	res, err = requester(s).SwitchProfile(ctx, item, 42, 8)
	if err != nil || !slices.Equal(s.switched, []int{8}) {
		t.Fatalf("a search that did not start must still report the switch: %v, switched %v", err, s.switched)
	}
	if c := res.Follow(ctx); c.Status != CheckFailed || c.Profile != "Remux + WEB 1080p" || !strings.Contains(c.Error, "start Radarr's search") {
		t.Errorf("check = %+v", c)
	}

	cases := []struct {
		name  string
		item  Item
		setup func(s *fakeSearcher)
		id    int
		want  error
		noSrc bool
	}{
		{name: "unknown profile", item: item, id: 99, want: ErrUnknownProfile},
		{name: "movie gone", item: item, setup: func(s *fakeSearcher) { s.movieErr = arr.ErrNotFound }, id: 8, want: ErrNotInLibrary},
		{name: "other movie", item: item, setup: func(s *fakeSearcher) { s.movie.TMDBID = 1 }, id: 8, want: ErrNotInLibrary},
		{name: "no searcher", item: item, id: 8, want: ErrNotConfigured, noSrc: true},
		{name: "series", item: Item{Kind: media.Series, TMDBID: 1}, id: 8},
		{name: "profile not set", item: item, setup: func(s *fakeSearcher) { s.setErr = errors.New("radarr: HTTP 500") }, id: 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSearcher()
			if tc.setup != nil {
				tc.setup(s)
			}
			r := requester(s)
			if tc.noSrc {
				r.RadarrSearch = nil
			}
			_, err := r.SwitchProfile(ctx, tc.item, 42, tc.id)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(s.switched) != 0 || len(s.searched) != 0 {
				t.Errorf("switched %v, searched %v after an error", s.switched, s.searched)
			}
		})
	}
}

func TestAlternativesOrder(t *testing.T) {
	anyLanguage := arr.QualityProfile{ID: 11, Name: "Any language", Rules: rules(arr.LanguageAny, qRemux1080)}
	got := Alternatives(childrenOfMen(), append(slices.Clone(radarrProfiles), anyLanguage), 7, english)
	var ids []int
	for _, o := range got {
		ids = append(ids, o.ID)
	}
	// Remux beats Bluray; between the remux profiles, the one accepting the French release has more.
	if !slices.Equal(ids, []int{11, 8, 10}) || got[0].Count != 3 || got[0].Best.Seeders == nil || *got[0].Best.Seeders != 249 {
		t.Errorf("alternatives = %+v", got)
	}
}

func TestLanguageFits(t *testing.T) {
	cases := []struct {
		want     int
		have     []int
		original int
		fits     bool
	}{
		{arr.LanguageAny, []int{french}, english, true},
		{english, []int{french, english}, english, true},
		{english, []int{french}, english, false},
		{english, nil, english, true},
		{arr.LanguageOriginal, []int{french}, french, true},
		{arr.LanguageOriginal, []int{english}, french, false},
		{arr.LanguageOriginal, []int{english}, 0, true},
	}
	for _, tc := range cases {
		if got := languageFits(tc.want, tc.have, tc.original); got != tc.fits {
			t.Errorf("languageFits(%d, %v, %d) = %v", tc.want, tc.have, tc.original, got)
		}
	}
}

func TestParseFallback(t *testing.T) {
	for in, want := range map[string]Fallback{"": FallbackWait, "wait": FallbackWait, "switch": FallbackSwitch} {
		if got, err := ParseFallback(in); err != nil || got != want {
			t.Errorf("ParseFallback(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseFallback("grab"); err == nil {
		t.Error("want an error for an unknown fallback")
	}
}
