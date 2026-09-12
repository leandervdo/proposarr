// Package request adds an accepted pick to Radarr or Sonarr. Every add asks
// its Chooser for a quality profile; there is no default profile.
package request

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
)

var (
	ErrAlreadyInLibrary = errors.New("already in library")
	ErrNotInLibrary     = errors.New("not in library")
	ErrNotConfigured    = errors.New("not configured")
	ErrUnknownProfile   = errors.New("unknown quality profile")
	ErrCancelled        = errors.New("cancelled")
)

type Item struct {
	Kind   media.Kind
	TMDBID int
	Title  string
	Year   int
}

func (i Item) Label() string { return media.Label(i.Title, i.Year) }

// Target is a Radarr (looks up by TMDB id) or Sonarr (looks up by TVDB id) client.
type Target interface {
	Lookup(ctx context.Context, id int) (*arr.Lookup, error)
	QualityProfiles(ctx context.Context) ([]arr.QualityProfile, error)
	RootFolders(ctx context.Context) ([]arr.RootFolder, error)
	Add(ctx context.Context, l *arr.Lookup, o arr.AddOptions) (arr.Added, error)
}

type TVDBResolver interface {
	TVDBID(ctx context.Context, tmdbID int) (int, error)
}

// Chooser asks the user. It is called for every title that is added.
type Chooser interface {
	ChooseQualityProfile(ctx context.Context, item Item, app string, profiles []arr.QualityProfile) (arr.QualityProfile, error)
	ChooseRootFolder(ctx context.Context, item Item, app string, folders []arr.RootFolder) (arr.RootFolder, error)
	// ChooseFallback is asked for movies whose search Proposarr follows.
	ChooseFallback(ctx context.Context, item Item, profile arr.QualityProfile) (Fallback, error)
}

type Requester struct {
	Radarr, Sonarr Target
	TMDB           TVDBResolver
	// RadarrSearch, when set, lets Result.Follow follow the search Radarr runs
	// for a new movie.
	RadarrSearch                       Searcher
	RadarrRootFolder, SonarrRootFolder string
	MinimumAvailability                string
}

type Result struct {
	App            string
	ID             int
	Title          string
	QualityProfile string
	RootFolder     string
	// Follow, set for movies added with RadarrSearch, waits for Radarr's search
	// and reports what it did. It can take about a minute, so callers may run it
	// in the background; the check's Profile is the movie's profile by then.
	Follow func(ctx context.Context) ReleaseCheck
}

// Add looks the title up, asks ch for a quality profile (and a root folder when
// it is ambiguous), then adds it with the app's own search. For a movie with
// RadarrSearch set it also asks what to do when nothing fits; see Result.Follow.
func (r *Requester) Add(ctx context.Context, item Item, ch Chooser) (Result, error) {
	if ch == nil {
		return Result{}, errors.New("a quality profile must be chosen for every title: no chooser given")
	}
	app, target, rootFolder, lookupID := "Radarr", r.Radarr, r.RadarrRootFolder, item.TMDBID
	if item.Kind == media.Series {
		app, target, rootFolder = "Sonarr", r.Sonarr, r.SonarrRootFolder
	}
	if target == nil {
		return Result{}, fmt.Errorf("%s: %w", app, ErrNotConfigured)
	}

	if item.Kind == media.Series {
		if r.TMDB == nil {
			return Result{}, fmt.Errorf("TMDB: %w", ErrNotConfigured)
		}
		tvdb, err := r.TMDB.TVDBID(ctx, item.TMDBID)
		if err != nil {
			return Result{}, fmt.Errorf("resolve TVDB id for %s: %w", item.Label(), err)
		}
		if tvdb == 0 {
			return Result{}, fmt.Errorf("%s has no TVDB id, Sonarr cannot add it", item.Label())
		}
		lookupID = tvdb
	}

	l, err := target.Lookup(ctx, lookupID)
	if err != nil {
		return Result{}, fmt.Errorf("%s lookup %s: %w", app, item.Label(), err)
	}
	if l == nil {
		return Result{}, fmt.Errorf("%s lookup %s: not found", app, item.Label())
	}
	if l.Title != "" {
		item.Title = l.Title
	}
	if l.Year > 0 {
		item.Year = l.Year
	}
	if l.LibraryID > 0 {
		return Result{}, fmt.Errorf("%s: %w", item.Label(), ErrAlreadyInLibrary)
	}

	profiles, err := target.QualityProfiles(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%s quality profiles: %w", app, err)
	}
	if len(profiles) == 0 {
		return Result{}, fmt.Errorf("%s has no quality profiles", app)
	}
	qp, err := ch.ChooseQualityProfile(ctx, item, app, profiles)
	if err != nil {
		return Result{}, err
	}
	if !hasProfile(profiles, qp.ID) {
		return Result{}, fmt.Errorf("quality profile %d (%q) is not a %s profile", qp.ID, qp.Name, app)
	}

	folders, err := target.RootFolders(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("%s root folders: %w", app, err)
	}
	folder, err := pickRootFolder(ctx, item, app, rootFolder, folders, ch)
	if err != nil {
		return Result{}, err
	}

	follow := item.Kind != media.Series && r.RadarrSearch != nil
	fallback := FallbackWait
	if follow {
		if fallback, err = ch.ChooseFallback(ctx, item, qp); err != nil {
			return Result{}, err
		}
	}

	opts := arr.AddOptions{QualityProfileID: qp.ID, RootFolderPath: folder.Path, Search: true}
	if item.Kind != media.Series {
		opts.MinimumAvailability = r.MinimumAvailability
	}
	added, err := target.Add(ctx, l, opts)
	if errors.Is(err, arr.ErrAlreadyAdded) {
		return Result{}, fmt.Errorf("%s: %w", item.Label(), ErrAlreadyInLibrary)
	}
	if err != nil {
		return Result{}, fmt.Errorf("%s add %s: %w", app, item.Label(), err)
	}
	title := added.Title
	if title == "" {
		title = item.Title
	}
	res := Result{App: app, ID: added.ID, Title: title, QualityProfile: qp.Name, RootFolder: folder.Path}
	if follow {
		res.Follow = func(ctx context.Context) ReleaseCheck {
			return r.followNewMovie(ctx, added.ID, qp, profiles, fallback)
		}
	}
	return res, nil
}

func pickRootFolder(ctx context.Context, item Item, app, configured string, folders []arr.RootFolder, ch Chooser) (arr.RootFolder, error) {
	if len(folders) == 0 {
		return arr.RootFolder{}, fmt.Errorf("%s has no root folders", app)
	}
	if configured != "" {
		for _, f := range folders {
			if f.Path == configured {
				return f, nil
			}
		}
		return arr.RootFolder{}, fmt.Errorf("configured %s root folder %q not found (have %s)", app, configured, folderPaths(folders))
	}
	if len(folders) == 1 {
		return folders[0], nil
	}
	f, err := ch.ChooseRootFolder(ctx, item, app, folders)
	if err != nil {
		return arr.RootFolder{}, err
	}
	for _, have := range folders {
		if have.Path == f.Path {
			return have, nil
		}
	}
	return arr.RootFolder{}, fmt.Errorf("root folder %q is not a %s root folder", f.Path, app)
}

func hasProfile(profiles []arr.QualityProfile, id int) bool {
	for _, p := range profiles {
		if p.ID == id {
			return true
		}
	}
	return false
}

func folderPaths(folders []arr.RootFolder) string {
	paths := make([]string, len(folders))
	for i, f := range folders {
		paths[i] = f.Path
	}
	return strings.Join(paths, ", ")
}

// FixedChooser answers from flags, for adding one scripted title.
type FixedChooser struct {
	QualityProfile string   // name (case-insensitive) or numeric id
	RootFolder     string   // exact path
	Fallback       Fallback // empty means FallbackWait
}

func (f FixedChooser) ChooseQualityProfile(_ context.Context, item Item, app string, profiles []arr.QualityProfile) (arr.QualityProfile, error) {
	want := strings.TrimSpace(f.QualityProfile)
	if want == "" {
		return arr.QualityProfile{}, fmt.Errorf("a quality profile is required for %s: pass --quality-profile", item.Label())
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Name, want) {
			return p, nil
		}
	}
	if id, err := strconv.Atoi(want); err == nil {
		for _, p := range profiles {
			if p.ID == id {
				return p, nil
			}
		}
	}
	names := make([]string, len(profiles))
	for i, p := range profiles {
		names[i] = fmt.Sprintf("%s (%d)", p.Name, p.ID)
	}
	return arr.QualityProfile{}, fmt.Errorf("%s has no quality profile %q (have %s)", app, want, strings.Join(names, ", "))
}

func (f FixedChooser) ChooseRootFolder(_ context.Context, item Item, app string, folders []arr.RootFolder) (arr.RootFolder, error) {
	if f.RootFolder == "" {
		return arr.RootFolder{}, fmt.Errorf("%s has several root folders, pass --root-folder for %s (have %s)", app, item.Label(), folderPaths(folders))
	}
	for _, fo := range folders {
		if fo.Path == f.RootFolder {
			return fo, nil
		}
	}
	return arr.RootFolder{}, fmt.Errorf("%s has no root folder %q (have %s)", app, f.RootFolder, folderPaths(folders))
}

func (f FixedChooser) ChooseFallback(context.Context, Item, arr.QualityProfile) (Fallback, error) {
	return ParseFallback(string(f.Fallback))
}
