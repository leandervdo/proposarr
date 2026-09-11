package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/candidates"
	"github.com/leandervdo/proposarr/internal/history"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/profile"
)

const (
	maxGenreLookups   = 60
	genreConcurrency  = 6
	detailConcurrency = 4
)

// Run executes one pass. On an agent failure it returns the partial Run
// (profile, candidates, warnings) together with the error.
func (p *Pipeline) Run(ctx context.Context, req Request) (*Run, error) {
	req = withDefaults(req)
	now := time.Now
	if p.d.Now != nil {
		now = p.d.Now
	}
	switch {
	case isNil(p.d.Library):
		return nil, errors.New("pipeline: no library configured")
	case isNil(p.d.Meta):
		return nil, errors.New("pipeline: TMDB is not configured")
	case isNil(p.d.Agent):
		return nil, errors.New("pipeline: no agent configured")
	}

	run := &Run{Kind: req.Kind, Vibe: req.Vibe, Model: req.Model, Effort: req.Effort, StartedAt: now(), Picks: []Pick{}}
	fail := func(err error) (*Run, error) {
		run.FinishedAt = now()
		return run, err
	}

	p.progress("Loading %s library…", req.Kind.App())
	lib, err := p.d.Library.Titles(ctx, req.Kind)
	if err != nil {
		return nil, err
	}
	run.LibraryCount = len(lib)

	hist := p.history(ctx, req, run.StartedAt, run)
	run.HistoryCount = len(hist)

	excluded := map[int]bool{}
	libIDs := map[int]bool{}
	for _, t := range lib {
		if t.TMDBID > 0 {
			libIDs[t.TMDBID] = true
			excluded[t.TMDBID] = true
		}
	}
	for _, h := range hist {
		if h.TMDBID > 0 {
			excluded[h.TMDBID] = true
		}
	}

	prof := profile.Build(req.Kind, lib, hist, p.extraGenres(ctx, req, hist, libIDs, run), req.TopTitles, profile.DefaultWeights)
	run.Profile = prof

	p.progress("Gathering candidates from TMDB…")
	var disc candidates.Discover
	if req.Kind == media.Movies && !isNil(p.d.Discover) {
		disc = p.d.Discover
	}
	cands, warns := candidates.Build(ctx, p.d.Meta, disc, prof.Top, func(id int) bool { return excluded[id] },
		candidates.Options{Kind: req.Kind, Seeds: req.Seeds, Cap: req.Candidates})
	run.Warnings = append(run.Warnings, warns...)
	run.CandidateCount = len(cands)
	if len(cands) == 0 && req.FreePicks == 0 {
		return fail(errors.New("no candidates found (library/history empty or TMDB unreachable)"))
	}

	schema, err := pickSchema(req.Picks)
	if err != nil {
		return fail(err)
	}
	dir, err := os.MkdirTemp("", "proposarr-run-*")
	if err != nil {
		return fail(fmt.Errorf("create run dir: %w", err))
	}
	defer os.RemoveAll(dir)

	p.progress("Asking Claude (%s, %s)…", orDefault(req.Model), orDefault(req.Effort))
	res, err := p.d.Agent.Run(ctx, agent.Options{
		Model:        req.Model,
		Effort:       req.Effort,
		SystemPrompt: systemPrompt(req.Kind),
		Prompt:       userPrompt(req, prof, cands),
		JSONSchema:   schema,
		MaxBudgetUSD: req.MaxBudgetUSD,
		Dir:          dir,
		Env:          req.AgentEnv,
		Timeout:      req.Timeout,
	})
	run.CostUSD = res.CostUSD
	run.Usage = res.Usage
	run.NumTurns = res.NumTurns
	run.SessionID = res.SessionID
	if err != nil {
		return fail(err)
	}

	raw, err := decodePicks(res)
	if err != nil {
		return fail(err)
	}

	p.progress("Verifying %d picks…", len(raw))
	p.verify(ctx, req, run, raw, cands, excluded, untrackedTitles(lib, hist), prof)
	run.FinishedAt = now()
	return run, nil
}

func withDefaults(r Request) Request {
	if r.Picks <= 0 {
		r.Picks = 10
	}
	if r.Candidates <= 0 {
		r.Candidates = 150
	}
	if r.FreePicks < 0 {
		r.FreePicks = 0
	}
	if r.Seeds <= 0 {
		r.Seeds = 15
	}
	if r.TopTitles <= 0 {
		r.TopTitles = 40
	}
	if r.HistoryDays <= 0 {
		r.HistoryDays = 180
	}
	if r.Region == "" {
		r.Region = "US"
	}
	return r
}

func (p *Pipeline) history(ctx context.Context, req Request, now time.Time, run *Run) []history.Entry {
	since := now.AddDate(0, 0, -req.HistoryDays)
	var lists [][]history.Entry
	for _, s := range p.d.History {
		if isNil(s) {
			continue
		}
		p.progress("Reading %s history…", s.Name())
		entries, err := s.History(ctx, req.Kind, since)
		if err != nil {
			run.Warnings = append(run.Warnings, fmt.Sprintf("%s history failed: %v", s.Name(), err))
			continue
		}
		lists = append(lists, entries)
	}
	if len(lists) == 0 {
		return nil
	}
	return history.Merge(lists...)
}

// extraGenres looks up genres for watched titles that are not in the library,
// so they count towards the profile's genre mix.
func (p *Pipeline) extraGenres(ctx context.Context, req Request, hist []history.Entry, libIDs map[int]bool, run *Run) map[int][]string {
	var ids []int
	seen := map[int]bool{}
	for _, h := range hist {
		if h.TMDBID > 0 && !libIDs[h.TMDBID] && !seen[h.TMDBID] {
			seen[h.TMDBID] = true
			ids = append(ids, h.TMDBID)
			if len(ids) == maxGenreLookups {
				break
			}
		}
	}
	out := map[int][]string{}
	if len(ids) == 0 {
		return out
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		sem    = make(chan struct{}, genreConcurrency)
		failed int
		first  error
	)
	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := p.d.Meta.Details(ctx, req.Kind, id, req.Region)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if first == nil {
					first = err
				}
				return
			}
			if len(d.Genres) > 0 {
				out[id] = d.Genres
			}
		}(id)
	}
	wg.Wait()
	if failed > 0 {
		run.Warnings = append(run.Warnings, fmt.Sprintf("tmdb details failed for %d of %d watched titles: %v", failed, len(ids), first))
	}
	return out
}

func (p *Pipeline) progress(format string, args ...any) {
	if p.d.Progress != nil {
		p.d.Progress(fmt.Sprintf(format, args...))
	}
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// isNil also catches typed nil pointers stored in an interface.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return rv.IsNil()
	}
	return false
}
