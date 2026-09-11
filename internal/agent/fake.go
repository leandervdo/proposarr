package agent

import (
	"context"
	"sync"
)

// Fake returns canned results and records every call.
type Fake struct {
	Result Result
	Err    error
	Fn     func(Options) (Result, error)

	mu    sync.Mutex
	calls []Options
}

func (f *Fake) Run(_ context.Context, o Options) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, o)
	f.mu.Unlock()
	if f.Fn != nil {
		return f.Fn(o)
	}
	return f.Result, f.Err
}

func (f *Fake) Calls() []Options {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Options(nil), f.calls...)
}
