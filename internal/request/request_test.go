package request

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
)

type fakeTarget struct {
	lookup   *arr.Lookup
	profiles []arr.QualityProfile
	folders  []arr.RootFolder
	lookedUp []int
	added    []arr.AddOptions
}

func (f *fakeTarget) Lookup(_ context.Context, id int) (*arr.Lookup, error) {
	f.lookedUp = append(f.lookedUp, id)
	return f.lookup, nil
}
func (f *fakeTarget) QualityProfiles(context.Context) ([]arr.QualityProfile, error) {
	return f.profiles, nil
}
func (f *fakeTarget) RootFolders(context.Context) ([]arr.RootFolder, error) { return f.folders, nil }
func (f *fakeTarget) Add(_ context.Context, l *arr.Lookup, o arr.AddOptions) (arr.Added, error) {
	f.added = append(f.added, o)
	return arr.Added{ID: 42, Title: l.Title}, nil
}

type fakeChooser struct {
	quality      arr.QualityProfile
	folder       arr.RootFolder
	err          error
	qualityAsked []Item
	folderAsked  int
}

func (c *fakeChooser) ChooseQualityProfile(_ context.Context, item Item, _ string, _ []arr.QualityProfile) (arr.QualityProfile, error) {
	c.qualityAsked = append(c.qualityAsked, item)
	return c.quality, c.err
}
func (c *fakeChooser) ChooseRootFolder(context.Context, Item, string, []arr.RootFolder) (arr.RootFolder, error) {
	c.folderAsked++
	return c.folder, nil
}

type fakeTVDB map[int]int

func (f fakeTVDB) TVDBID(_ context.Context, tmdb int) (int, error) { return f[tmdb], nil }

var profiles = []arr.QualityProfile{{ID: 1, Name: "HD-1080p"}, {ID: 4, Name: "Ultra-HD"}}

func movieTarget() *fakeTarget {
	return &fakeTarget{
		lookup:   &arr.Lookup{Title: "Arrival", Year: 2016, TMDBID: 329865},
		profiles: profiles,
		folders:  []arr.RootFolder{{ID: 1, Path: "/movies"}},
	}
}

func TestAddAsksQualityOncePerTitle(t *testing.T) {
	target := movieTarget()
	r := &Requester{Radarr: target, MinimumAvailability: "released"}
	ch := &fakeChooser{quality: profiles[1]}
	ctx := context.Background()

	for _, id := range []int{329865, 603} {
		res, err := r.Add(ctx, Item{Kind: media.Movies, TMDBID: id}, ch)
		if err != nil {
			t.Fatal(err)
		}
		if res.QualityProfile != "Ultra-HD" || res.RootFolder != "/movies" || res.App != "Radarr" {
			t.Fatalf("result = %+v", res)
		}
	}
	if len(ch.qualityAsked) != 2 {
		t.Fatalf("quality asked %d times, want 2", len(ch.qualityAsked))
	}
	if ch.qualityAsked[0].Title != "Arrival" {
		t.Fatalf("chooser item title = %q, want lookup title", ch.qualityAsked[0].Title)
	}
	want := arr.AddOptions{QualityProfileID: 4, RootFolderPath: "/movies", MinimumAvailability: "released", Search: true}
	if target.added[0] != want {
		t.Fatalf("add options = %+v, want %+v", target.added[0], want)
	}
}

func TestAddRejectsUnknownProfile(t *testing.T) {
	target := movieTarget()
	r := &Requester{Radarr: target}
	_, err := r.Add(context.Background(), Item{Kind: media.Movies, TMDBID: 1}, &fakeChooser{quality: arr.QualityProfile{ID: 99, Name: "made up"}})
	if err == nil || len(target.added) != 0 {
		t.Fatalf("err = %v, added = %d", err, len(target.added))
	}
}

func TestAddCancelAddsNothing(t *testing.T) {
	target := movieTarget()
	r := &Requester{Radarr: target}
	_, err := r.Add(context.Background(), Item{Kind: media.Movies, TMDBID: 1}, &fakeChooser{err: ErrCancelled})
	if !errors.Is(err, ErrCancelled) || len(target.added) != 0 {
		t.Fatalf("err = %v, added = %d", err, len(target.added))
	}
}

func TestAddNilChooser(t *testing.T) {
	target := movieTarget()
	r := &Requester{Radarr: target}
	if _, err := r.Add(context.Background(), Item{Kind: media.Movies, TMDBID: 1}, nil); err == nil {
		t.Fatal("want error for nil chooser")
	}
	if len(target.lookedUp) != 0 {
		t.Fatal("nil chooser should fail before lookup")
	}
}

func TestAddAlreadyInLibraryNeverAsks(t *testing.T) {
	target := movieTarget()
	target.lookup.LibraryID = 7
	ch := &fakeChooser{quality: profiles[0]}
	_, err := (&Requester{Radarr: target}).Add(context.Background(), Item{Kind: media.Movies, TMDBID: 1}, ch)
	if !errors.Is(err, ErrAlreadyInLibrary) {
		t.Fatalf("err = %v", err)
	}
	if len(ch.qualityAsked) != 0 {
		t.Fatal("chooser asked for a title already in the library")
	}
}

func TestAddNotConfigured(t *testing.T) {
	_, err := (&Requester{}).Add(context.Background(), Item{Kind: media.Series, TMDBID: 1}, &fakeChooser{})
	if !errors.Is(err, ErrNotConfigured) || !strings.Contains(err.Error(), "Sonarr") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddSeriesUsesTVDB(t *testing.T) {
	target := &fakeTarget{
		lookup:   &arr.Lookup{Title: "Severance", Year: 2022, TVDBID: 371980},
		profiles: profiles,
		folders:  []arr.RootFolder{{Path: "/tv"}},
	}
	r := &Requester{Sonarr: target, TMDB: fakeTVDB{95396: 371980}, MinimumAvailability: "released"}
	res, err := r.Add(context.Background(), Item{Kind: media.Series, TMDBID: 95396}, &fakeChooser{quality: profiles[0]})
	if err != nil {
		t.Fatal(err)
	}
	if target.lookedUp[0] != 371980 || res.App != "Sonarr" {
		t.Fatalf("lookedUp = %v, res = %+v", target.lookedUp, res)
	}
	if target.added[0].MinimumAvailability != "" {
		t.Fatal("minimum availability must not be sent to Sonarr")
	}

	_, err = r.Add(context.Background(), Item{Kind: media.Series, TMDBID: 1}, &fakeChooser{quality: profiles[0]})
	if err == nil || !strings.Contains(err.Error(), "no TVDB id") {
		t.Fatalf("err = %v", err)
	}
}

func TestRootFolderRules(t *testing.T) {
	two := []arr.RootFolder{{Path: "/a"}, {Path: "/b"}}
	ctx := context.Background()
	item := Item{Kind: media.Movies, TMDBID: 1}

	t.Run("configured match", func(t *testing.T) {
		target := movieTarget()
		target.folders = two
		ch := &fakeChooser{quality: profiles[0]}
		res, err := (&Requester{Radarr: target, RadarrRootFolder: "/b"}).Add(ctx, item, ch)
		if err != nil || res.RootFolder != "/b" || ch.folderAsked != 0 {
			t.Fatalf("res = %+v err = %v asked = %d", res, err, ch.folderAsked)
		}
	})
	t.Run("configured missing", func(t *testing.T) {
		target := movieTarget()
		target.folders = two
		_, err := (&Requester{Radarr: target, RadarrRootFolder: "/c"}).Add(ctx, item, &fakeChooser{quality: profiles[0]})
		if err == nil || len(target.added) != 0 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("several asks", func(t *testing.T) {
		target := movieTarget()
		target.folders = two
		ch := &fakeChooser{quality: profiles[0], folder: arr.RootFolder{Path: "/a"}}
		res, err := (&Requester{Radarr: target}).Add(ctx, item, ch)
		if err != nil || res.RootFolder != "/a" || ch.folderAsked != 1 {
			t.Fatalf("res = %+v err = %v asked = %d", res, err, ch.folderAsked)
		}
	})
	t.Run("several invalid choice", func(t *testing.T) {
		target := movieTarget()
		target.folders = two
		ch := &fakeChooser{quality: profiles[0], folder: arr.RootFolder{Path: "/nope"}}
		if _, err := (&Requester{Radarr: target}).Add(ctx, item, ch); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("none", func(t *testing.T) {
		target := movieTarget()
		target.folders = nil
		if _, err := (&Requester{Radarr: target}).Add(ctx, item, &fakeChooser{quality: profiles[0]}); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestFixedChooser(t *testing.T) {
	ctx := context.Background()
	item := Item{Title: "Arrival", Year: 2016}

	for _, in := range []string{"ultra-hd", "4", " HD-1080p "} {
		if _, err := (FixedChooser{QualityProfile: in}).ChooseQualityProfile(ctx, item, "Radarr", profiles); err != nil {
			t.Errorf("%q: %v", in, err)
		}
	}
	p, _ := FixedChooser{QualityProfile: "4"}.ChooseQualityProfile(ctx, item, "Radarr", profiles)
	if p.Name != "Ultra-HD" {
		t.Fatalf("id match = %+v", p)
	}
	if _, err := (FixedChooser{}).ChooseQualityProfile(ctx, item, "Radarr", profiles); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty: %v", err)
	}
	if _, err := (FixedChooser{QualityProfile: "SD"}).ChooseQualityProfile(ctx, item, "Radarr", profiles); err == nil || !strings.Contains(err.Error(), "HD-1080p") {
		t.Fatalf("unknown: %v", err)
	}

	folders := []arr.RootFolder{{Path: "/a"}, {Path: "/b"}}
	if f, err := (FixedChooser{RootFolder: "/b"}).ChooseRootFolder(ctx, item, "Radarr", folders); err != nil || f.Path != "/b" {
		t.Fatalf("folder = %+v err = %v", f, err)
	}
	if _, err := (FixedChooser{}).ChooseRootFolder(ctx, item, "Radarr", folders); err == nil || !strings.Contains(err.Error(), "/a, /b") {
		t.Fatalf("empty folder: %v", err)
	}
	if _, err := (FixedChooser{RootFolder: "/c"}).ChooseRootFolder(ctx, item, "Radarr", folders); err == nil {
		t.Fatal("want error for unknown folder")
	}
}
