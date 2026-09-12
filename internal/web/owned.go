package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/store"
)

// RadarrLibrary reads movies in the Radarr library live (arr.Radarr).
type RadarrLibrary interface {
	MovieByTMDB(ctx context.Context, tmdbID int) (arr.Movie, error)
	Queue(ctx context.Context, movieID int) ([]arr.QueueItem, error)
	QualityProfiles(ctx context.Context) ([]arr.QualityProfile, error)
}

// The status of an owned title.
const (
	OwnedDownloaded  = "downloaded"
	OwnedDownloading = "downloading"
	OwnedUnreleased  = "unreleased"
	OwnedUnmonitored = "unmonitored"
	OwnedMissing     = "missing"
	OwnedUnknown     = "unknown"
	OwnedInLibrary   = "in_library" // series
)

// ownedConcurrency is how many owned movies are read from Radarr at once.
const ownedConcurrency = 4

// OwnedTitle is an owned match with its live state in Radarr or Sonarr.
type OwnedTitle struct {
	TMDBID    int                  `json:"tmdb_id"`
	Kind      media.Kind           `json:"kind"`
	Title     string               `json:"title"`
	Year      int                  `json:"year,omitempty"`
	PosterURL string               `json:"poster_url,omitempty"`
	Status    string               `json:"status"`
	Radarr    *RadarrMovie         `json:"radarr,omitempty"` // movies, when Radarr could be read
	Search    *store.LibrarySearch `json:"search,omitempty"` // the latest search Proposarr started
	Error     string               `json:"error,omitempty"`  // status unknown: why
}

// RadarrMovie is a movie's state in Radarr.
type RadarrMovie struct {
	ID               int          `json:"id"`
	Monitored        bool         `json:"monitored"`
	HasFile          bool         `json:"has_file"`
	Available        bool         `json:"available"`
	QualityProfileID int          `json:"quality_profile_id"`
	QualityProfile   string       `json:"quality_profile"`
	FileQuality      string       `json:"file_quality,omitempty"`
	SizeOnDisk       int64        `json:"size_on_disk,omitempty"`
	Queue            *RadarrQueue `json:"queue,omitempty"`
}

// RadarrQueue is a movie's release in Radarr's download queue.
type RadarrQueue struct {
	Status   string   `json:"status"`
	Progress *float64 `json:"progress,omitempty"` // 0-100
	Quality  string   `json:"quality,omitempty"`
	Title    string   `json:"title,omitempty"`
}

func (s *Server) runOwned(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, _, err := s.o.Store.GetRun(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.internalError(w, "get run", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	writeJSON(w, http.StatusOK, s.ownedTitles(ctx, run.Kind, run.Owned))
}

// searchLibraryMovie gets a movie that is in Radarr without a file: Radarr
// monitors it, moves it to the chosen quality profile and searches, and
// Proposarr follows that search in the background.
func (s *Server) searchLibraryMovie(w http.ResponseWriter, r *http.Request) {
	tmdbID, err := strconv.Atoi(r.PathValue("tmdb_id"))
	if err != nil || tmdbID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid tmdb_id")
		return
	}
	var body struct {
		QualityProfileID int    `json:"quality_profile_id"`
		IfNothingFits    string `json:"if_nothing_fits"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// There is no default quality profile: every title needs an explicit choice.
	if body.QualityProfileID <= 0 {
		writeError(w, http.StatusBadRequest, "quality_profile_id is required: choose a quality profile for this movie")
		return
	}
	fallback, err := request.ParseFallback(body.IfNothingFits)
	if err != nil {
		writeError(w, http.StatusBadRequest, "if_nothing_fits must be switch or wait")
		return
	}
	if s.o.Adder == nil || s.o.Store == nil {
		writeError(w, http.StatusBadRequest, "adding titles is not configured")
		return
	}
	// Claimed before Radarr is touched, so two requests cannot both start a search.
	if !s.claimSearch(tmdbID) {
		writeError(w, http.StatusConflict, "Radarr is still searching for this movie")
		return
	}
	followed := false
	defer func() {
		if !followed {
			s.releaseSearch(tmdbID)
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	res, err := s.o.Adder.SearchExisting(ctx, tmdbID, body.QualityProfileID, fallback)
	switch {
	case err == nil:
	case errors.Is(err, request.ErrUnknownProfile), errors.Is(err, request.ErrNotConfigured):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, request.ErrNotInLibrary):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, request.ErrHasFile):
		writeError(w, http.StatusConflict, err.Error())
		return
	default:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	search := store.LibrarySearch{Kind: media.Movies, TMDBID: tmdbID, TargetID: res.ID, QualityProfile: res.QualityProfile, RequestedAt: s.now().UTC()}
	if res.Follow != nil {
		search.Release = releaseJSON(request.Checking(res.QualityProfile))
	}
	if err := s.o.Store.RecordLibrarySearch(r.Context(), search); err != nil {
		s.internalError(w, "record library search", fmt.Errorf("searching in Radarr but not recorded: %w", err))
		return
	}
	s.log.Info("library search started", "title", res.Title, "quality_profile", res.QualityProfile)
	title := s.ownedMovie(ctx, tmdbID)
	s.events.publish("owned.updated", title)
	writeJSON(w, http.StatusOK, title)
	followed = s.followLibrarySearch(tmdbID, res)
}

// followLibrarySearch follows Radarr's search for a library movie in the
// background, then stores the outcome and announces the movie. It reports
// whether it started; the movie stays claimed until the outcome is stored.
func (s *Server) followLibrarySearch(tmdbID int, res request.Result) bool {
	if res.Follow == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false // reopening the store marks the check searching
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		check := res.Follow(s.ctx)
		s.log.Info("library search check", "title", res.Title, "status", check.Status, "quality_profile", check.Profile,
			"switched_from", check.SwitchedFrom, "found", check.Found, "alternatives", len(check.Alternatives), "error", check.Error)
		// The outcome is stored even while the server shuts down.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 10*time.Second)
		defer cancel()
		err := s.o.Store.UpdateLibrarySearch(sctx, media.Movies, tmdbID, check.Profile, releaseJSON(check))
		s.releaseSearch(tmdbID)
		if err != nil {
			s.log.Error("store library search check", "tmdb_id", tmdbID, "err", err)
			return
		}
		if s.ctx.Err() != nil {
			return // nobody is listening any more
		}
		ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
		defer cancel()
		s.events.publish("owned.updated", s.ownedMovie(ctx, tmdbID))
	}()
	return true
}

// claimSearch marks a library movie's search as followed; false when one already is.
func (s *Server) claimSearch(tmdbID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searching[tmdbID] {
		return false
	}
	s.searching[tmdbID] = true
	return true
}

func (s *Server) releaseSearch(tmdbID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.searching, tmdbID)
}

// ownedMovie is a library movie's live state, with its latest search.
func (s *Server) ownedMovie(ctx context.Context, tmdbID int) OwnedTitle {
	return s.ownedTitles(ctx, media.Movies, []pipeline.OwnedMatch{{TMDBID: tmdbID}})[0]
}

// ownedTitles reads the live state of owned matches, in their order. Movies
// are read from Radarr a few at a time; series are only in the library.
// Posters, and titles a match lacks, come from the library snapshot, and the
// latest search Proposarr started for each movie is merged in.
func (s *Server) ownedTitles(ctx context.Context, kind media.Kind, matches []pipeline.OwnedMatch) []OwnedTitle {
	out := make([]OwnedTitle, len(matches))
	if len(matches) == 0 {
		return out
	}
	lib := s.libraryTitles(ctx, kind)
	for i, m := range matches {
		t := OwnedTitle{TMDBID: m.TMDBID, Kind: kind, Title: m.Title, Year: m.Year, Status: OwnedInLibrary}
		if l, ok := lib[m.TMDBID]; ok {
			t.PosterURL = l.PosterURL
			if t.Title == "" {
				t.Title, t.Year = l.Title, l.Year
			}
		}
		out[i] = t
	}
	if kind != media.Movies {
		return out
	}

	var radarr RadarrLibrary
	if s.o.Radarr != nil {
		radarr = s.o.Radarr()
	}
	if radarr == nil {
		for i := range out {
			out[i].Status, out[i].Error = OwnedUnknown, "Radarr is not configured"
		}
	} else {
		names := map[int]string{}
		profiles, err := radarr.QualityProfiles(ctx)
		if err != nil {
			s.log.Warn("radarr quality profiles", "err", err)
		}
		for _, p := range profiles {
			names[p.ID] = p.Name
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, ownedConcurrency)
		for i := range out {
			wg.Add(1)
			go func(t *OwnedTitle) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				readMovie(ctx, radarr, names, t)
			}(&out[i])
		}
		wg.Wait()
	}
	s.mergeSearches(ctx, out)
	return out
}

// readMovie fills an owned movie's state from Radarr. When Radarr cannot be
// read the status is unknown, with the reason.
func readMovie(ctx context.Context, radarr RadarrLibrary, profiles map[int]string, t *OwnedTitle) {
	m, err := radarr.MovieByTMDB(ctx, t.TMDBID)
	switch {
	case errors.Is(err, arr.ErrNotFound):
		t.Status, t.Error = OwnedUnknown, "not in Radarr any more"
		return
	case err != nil:
		t.Status, t.Error = OwnedUnknown, err.Error()
		return
	}
	if t.Title == "" {
		t.Title, t.Year = m.Title, m.Year
	}
	t.Radarr = &RadarrMovie{ID: m.ID, Monitored: m.Monitored, HasFile: m.HasFile, Available: m.Available,
		QualityProfileID: m.QualityProfileID, QualityProfile: profiles[m.QualityProfileID], FileQuality: m.FileQuality, SizeOnDisk: m.SizeOnDisk}
	queue, err := radarr.Queue(ctx, m.ID)
	if err != nil {
		t.Status, t.Error = OwnedUnknown, fmt.Sprintf("read Radarr's queue: %v", err)
		return
	}
	if len(queue) > 0 {
		q := queue[0]
		t.Radarr.Queue = &RadarrQueue{Status: q.Status, Quality: q.Quality, Title: q.Title}
		if p, ok := q.Progress(); ok {
			t.Radarr.Queue.Progress = &p
		}
	}
	t.Status = movieStatus(m, len(queue) > 0)
}

// movieStatus is the first status that applies to a movie in Radarr.
func movieStatus(m arr.Movie, queued bool) string {
	switch {
	case m.HasFile:
		return OwnedDownloaded
	case queued:
		return OwnedDownloading
	case !m.Available:
		return OwnedUnreleased
	case !m.Monitored:
		return OwnedUnmonitored
	}
	return OwnedMissing
}

// mergeSearches attaches the latest search Proposarr started to each movie.
func (s *Server) mergeSearches(ctx context.Context, titles []OwnedTitle) {
	if s.o.Store == nil {
		return
	}
	ids := make([]int, len(titles))
	for i, t := range titles {
		ids[i] = t.TMDBID
	}
	searches, err := s.o.Store.LibrarySearches(ctx, media.Movies, ids)
	if err != nil {
		s.log.Error("library searches", "err", err)
		return
	}
	for i := range titles {
		if ls, ok := searches[titles[i].TMDBID]; ok {
			titles[i].Search = &ls
		}
	}
}

// libraryTitles indexes the library snapshot by TMDB id; empty when it is unavailable.
func (s *Server) libraryTitles(ctx context.Context, kind media.Kind) map[int]media.Title {
	out := map[int]media.Title{}
	if s.o.Library == nil {
		return out
	}
	titles, err := s.o.Library(ctx, kind)
	if err != nil {
		return out
	}
	for _, t := range titles {
		if _, ok := out[t.TMDBID]; t.TMDBID > 0 && !ok {
			out[t.TMDBID] = t
		}
	}
	return out
}
