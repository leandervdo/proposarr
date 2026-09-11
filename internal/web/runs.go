package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/leandervdo/proposarr/internal/agent"
	"github.com/leandervdo/proposarr/internal/media"
	"github.com/leandervdo/proposarr/internal/pipeline"
	"github.com/leandervdo/proposarr/internal/store"
)

// activeRun is both the /api/status running entry and the run.progress payload.
type activeRun struct {
	RunID   int64      `json:"run_id"`
	Kind    media.Kind `json:"kind"`
	Message string     `json:"message"`
}

// startRun reserves the kind's slot, records the run and starts it in the
// background. It returns the HTTP status to use on error.
func (s *Server) startRun(kind media.Kind, vibe string, useTaste bool) (store.Run, int, error) {
	if s.o.NewRunner == nil || s.o.RunRequest == nil || s.o.Store == nil {
		return store.Run{}, http.StatusServiceUnavailable, errors.New("runs are not available")
	}
	s.mu.Lock()
	switch {
	case s.closed:
		s.mu.Unlock()
		return store.Run{}, http.StatusServiceUnavailable, errors.New("server is shutting down")
	case s.running[kind] != nil:
		s.mu.Unlock()
		return store.Run{}, http.StatusConflict, fmt.Errorf("a %s run is already running", kind)
	}
	active := &activeRun{Kind: kind, Message: "Starting…"}
	s.running[kind] = active
	s.wg.Add(1)
	s.mu.Unlock()

	abort := func(status int, err error) (store.Run, int, error) {
		s.release(kind)
		s.wg.Done()
		return store.Run{}, status, err
	}
	req, err := s.o.RunRequest(kind, vibe, useTaste)
	if err != nil {
		return abort(http.StatusBadRequest, err)
	}
	req.Kind, req.Vibe, req.OpenSearch = kind, vibe, !useTaste

	rec := store.Run{Kind: kind, Vibe: vibe, UseTaste: useTaste, Model: req.Model, Effort: req.Effort, Status: store.RunRunning, StartedAt: s.now().UTC()}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	id, err := s.o.Store.CreateRun(ctx, rec)
	cancel()
	if err != nil {
		return abort(http.StatusInternalServerError, fmt.Errorf("create run: %w", err))
	}
	rec.ID = id
	rec = normalizeRun(rec)

	s.mu.Lock()
	active.RunID = id
	s.mu.Unlock()
	s.events.publish("run.started", rec)
	s.log.Info("run started", "run_id", id, "kind", kind, "model", req.Model, "effort", req.Effort)

	runner := s.o.NewRunner(func(msg string) {
		s.mu.Lock()
		active.Message = msg
		s.mu.Unlock()
		s.events.publish("run.progress", activeRun{RunID: id, Kind: kind, Message: msg})
	})
	go s.execute(runner, req, rec)
	return rec, http.StatusAccepted, nil
}

func (s *Server) execute(runner Runner, req pipeline.Request, rec store.Run) {
	defer s.wg.Done()
	res, runErr := s.safeRun(runner, req)

	// Record with a fresh context so a shutdown still stores the outcome.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.o.Store.FinishRun(ctx, rec.ID, res, runErr); err != nil {
		s.log.Error("record run", "run_id", rec.ID, "err", err)
	}
	final, _, err := s.o.Store.GetRun(ctx, rec.ID)
	if err != nil {
		s.log.Error("load finished run", "run_id", rec.ID, "err", err)
		final = fallbackFinished(rec, runErr, s.now().UTC())
	}
	final.Profile = nil

	s.release(rec.Kind)
	s.events.publish("run.finished", normalizeRun(final))
	if runErr != nil {
		s.log.Warn("run finished", "run_id", rec.ID, "kind", rec.Kind, "status", final.Status, "err", runErr)
		return
	}
	s.log.Info("run finished", "run_id", rec.ID, "kind", rec.Kind, "status", final.Status, "picks", final.PickCount, "cost_usd", final.CostUSD)
}

func (s *Server) safeRun(runner Runner, req pipeline.Request) (res *pipeline.Run, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("run panicked: %v", p)
		}
	}()
	return runner.Run(s.ctx, req)
}

func fallbackFinished(rec store.Run, runErr error, now time.Time) store.Run {
	rec.FinishedAt = &now
	var sl *agent.SessionLimitError
	switch {
	case errors.As(runErr, &sl):
		rec.Status, rec.Error = store.RunRateLimited, runErr.Error()
	case runErr != nil:
		rec.Status, rec.Error = store.RunFailed, runErr.Error()
	default:
		rec.Status = store.RunSucceeded
	}
	return rec
}

func (s *Server) release(kind media.Kind) {
	s.mu.Lock()
	delete(s.running, kind)
	s.mu.Unlock()
}

func (s *Server) runningList() []activeRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]activeRun, 0, len(s.running))
	for _, a := range s.running {
		if a.RunID > 0 {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}
