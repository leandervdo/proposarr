package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/request"
	"github.com/leandervdo/proposarr/internal/store"
)

func waitingCheck() *request.ReleaseCheck {
	seeders := 249
	return &request.ReleaseCheck{
		Status: request.CheckWaiting, Profile: "HD-1080p", SwitchedFrom: "Ultra-HD", Found: 14,
		Qualities: []request.QualityCount{{Quality: "Remux-1080p", Count: 6}},
		Alternatives: []request.ProfileOption{{ID: 6, Name: "Remux-1080p", Count: 5, Best: request.ReleaseInfo{
			Title: "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", Quality: "Remux-1080p", Size: 32_400_000_000, Indexer: "TorrentLeech", Protocol: "torrent", Seeders: &seeders,
		}}},
	}
}

func releaseOf(t *testing.T, r *store.Request) request.ReleaseCheck {
	t.Helper()
	var c request.ReleaseCheck
	if r == nil || len(r.Release) == 0 {
		t.Fatalf("request %+v has no release", r)
	}
	if err := json.Unmarshal(r.Release, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// waitRelease waits until the pick's latest request has a release check with status.
func waitRelease(t *testing.T, env *testEnv, pickID int64, status request.CheckStatus) store.Request {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		p, err := env.store.GetPick(context.Background(), pickID)
		if err == nil && p.Request != nil && len(p.Request.Release) > 0 && releaseOf(t, p.Request).Status == status {
			return *p.Request
		}
		if time.Now().After(deadline) {
			t.Fatalf("release of pick %d never became %s: %+v", pickID, status, p.Request)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRequestFollowsRelease(t *testing.T) {
	env := newEnv(t, nil)
	env.adder.release = waitingCheck()
	env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 9693, Kind: media.Movies, Title: "Children of Men", Year: 2006}})

	wantStatus(t, env.do(t, "POST", "/api/picks/1/request", `{"quality_profile_id":5,"if_nothing_fits":"grab"}`), http.StatusBadRequest)
	if env.adder.calls != 0 {
		t.Fatalf("adder called with an unknown if_nothing_fits")
	}

	rec := env.do(t, "POST", "/api/picks/1/request", `{"quality_profile_id":5,"if_nothing_fits":"switch"}`)
	wantStatus(t, rec, http.StatusOK)
	p := decode[store.Pick](t, rec)
	if c := releaseOf(t, p.Request); c.Status != request.CheckChecking || c.Profile != "Ultra-HD" || p.Request.QualityProfile != "Ultra-HD" {
		t.Fatalf("response request = %+v, release %+v", p.Request, c)
	}
	if env.adder.fallback != request.FallbackSwitch {
		t.Errorf("fallback = %q", env.adder.fallback)
	}

	// The outcome lands on the request; a fallback switch changes its profile.
	done := waitRelease(t, env, 1, request.CheckWaiting)
	if c := releaseOf(t, &done); done.QualityProfile != "HD-1080p" || c.SwitchedFrom != "Ultra-HD" || len(c.Alternatives) != 1 || *c.Alternatives[0].Best.Seeders != 249 {
		t.Errorf("stored request = %+v, release %+v", done, c)
	}

	env.adder.release = nil
	env.store.addPick(store.Pick{ID: 2, RunID: 1, Pick: pipeline.Pick{TMDBID: 603, Kind: media.Movies, Title: "The Matrix"}})
	rec = env.do(t, "POST", "/api/picks/2/request", `{"quality_profile_id":5}`)
	wantStatus(t, rec, http.StatusOK)
	if p := decode[store.Pick](t, rec); p.Request.Release != nil || env.adder.fallback != request.FallbackWait {
		t.Errorf("an add that is not followed: release %s, fallback %q", p.Request.Release, env.adder.fallback)
	}
}

func TestSwitchProfile(t *testing.T) {
	env := newEnv(t, nil)
	env.store.addPick(store.Pick{ID: 1, RunID: 1, Pick: pipeline.Pick{TMDBID: 9693, Kind: media.Movies, Title: "Children of Men", Year: 2006}})
	env.store.addPick(store.Pick{ID: 2, RunID: 1, Pick: pipeline.Pick{TMDBID: 95396, Kind: media.Series, Title: "Severance"}})

	wantStatus(t, env.do(t, "POST", "/api/picks/1/request/profile", `{"quality_profile_id":4}`), http.StatusConflict)
	wantStatus(t, env.do(t, "POST", "/api/picks/2/request/profile", `{"quality_profile_id":4}`), http.StatusBadRequest)
	wantStatus(t, env.do(t, "POST", "/api/picks/99/request/profile", `{"quality_profile_id":4}`), http.StatusNotFound)
	if len(env.adder.switched) != 0 {
		t.Fatalf("switched without an added movie: %v", env.adder.switched)
	}

	waiting := waitingCheck()
	waiting.Profile, waiting.SwitchedFrom = "Ultra-HD", ""
	env.adder.release = waiting
	wantStatus(t, env.do(t, "POST", "/api/picks/1/request", `{"quality_profile_id":5}`), http.StatusOK)
	added := waitRelease(t, env, 1, request.CheckWaiting)

	env.adder.release = &request.ReleaseCheck{Status: request.CheckGrabbed, Profile: "HD-1080p",
		Release: &request.ReleaseInfo{Title: "Children of Men 2006 BluRay 1080p REMUX-FraMeSToR", Quality: "Remux-1080p"}}
	env.adder.block = make(chan struct{})
	rec := env.do(t, "POST", "/api/picks/1/request/profile", `{"quality_profile_id":4}`)
	wantStatus(t, rec, http.StatusOK)
	p := decode[store.Pick](t, rec)
	if c := releaseOf(t, p.Request); c.Status != request.CheckChecking || p.Request.QualityProfile != "HD-1080p" {
		t.Fatalf("response request = %+v, release %+v", p.Request, c)
	}
	rec = env.do(t, "POST", "/api/picks/1/request/profile", `{"quality_profile_id":5}`)
	wantStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "still searching") {
		t.Errorf("body = %s", rec.Body.String())
	}
	close(env.adder.block)
	env.adder.block = nil

	last := waitRelease(t, env, 1, request.CheckGrabbed)
	if len(env.adder.switched) != 1 || env.adder.switched[0] != [2]int{42, 4} {
		t.Errorf("switched = %v", env.adder.switched)
	}
	if last.QualityProfile != "HD-1080p" || last.TargetID != 42 || last.RootFolder != added.RootFolder || last.Status != "added" || !last.RequestedAt.Equal(added.RequestedAt) {
		t.Errorf("recorded request = %+v, added %+v", last, added)
	}

	cases := []struct {
		name string
		body string
		err  error
		code int
	}{
		{name: "no profile", body: `{}`, code: http.StatusBadRequest},
		{name: "unknown profile", body: `{"quality_profile_id":77}`, code: http.StatusBadRequest},
		{name: "gone from Radarr", body: `{"quality_profile_id":4}`, err: fmt.Errorf("Children of Men (2006): %w", request.ErrNotInLibrary), code: http.StatusConflict},
		{name: "Radarr down", body: `{"quality_profile_id":4}`, err: errors.New("radarr: PUT /api/v3/movie/editor: HTTP 500"), code: http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env.store.mu.Lock()
			before := len(env.store.requests)
			env.store.mu.Unlock()
			env.adder.switchErr = tc.err
			wantStatus(t, env.do(t, "POST", "/api/picks/1/request/profile", tc.body), tc.code)
			env.store.mu.Lock()
			defer env.store.mu.Unlock()
			if len(env.store.requests) != before {
				t.Errorf("a failed switch recorded a request: %+v", env.store.requests[before:])
			}
		})
	}
}
