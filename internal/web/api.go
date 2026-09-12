package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/leandervdo/proposarr/internal/arr"
	"github.com/leandervdo/proposarr/internal/config"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/settings"
	"github.com/leandervdo/proposarr/internal/store"
)

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	cfg := s.cfg()
	missing := settings.Missing(cfg)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.o.Version,
		"setup_required": len(missing) > 0,
		"missing":        missing,
		"claude_auth":    claudeAuth(cfg),
		"connections": map[string]bool{
			"radarr":   cfg.Radarr.Configured(),
			"sonarr":   cfg.Sonarr.Configured(),
			"tmdb":     cfg.TMDB.APIKey != "",
			"plex":     cfg.Plex.Configured(),
			"jellyfin": cfg.Jellyfin.Configured(),
		},
		"running": s.runningList(),
	})
}

func claudeAuth(cfg config.Config) string {
	name, _, _ := cfg.Claude.Auth()
	switch name {
	case config.EnvOAuthToken:
		return "oauth_token"
	case config.EnvAPIKey:
		return "api_key"
	}
	return "local"
}

func (s *Server) checkConnections(w http.ResponseWriter, r *http.Request) {
	results := []CheckResult{}
	if s.o.Check != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		if got := s.o.Check(ctx); got != nil {
			results = got
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicConfigOf(s.cfg()))
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	if s.o.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "store is not available")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, 500)
	}
	runs, err := s.o.Store.ListRuns(r.Context(), limit)
	if err != nil {
		s.internalError(w, "list runs", err)
		return
	}
	out := make([]store.Run, 0, len(runs))
	for _, run := range runs {
		run.Profile = nil
		out = append(out, normalizeRun(run))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, picks, err := s.o.Store.GetRun(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.internalError(w, "get run", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": normalizeRun(run), "picks": normalizePicks(picks)})
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string `json:"kind"`
		Vibe     string `json:"vibe"`
		UseTaste *bool  `json:"use_taste"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := media.ParseKind(body.Kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vibe := strings.TrimSpace(body.Vibe)
	useTaste := body.UseTaste == nil || *body.UseTaste
	if !useTaste && vibe == "" {
		writeError(w, http.StatusBadRequest, "describe what you are looking for")
		return
	}
	run, status, err := s.startRun(kind, vibe, useTaste)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) listPicks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f store.PickFilter
	if k := q.Get("kind"); k != "" {
		kind, err := media.ParseKind(k)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		f.Kind = kind
	}
	switch run := q.Get("run"); run {
	case "", "latest":
		f.LatestRun = true
	case "all":
		f.AllRuns = true
	default:
		id, err := strconv.ParseInt(run, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, `run must be "latest", "all" or a run id`)
			return
		}
		f.RunID = id
	}
	if v := q.Get("verdict"); v != "" {
		var verdict store.Verdict
		switch v {
		case "none":
			verdict = store.VerdictNone
		case string(store.VerdictAccepted), string(store.VerdictIgnored), string(store.VerdictLater):
			verdict = store.Verdict(v)
		default:
			writeError(w, http.StatusBadRequest, "verdict must be none, accepted, ignored or later")
			return
		}
		f.Verdict = &verdict
	}
	var bools [3]*bool
	for i, name := range []string{"added", "search", "distinct"} {
		b, err := queryBool(q, name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		bools[i] = b
	}
	f.Added = bools[0] != nil && *bools[0]
	f.Search = bools[1]
	f.Distinct = bools[2] != nil && *bools[2]
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxPickLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer from 1 to %d", store.MaxPickLimit))
			return
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		f.Offset = n
	}
	f.ExcludeTMDB = s.libraryIDs(r.Context(), f.Kind)
	picks, err := s.o.Store.ListPicks(r.Context(), f)
	if err != nil {
		s.internalError(w, "list picks", err)
		return
	}
	total, err := s.o.Store.CountPicks(r.Context(), f)
	if err != nil {
		s.internalError(w, "count picks", err)
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, normalizePicks(picks))
}

// libraryIDs returns the TMDB ids currently in the library for kind (both
// kinds when empty), so titles already in Radarr/Sonarr are not shown as picks.
// An unavailable library filters nothing.
func (s *Server) libraryIDs(ctx context.Context, kind media.Kind) []int {
	if s.o.Library == nil {
		return nil
	}
	kinds := []media.Kind{kind}
	if kind == "" {
		kinds = []media.Kind{media.Movies, media.Series}
	}
	var ids []int
	for _, k := range kinds {
		titles, err := s.o.Library(ctx, k)
		if err != nil {
			continue
		}
		for _, t := range titles {
			if t.TMDBID > 0 {
				ids = append(ids, t.TMDBID)
			}
		}
	}
	return ids
}

// queryBool parses an optional true|false query parameter; nil when absent.
func queryBool(q url.Values, name string) (*bool, error) {
	var b bool
	switch q.Get(name) {
	case "":
		return nil, nil
	case "true":
		b = true
	case "false":
	default:
		return nil, fmt.Errorf("%s must be true or false", name)
	}
	return &b, nil
}

func (s *Server) setVerdict(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Verdict   *string `json:"verdict"`
		LaterDays *int    `json:"later_days"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Verdict == nil {
		writeError(w, http.StatusBadRequest, `verdict is required: accepted, ignored, later, or "" to clear`)
		return
	}
	v := store.Verdict(*body.Verdict)
	switch v {
	case store.VerdictNone, store.VerdictAccepted, store.VerdictIgnored, store.VerdictLater:
	default:
		writeError(w, http.StatusBadRequest, `verdict must be accepted, ignored, later, or "" to clear`)
		return
	}
	pick, ok := s.loadPick(w, r, id)
	if !ok {
		return
	}
	var until *time.Time
	if v == store.VerdictLater {
		days := 30
		if body.LaterDays != nil && *body.LaterDays > 0 {
			days = *body.LaterDays
		}
		t := s.now().UTC().AddDate(0, 0, days)
		until = &t
	}
	if err := s.o.Store.SetVerdict(r.Context(), pick.Kind, pick.TMDBID, v, until); err != nil {
		s.internalError(w, "set verdict", err)
		return
	}
	s.respondPick(w, r, id)
}

func (s *Server) requestPick(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		QualityProfileID int    `json:"quality_profile_id"`
		RootFolder       string `json:"root_folder"`
		IfNothingFits    string `json:"if_nothing_fits"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// There is no default quality profile: every title needs an explicit choice.
	if body.QualityProfileID <= 0 {
		writeError(w, http.StatusBadRequest, "quality_profile_id is required: choose a quality profile for this title")
		return
	}
	fallback, err := request.ParseFallback(body.IfNothingFits)
	if err != nil {
		writeError(w, http.StatusBadRequest, "if_nothing_fits must be switch or wait")
		return
	}
	if s.o.Adder == nil {
		writeError(w, http.StatusBadRequest, "adding titles is not configured")
		return
	}
	pick, ok := s.loadPick(w, r, id)
	if !ok {
		return
	}
	app := appOf(pick.Kind)
	ch := &choice{profileID: body.QualityProfileID, rootFolder: strings.TrimSpace(body.RootFolder), fallback: fallback}
	if s.o.App != nil {
		if _, root, ok := s.o.App(app); ok {
			ch.defaultRoot = root
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	item := request.Item{Kind: pick.Kind, TMDBID: pick.TMDBID, Title: pick.Title, Year: pick.Year}
	res, err := s.o.Adder.Add(ctx, item, ch)
	var ce *choiceError
	switch {
	case err == nil:
	case errors.Is(err, request.ErrAlreadyInLibrary):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, request.ErrNotConfigured), errors.As(err, &ce):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		failed := store.Request{PickID: pick.ID, App: app, QualityProfile: ch.chosenProfile, RootFolder: ch.chosenRoot,
			Status: "failed", Error: err.Error(), RequestedAt: s.now().UTC()}
		if rerr := s.o.Store.RecordRequest(r.Context(), failed); rerr != nil {
			s.log.Error("record failed request", "pick_id", pick.ID, "err", rerr)
		} else if updated, gerr := s.o.Store.GetPick(r.Context(), id); gerr == nil {
			s.events.publish("pick.updated", normalizePick(updated))
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	added := store.Request{PickID: pick.ID, App: app, TargetID: res.ID, QualityProfile: res.QualityProfile,
		RootFolder: res.RootFolder, Status: "added", RequestedAt: s.now().UTC()}
	if res.Follow != nil {
		added.Release = releaseJSON(request.Checking(res.QualityProfile))
	}
	if err := s.o.Store.RecordRequest(r.Context(), added); err != nil {
		s.internalError(w, "record request", fmt.Errorf("added to %s but not recorded: %w", res.App, err))
		return
	}
	if err := s.o.Store.SetVerdict(r.Context(), pick.Kind, pick.TMDBID, store.VerdictAccepted, nil); err != nil {
		s.log.Error("set accepted verdict", "pick_id", pick.ID, "err", err)
	}
	s.log.Info("title added", "app", app, "title", res.Title, "quality_profile", res.QualityProfile, "root_folder", res.RootFolder)
	s.respondPick(w, r, id)
	s.followRelease(pick.ID, res)
}

// switchProfile moves a movie added through this pick to another quality
// profile and has Radarr search again. The profile is always the user's
// explicit choice.
func (s *Server) switchProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		QualityProfileID int `json:"quality_profile_id"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.QualityProfileID <= 0 {
		writeError(w, http.StatusBadRequest, "quality_profile_id is required: choose the quality profile to switch to")
		return
	}
	if s.o.Adder == nil {
		writeError(w, http.StatusBadRequest, "adding titles is not configured")
		return
	}
	pick, ok := s.loadPick(w, r, id)
	if !ok {
		return
	}
	if pick.Kind != media.Movies {
		writeError(w, http.StatusBadRequest, "only a movie's quality profile can be switched after its search")
		return
	}
	prev := pick.Request
	if prev == nil || prev.App != "radarr" || prev.Status != "added" || prev.TargetID <= 0 {
		writeError(w, http.StatusConflict, pick.Title+" has not been added to Radarr")
		return
	}
	if s.isFollowing(pick.ID) {
		writeError(w, http.StatusConflict, "Radarr is still searching for "+pick.Title)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	item := request.Item{Kind: pick.Kind, TMDBID: pick.TMDBID, Title: pick.Title, Year: pick.Year}
	res, err := s.o.Adder.SwitchProfile(ctx, item, prev.TargetID, body.QualityProfileID)
	switch {
	case err == nil:
	case errors.Is(err, request.ErrUnknownProfile), errors.Is(err, request.ErrNotConfigured):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, request.ErrNotInLibrary):
		writeError(w, http.StatusConflict, err.Error())
		return
	default:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	switched := *prev
	switched.QualityProfile, switched.Release = res.QualityProfile, nil
	if res.Follow != nil {
		switched.Release = releaseJSON(request.Checking(res.QualityProfile))
	}
	if err := s.o.Store.RecordRequest(r.Context(), switched); err != nil {
		s.internalError(w, "record request", fmt.Errorf("switched in Radarr but not recorded: %w", err))
		return
	}
	s.log.Info("quality profile switched", "title", res.Title, "quality_profile", res.QualityProfile)
	s.respondPick(w, r, id)
	s.followRelease(pick.ID, res)
}

// followRelease follows Radarr's search for an added or switched movie in the
// background, then stores the outcome on the pick's latest request and
// announces the pick.
func (s *Server) followRelease(pickID int64, res request.Result) {
	if res.Follow == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return // reopening the store marks the check searching
	}
	s.following[pickID] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		check := res.Follow(s.ctx)
		s.log.Info("release check", "title", res.Title, "status", check.Status, "quality_profile", check.Profile,
			"switched_from", check.SwitchedFrom, "found", check.Found, "alternatives", len(check.Alternatives), "error", check.Error)
		// The outcome is stored even while the server shuts down.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 10*time.Second)
		defer cancel()
		err := s.o.Store.UpdateRequestRelease(ctx, pickID, check.Profile, releaseJSON(check))
		s.mu.Lock()
		delete(s.following, pickID)
		s.mu.Unlock()
		if err != nil {
			s.log.Error("store release check", "pick_id", pickID, "err", err)
			return
		}
		if pick, err := s.o.Store.GetPick(ctx, pickID); err == nil {
			s.events.publish("pick.updated", normalizePick(pick))
		}
	}()
}

func (s *Server) isFollowing(pickID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.following[pickID]
}

func releaseJSON(c request.ReleaseCheck) json.RawMessage {
	raw, _ := json.Marshal(c) // a ReleaseCheck always encodes
	return raw
}

func (s *Server) appOptions(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if app != "radarr" && app != "sonarr" {
		writeError(w, http.StatusNotFound, "unknown app "+strconv.Quote(app))
		return
	}
	var (
		catalog AppCatalog
		root    string
		ok      bool
	)
	if s.o.App != nil {
		catalog, root, ok = s.o.App(app)
	}
	if !ok || catalog == nil {
		writeError(w, http.StatusBadRequest, appTitle(app)+" is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	profiles, err := catalog.QualityProfiles(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	folders, err := catalog.RootFolders(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	type profileJSON struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	type folderJSON struct {
		ID        int    `json:"id"`
		Path      string `json:"path"`
		FreeSpace int64  `json:"free_space"`
	}
	ps := make([]profileJSON, 0, len(profiles))
	for _, p := range profiles {
		ps = append(ps, profileJSON{ID: p.ID, Name: p.Name})
	}
	fs := make([]folderJSON, 0, len(folders))
	for _, f := range folders {
		fs = append(fs, folderJSON{ID: f.ID, Path: f.Path, FreeSpace: f.FreeSpace})
	}
	writeJSON(w, http.StatusOK, map[string]any{"quality_profiles": ps, "root_folders": fs, "default_root_folder": root})
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	k := r.URL.Query().Get("kind")
	if k == "" {
		writeError(w, http.StatusBadRequest, "kind is required (movies or series)")
		return
	}
	kind, err := media.ParseKind(k)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := s.cfg()
	arrCfg := cfg.Radarr
	if kind == media.Series {
		arrCfg = cfg.Sonarr
	}
	if s.o.Library == nil || !arrCfg.Configured() {
		writeError(w, http.StatusBadRequest, kind.App()+" is not configured")
		return
	}
	titles, err := s.o.Library(r.Context(), kind)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if titles == nil {
		titles = []media.Title{}
	}
	var prof any
	if s.o.Store != nil {
		p, err := s.o.Store.LatestProfile(r.Context(), kind)
		if err != nil {
			s.log.Error("latest profile", "kind", kind, "err", err)
		} else if p != nil {
			prof = p
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "titles": titles, "profile": prof})
}

func (s *Server) loadPick(w http.ResponseWriter, r *http.Request, id int64) (store.Pick, bool) {
	pick, err := s.o.Store.GetPick(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "pick not found")
		return store.Pick{}, false
	}
	if err != nil {
		s.internalError(w, "get pick", err)
		return store.Pick{}, false
	}
	return pick, true
}

// respondPick reloads the pick, announces it and writes it.
func (s *Server) respondPick(w http.ResponseWriter, r *http.Request, id int64) {
	pick, ok := s.loadPick(w, r, id)
	if !ok {
		return
	}
	pick = normalizePick(pick)
	s.events.publish("pick.updated", pick)
	writeJSON(w, http.StatusOK, pick)
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, what+": "+err.Error())
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func appOf(k media.Kind) string {
	if k == media.Series {
		return "sonarr"
	}
	return "radarr"
}

func appTitle(app string) string {
	if app == "sonarr" {
		return "Sonarr"
	}
	return "Radarr"
}

// choiceError is a quality profile or root folder the user must correct.
type choiceError struct{ msg string }

func (e *choiceError) Error() string { return e.msg }

// choice answers request.Chooser from the request body: the quality profile is
// always the explicit id, the root folder is the given path or the configured default.
type choice struct {
	profileID   int
	rootFolder  string
	defaultRoot string
	fallback    request.Fallback

	chosenProfile, chosenRoot string
}

func (c *choice) ChooseFallback(context.Context, request.Item, arr.QualityProfile) (request.Fallback, error) {
	return c.fallback, nil
}

func (c *choice) ChooseQualityProfile(_ context.Context, _ request.Item, app string, profiles []arr.QualityProfile) (arr.QualityProfile, error) {
	for _, p := range profiles {
		if p.ID == c.profileID {
			c.chosenProfile = p.Name
			return p, nil
		}
	}
	have := make([]string, len(profiles))
	for i, p := range profiles {
		have[i] = fmt.Sprintf("%s (%d)", p.Name, p.ID)
	}
	return arr.QualityProfile{}, &choiceError{fmt.Sprintf("%s has no quality profile %d (have %s)", app, c.profileID, strings.Join(have, ", "))}
}

func (c *choice) ChooseRootFolder(_ context.Context, _ request.Item, app string, folders []arr.RootFolder) (arr.RootFolder, error) {
	paths := make([]string, len(folders))
	for i, f := range folders {
		paths[i] = f.Path
	}
	want := c.rootFolder
	if want == "" {
		want = c.defaultRoot
	}
	if want == "" {
		return arr.RootFolder{}, &choiceError{fmt.Sprintf("%s has several root folders, choose one: %s", app, strings.Join(paths, ", "))}
	}
	for _, f := range folders {
		if f.Path == want {
			c.chosenRoot = f.Path
			return f, nil
		}
	}
	return arr.RootFolder{}, &choiceError{fmt.Sprintf("%s has no root folder %q (have %s)", app, want, strings.Join(paths, ", "))}
}

func normalizeRun(r store.Run) store.Run {
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	if r.Rejected == nil {
		r.Rejected = []pipeline.Rejected{}
	}
	if r.Owned == nil {
		r.Owned = []pipeline.OwnedMatch{}
	}
	return r
}

func normalizePick(p store.Pick) store.Pick {
	if p.RelatedTo == nil {
		p.RelatedTo = []string{}
	}
	return p
}

func normalizePicks(ps []store.Pick) []store.Pick {
	out := make([]store.Pick, 0, len(ps))
	for _, p := range ps {
		out = append(out, normalizePick(p))
	}
	return out
}
