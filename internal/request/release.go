package request

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
)

// Searcher follows Radarr's searches for a movie in its library (arr.Radarr).
// Radarr does every search and grab; Proposarr only reads what happened and
// may switch the quality profile and ask Radarr to search again.
type Searcher interface {
	Movie(ctx context.Context, id int) (arr.Movie, error)
	MovieByTMDB(ctx context.Context, tmdbID int) (arr.Movie, error)
	MovieSearches(ctx context.Context, movieID int) ([]arr.Command, error)
	Command(ctx context.Context, id int) (arr.Command, error)
	SearchMovie(ctx context.Context, movieID int) (arr.Command, error)
	Grabs(ctx context.Context, movieID int) ([]arr.Grab, error)
	Pending(ctx context.Context, movieID int) ([]arr.Grab, error)
	Releases(ctx context.Context, movieID int) ([]arr.Release, error)
	SetQualityProfile(ctx context.Context, movieID, profileID int) error
	SetMonitored(ctx context.Context, movieID int, monitored bool) error
}

// Fallback is what to do when Radarr's search finds releases for a movie but
// none fit its quality profile. It is chosen per title, like the profile.
type Fallback string

const (
	FallbackWait   Fallback = "wait"   // keep the profile; Radarr grabs a release once one fits
	FallbackSwitch Fallback = "switch" // switch to the highest profile ranked below that would grab a release, and search again
)

// ParseFallback reads a fallback; empty means FallbackWait.
func ParseFallback(s string) (Fallback, error) {
	switch f := Fallback(s); f {
	case "", FallbackWait:
		return FallbackWait, nil
	case FallbackSwitch:
		return f, nil
	}
	return "", fmt.Errorf("unknown fallback %q: use switch or wait", s)
}

type CheckStatus string

const (
	CheckChecking    CheckStatus = "checking"    // Proposarr is still following Radarr's search
	CheckGrabbed     CheckStatus = "grabbed"     // Radarr grabbed Release
	CheckPending     CheckStatus = "pending"     // Radarr holds Release for a delay profile
	CheckWaiting     CheckStatus = "waiting"     // Radarr grabbed nothing and keeps looking
	CheckSearching   CheckStatus = "searching"   // Radarr was still searching when Proposarr stopped following it
	CheckUnavailable CheckStatus = "unavailable" // not released yet for the movie's minimum availability
	CheckFailed      CheckStatus = "failed"      // the outcome could not be read, see Error
)

// ReleaseCheck is what Radarr's search did for a movie that was just added or switched.
type ReleaseCheck struct {
	Status       CheckStatus     `json:"status"`
	Profile      string          `json:"profile"`                 // the profile Radarr searched with
	SwitchedFrom string          `json:"switched_from,omitempty"` // nothing fit this profile, so Proposarr switched away from it
	Release      *ReleaseInfo    `json:"release,omitempty"`
	Found        int             `json:"found"`        // waiting: releases the explaining search found
	Qualities    []QualityCount  `json:"qualities"`    // waiting: what was found, most common first
	Alternatives []ProfileOption `json:"alternatives"` // waiting: other profiles that would grab a release now, in ranking order
	Error        string          `json:"error,omitempty"`
}

// Checking is the check of a search Proposarr has started to follow.
func Checking(profile string) ReleaseCheck {
	return ReleaseCheck{Status: CheckChecking, Profile: profile, Qualities: []QualityCount{}, Alternatives: []ProfileOption{}}
}

type ReleaseInfo struct {
	Title    string `json:"title"`
	Quality  string `json:"quality"`
	Size     int64  `json:"size,omitempty"`
	Indexer  string `json:"indexer,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Seeders  *int   `json:"seeders,omitempty"` // torrents from a search only
}

type QualityCount struct {
	Quality string `json:"quality"`
	Count   int    `json:"count"`
}

// ProfileOption is another quality profile and the releases it would accept.
type ProfileOption struct {
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Count int         `json:"count"`
	Best  ReleaseInfo `json:"best"` // the one it would most likely grab
}

// How often to look at Radarr's search, and how long to wait for it before
// reporting that it is still searching.
var (
	pollInterval = 1500 * time.Millisecond
	searchWait   = 45 * time.Second
)

// followNewMovie reports the search Radarr runs after adding a movie. When
// nothing fits the profile and fallback is FallbackSwitch, it switches once to
// the highest-ranked profile below it that would grab a release, and has Radarr
// search again.
func (r *Requester) followNewMovie(ctx context.Context, movieID int, profile arr.QualityProfile, profiles []arr.QualityProfile, fallback Fallback) ReleaseCheck {
	// Radarr queues this search itself, after refreshing the new movie.
	find := func(ctx context.Context) (arr.Command, bool, error) {
		cmds, err := r.RadarrSearch.MovieSearches(ctx, movieID)
		if err != nil || len(cmds) == 0 {
			return arr.Command{}, false, err
		}
		return cmds[0], true, nil
	}
	return r.followWithFallback(ctx, movieID, 0, profile, profiles, fallback, find)
}

// followWithFallback follows a search like followSearch. When nothing fits the
// profile and fallback is FallbackSwitch, it switches once to the highest-ranked
// profile below it that would grab a release, and has Radarr search again.
func (r *Requester) followWithFallback(ctx context.Context, movieID, grabsBefore int, profile arr.QualityProfile, profiles []arr.QualityProfile, fallback Fallback, find func(context.Context) (arr.Command, bool, error)) ReleaseCheck {
	check := r.followSearch(ctx, movieID, grabsBefore, profile, profiles, find)
	if fallback != FallbackSwitch || check.Status != CheckWaiting {
		return check
	}
	best, ok := fallbackChoice(check.Alternatives, rankedIDs(r.ProfileOrder, profiles), profile.ID)
	if !ok {
		return check
	}
	i := slices.IndexFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == best.ID })
	follow, err := r.startSwitch(ctx, movieID, profiles[i], profiles)
	if err != nil {
		check.Status, check.Error = CheckFailed, fmt.Sprintf("nothing fit %s, and switching to %s failed: %v", profile.Name, best.Name, err)
		return check
	}
	next := follow(ctx)
	next.SwitchedFrom = profile.Name
	return next
}

// startSwitch moves a movie to another quality profile and has Radarr search
// again, then returns how to follow that search. An error means the profile
// was not changed.
func (r *Requester) startSwitch(ctx context.Context, movieID int, profile arr.QualityProfile, profiles []arr.QualityProfile) (func(context.Context) ReleaseCheck, error) {
	grabs, err := r.RadarrSearch.Grabs(ctx, movieID)
	if err != nil {
		return nil, fmt.Errorf("read Radarr's history: %w", err)
	}
	if err := r.RadarrSearch.SetQualityProfile(ctx, movieID, profile.ID); err != nil {
		return nil, fmt.Errorf("set the quality profile: %w", err)
	}
	// A switch never switches again on its own.
	return r.startSearch(ctx, movieID, len(grabs), profile, profiles, FallbackWait), nil
}

// startSearch has Radarr search a library movie and returns how to follow that
// search. grabsBefore is the number of grabs in the movie's history before
// anything was changed. A search that did not start is reported by the follow,
// because the movie may already have been changed.
func (r *Requester) startSearch(ctx context.Context, movieID, grabsBefore int, profile arr.QualityProfile, profiles []arr.QualityProfile, fallback Fallback) func(context.Context) ReleaseCheck {
	cmd, err := r.RadarrSearch.SearchMovie(ctx, movieID)
	if err != nil {
		msg := fmt.Sprintf("start Radarr's search: %v", err)
		return func(context.Context) ReleaseCheck {
			check := Checking(profile.Name)
			check.Status, check.Error = CheckFailed, msg
			return check
		}
	}
	find := func(ctx context.Context) (arr.Command, bool, error) {
		c, err := r.RadarrSearch.Command(ctx, cmd.ID)
		return c, err == nil, err
	}
	return func(ctx context.Context) ReleaseCheck {
		return r.followWithFallback(ctx, movieID, grabsBefore, profile, profiles, fallback, find)
	}
}

// followSearch waits for a Radarr search of a movie, found by find, and reports
// what Radarr did. grabsBefore is the number of grabs in the movie's history
// before that search. When Radarr grabbed nothing, an interactive search
// explains why and finds the other profiles that would grab a release now.
func (r *Requester) followSearch(ctx context.Context, movieID, grabsBefore int, profile arr.QualityProfile, profiles []arr.QualityProfile, find func(context.Context) (arr.Command, bool, error)) ReleaseCheck {
	check := Checking(profile.Name)
	fail := func(err error) ReleaseCheck {
		check.Status, check.Error = CheckFailed, err.Error()
		if ctx.Err() != nil {
			// Proposarr stopped following; Radarr carries on.
			check.Status, check.Error = CheckSearching, ""
		}
		return check
	}
	movie, err := r.RadarrSearch.Movie(ctx, movieID)
	if err != nil {
		return fail(fmt.Errorf("read the movie from Radarr: %w", err))
	}
	if !movie.Available {
		check.Status = CheckUnavailable
		return check
	}
	finished, err := waitFor(ctx, find)
	switch {
	case err != nil:
		return fail(err)
	case !finished:
		check.Status = CheckSearching
		return check
	}
	grabs, err := r.RadarrSearch.Grabs(ctx, movieID)
	if err != nil {
		return fail(fmt.Errorf("read Radarr's history: %w", err))
	}
	if len(grabs) > grabsBefore {
		check.Status, check.Release = CheckGrabbed, grabInfo(grabs[0])
		return check
	}
	pending, err := r.RadarrSearch.Pending(ctx, movieID)
	if err != nil {
		return fail(fmt.Errorf("read Radarr's queue: %w", err))
	}
	if len(pending) > 0 {
		check.Status, check.Release = CheckPending, grabInfo(pending[0])
		return check
	}
	rels, err := r.RadarrSearch.Releases(ctx, movieID)
	if err != nil {
		return fail(fmt.Errorf("search the indexers: %w", err))
	}
	check.Status = CheckWaiting
	check.Found, check.Qualities = len(rels), countQualities(rels)
	check.Alternatives = orderAlternatives(Alternatives(rels, profiles, profile.ID, movie.OriginalLanguage), rankedIDs(r.ProfileOrder, profiles))
	return check
}

// waitFor polls find until the command it returns has finished. It reports
// false when the command is still running after searchWait, or when ctx ends.
func waitFor(ctx context.Context, find func(context.Context) (arr.Command, bool, error)) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, searchWait)
	defer cancel()
	for {
		cmd, ok, err := find(ctx)
		switch {
		case ctx.Err() != nil:
			return false, nil
		case err != nil:
			return false, fmt.Errorf("follow Radarr's search: %w", err)
		case ok && cmd.Done():
			if cmd.Status != "completed" {
				return false, fmt.Errorf("Radarr's search ended as %s", cmd.Status)
			}
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-time.After(pollInterval):
		}
	}
}

// SwitchProfile moves a movie already added to Radarr to another quality
// profile and has Radarr search again; Result.Follow follows that search. The
// profile is always the user's explicit choice, and there is no further switch.
func (r *Requester) SwitchProfile(ctx context.Context, item Item, movieID, profileID int) (Result, error) {
	if item.Kind == media.Series {
		return Result{}, errors.New("switching the quality profile after a search is only for movies")
	}
	if r.Radarr == nil || r.RadarrSearch == nil {
		return Result{}, fmt.Errorf("Radarr: %w", ErrNotConfigured)
	}
	movie, err := r.RadarrSearch.Movie(ctx, movieID)
	if errors.Is(err, arr.ErrNotFound) || (err == nil && movie.TMDBID != item.TMDBID) {
		return Result{}, fmt.Errorf("%s: %w", item.Label(), ErrNotInLibrary)
	}
	if err != nil {
		return Result{}, fmt.Errorf("Radarr movie %s: %w", item.Label(), err)
	}
	profiles, err := r.Radarr.QualityProfiles(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("Radarr quality profiles: %w", err)
	}
	i := slices.IndexFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == profileID })
	if i < 0 {
		return Result{}, fmt.Errorf("Radarr quality profile %d: %w", profileID, ErrUnknownProfile)
	}
	follow, err := r.startSwitch(ctx, movie.ID, profiles[i], profiles)
	if err != nil {
		return Result{}, fmt.Errorf("Radarr %s: %w", item.Label(), err)
	}
	return Result{App: "Radarr", ID: movie.ID, Title: movie.Title, QualityProfile: profiles[i].Name, Follow: follow}, nil
}

// SearchExisting gets a movie that is in Radarr but has no file. It monitors
// the movie when it is not, moves it to profileID when that differs, and has
// Radarr search; Result.Follow follows that search exactly like after an add,
// including fallback. The profile is always the user's explicit choice.
func (r *Requester) SearchExisting(ctx context.Context, tmdbID, profileID int, fallback Fallback) (Result, error) {
	if r.Radarr == nil || r.RadarrSearch == nil {
		return Result{}, fmt.Errorf("Radarr: %w", ErrNotConfigured)
	}
	movie, err := r.RadarrSearch.MovieByTMDB(ctx, tmdbID)
	if errors.Is(err, arr.ErrNotFound) {
		return Result{}, fmt.Errorf("movie tmdb:%d: %w", tmdbID, ErrNotInLibrary)
	}
	if err != nil {
		return Result{}, fmt.Errorf("Radarr movie tmdb:%d: %w", tmdbID, err)
	}
	label := media.Label(movie.Title, movie.Year)
	if movie.HasFile {
		return Result{}, fmt.Errorf("%s: %w", label, ErrHasFile)
	}
	profiles, err := r.Radarr.QualityProfiles(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("Radarr quality profiles: %w", err)
	}
	i := slices.IndexFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == profileID })
	if i < 0 {
		return Result{}, fmt.Errorf("Radarr quality profile %d: %w", profileID, ErrUnknownProfile)
	}
	// Counted before anything changes, so grabs from earlier searches are not reported as new.
	grabs, err := r.RadarrSearch.Grabs(ctx, movie.ID)
	if err != nil {
		return Result{}, fmt.Errorf("Radarr %s: read Radarr's history: %w", label, err)
	}
	if !movie.Monitored {
		if err := r.RadarrSearch.SetMonitored(ctx, movie.ID, true); err != nil {
			return Result{}, fmt.Errorf("Radarr %s: monitor the movie: %w", label, err)
		}
	}
	if movie.QualityProfileID != profileID {
		if err := r.RadarrSearch.SetQualityProfile(ctx, movie.ID, profileID); err != nil {
			return Result{}, fmt.Errorf("Radarr %s: set the quality profile: %w", label, err)
		}
	}
	follow := r.startSearch(ctx, movie.ID, len(grabs), profiles[i], profiles, fallback)
	return Result{App: "Radarr", ID: movie.ID, Title: movie.Title, QualityProfile: profiles[i].Name, Follow: follow}, nil
}

// profileRejections match Radarr's rejections for the checks Alternatives
// redoes per profile: quality, custom format score and language.
var profileRejections = []string{"not wanted in profile", "profile minimum", "wanted, but found"}

// Alternatives estimates from one search which other quality profiles would
// grab a release now, the way Radarr judges one: the quality must be allowed,
// the custom format score must reach the profile's minimum and the language
// must fit. A rejection that does not depend on the profile (seeders, size,
// availability, an unparsable title) rules a release out for every profile.
// Profiles without rules are skipped; the result keeps the order of profiles.
func Alternatives(rels []arr.Release, profiles []arr.QualityProfile, currentID, originalLanguage int) []ProfileOption {
	type candidate struct {
		rel         arr.Release
		pref, score int
	}
	type option struct {
		ProfileOption
		top candidate
	}
	var opts []option
	for _, p := range profiles {
		if p.ID == currentID || p.Rules == nil {
			continue
		}
		o := option{ProfileOption: ProfileOption{ID: p.ID, Name: p.Name}}
		for _, rel := range rels {
			pref, ok := p.Rules.Qualities[rel.QualityID]
			if !ok || !onlyProfileRejections(rel) || !languageFits(p.Rules.Language, rel.Languages, originalLanguage) {
				continue
			}
			score := p.Rules.Score(rel.CustomFormats)
			if score < p.Rules.MinFormatScore {
				continue
			}
			// Ties keep Radarr's order.
			c := candidate{rel, pref, score}
			if o.Count == 0 || cmp.Or(cmp.Compare(c.pref, o.top.pref), cmp.Compare(c.score, o.top.score), cmp.Compare(c.rel.Seeders, o.top.rel.Seeders)) > 0 {
				o.top = c
			}
			o.Count++
		}
		if o.Count > 0 {
			o.Best = releaseInfo(o.top.rel)
			opts = append(opts, o)
		}
	}
	out := make([]ProfileOption, len(opts))
	for i, o := range opts {
		out[i] = o.ProfileOption
	}
	return out
}

// ParseProfileList splits a comma-separated list of quality profile names or ids.
func ParseProfileList(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// rankedIDs resolves the profile ranking, by id or case-insensitive name, to
// profile ids, best first. Unknown entries are skipped.
func rankedIDs(entries []string, profiles []arr.QualityProfile) []int {
	var ids []int
	for _, e := range entries {
		i := slices.IndexFunc(profiles, func(p arr.QualityProfile) bool {
			return strconv.Itoa(p.ID) == e || strings.EqualFold(p.Name, e)
		})
		if i >= 0 && !slices.Contains(ids, profiles[i].ID) {
			ids = append(ids, profiles[i].ID)
		}
	}
	return ids
}

// orderAlternatives puts ranked profiles first, in ranking order; unranked
// profiles follow in Radarr's order.
func orderAlternatives(alts []ProfileOption, ranking []int) []ProfileOption {
	rank := func(o ProfileOption) int {
		if i := slices.Index(ranking, o.ID); i >= 0 {
			return i
		}
		return len(ranking)
	}
	slices.SortStableFunc(alts, func(a, b ProfileOption) int { return cmp.Compare(rank(a), rank(b)) })
	return alts
}

// hasLowerRanked reports whether a movie with profileID could fall back: the
// profile is ranked, with profiles below it.
func hasLowerRanked(ranking []int, profileID int) bool {
	i := slices.Index(ranking, profileID)
	return i >= 0 && i < len(ranking)-1
}

// fallbackChoice is the profile a movie with currentID falls back to: the
// highest-ranked profile below it that would grab a release. alts must be in
// ranking order. A higher-ranked or unranked profile is never chosen.
func fallbackChoice(alts []ProfileOption, ranking []int, currentID int) (ProfileOption, bool) {
	cur := slices.Index(ranking, currentID)
	if cur < 0 {
		return ProfileOption{}, false
	}
	for _, o := range alts {
		if slices.Index(ranking, o.ID) > cur {
			return o, true
		}
	}
	return ProfileOption{}, false
}

func onlyProfileRejections(rel arr.Release) bool {
	for _, msg := range rel.Rejections {
		if !slices.ContainsFunc(profileRejections, func(s string) bool { return strings.Contains(msg, s) }) {
			return false
		}
	}
	return true
}

// languageFits mirrors Radarr's language check. Releases without a parsed
// language and an unknown original language are given the benefit of the doubt.
func languageFits(want int, have []int, originalLanguage int) bool {
	switch {
	case want == arr.LanguageAny || len(have) == 0:
		return true
	case want == arr.LanguageOriginal:
		if originalLanguage <= 0 {
			return true
		}
		want = originalLanguage
	}
	return slices.Contains(have, want)
}

func countQualities(rels []arr.Release) []QualityCount {
	counts := map[string]int{}
	for _, rel := range rels {
		counts[rel.Quality]++
	}
	out := make([]QualityCount, 0, len(counts))
	for q, n := range counts {
		out = append(out, QualityCount{Quality: q, Count: n})
	}
	slices.SortFunc(out, func(a, b QualityCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Quality, b.Quality))
	})
	return out
}

func releaseInfo(rel arr.Release) ReleaseInfo {
	info := ReleaseInfo{Title: rel.Title, Quality: rel.Quality, Size: rel.Size, Indexer: rel.Indexer, Protocol: rel.Protocol}
	if rel.Protocol == "torrent" {
		seeders := rel.Seeders
		info.Seeders = &seeders
	}
	return info
}

func grabInfo(g arr.Grab) *ReleaseInfo {
	return &ReleaseInfo{Title: g.Title, Quality: g.Quality, Size: g.Size, Indexer: g.Indexer, Protocol: g.Protocol}
}
