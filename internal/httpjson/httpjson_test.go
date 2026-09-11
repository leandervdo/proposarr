package httpjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryHeaderMerge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/items" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("api_key") != "k" || q.Get("page") != "2" {
			t.Errorf("query = %v", q)
		}
		if got := r.Header.Get("X-Api-Key"); got != "hdr" {
			t.Errorf("X-Api-Key = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type on GET = %q", got)
		}
		fmt.Fprint(w, `{"name":"ok"}`)
	}))
	defer srv.Close()

	c := &Client{
		BaseURL: srv.URL + "/",
		Header:  http.Header{"X-Api-Key": {"hdr"}},
		Query:   url.Values{"api_key": {"k"}},
	}
	var out struct{ Name string }
	if err := c.Get(context.Background(), "/api/items", url.Values{"page": {"2"}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "ok" {
		t.Errorf("out = %+v", out)
	}
}

func TestPostRoundTrip(t *testing.T) {
	type payload struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var in payload
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("decode: %v", err)
		}
		in.ID++
		_ = json.NewEncoder(w).Encode(in)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	var out payload
	if err := c.Post(context.Background(), "/add", payload{ID: 1, Title: "Arrival"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != 2 || out.Title != "Arrival" {
		t.Errorf("out = %+v", out)
	}
}

func TestStatusError(t *testing.T) {
	long := strings.Repeat("x", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, long)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Query: url.Values{"api_key": {"secret"}}}
	err := c.Get(context.Background(), "/api/v3/movie", url.Values{"x": {"1"}}, nil)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if se.Code != http.StatusBadRequest || se.Method != http.MethodGet {
		t.Errorf("StatusError = %+v", se)
	}
	if se.Path != "/api/v3/movie" {
		t.Errorf("Path = %q, want no query string", se.Path)
	}
	if len(se.Body) != 512 {
		t.Errorf("body snippet length = %d, want 512", len(se.Body))
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks query credential: %v", err)
	}
	if !IsStatus(err, http.StatusBadRequest) || IsStatus(err, http.StatusNotFound) {
		t.Error("IsStatus mismatch")
	}
	if IsStatus(errors.New("plain"), http.StatusBadRequest) {
		t.Error("IsStatus true for non-StatusError")
	}
	if !IsStatus(fmt.Errorf("wrapped: %w", err), http.StatusBadRequest) {
		t.Error("IsStatus false for wrapped StatusError")
	}
}

func TestRetry429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, MaxRetries: 2}
	err := c.Get(context.Background(), "/", nil, nil)
	if !IsStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("err = %v, want 429 StatusError", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (1 + 2 retries)", got)
	}
}

func TestRetry429ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, MaxRetries: 3}
	var out struct{ OK bool }
	if err := c.Get(context.Background(), "/", nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || calls.Load() != 2 {
		t.Errorf("out = %+v, calls = %d", out, calls.Load())
	}
}

func TestNoRetryWhenMaxRetriesZero(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.Get(context.Background(), "/", nil, nil); !IsStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestContextCancelDuringRetryWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	c := &Client{BaseURL: srv.URL, MaxRetries: 3}
	start := time.Now()
	err := c.Get(ctx, "/", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("did not return promptly on cancel")
	}
}

func TestTransportErrorRedactsQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	c := &Client{BaseURL: base, Query: url.Values{"api_key": {"secret"}}}
	err := c.Get(context.Background(), "/3/configuration", nil, nil)
	if err == nil {
		t.Fatal("want transport error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("transport error leaks api_key: %v", err)
	}
	if !strings.Contains(err.Error(), "GET") {
		t.Errorf("error lacks method: %v", err)
	}
}

func TestNilOutDrainsBody(t *testing.T) {
	var drained atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("y", 64*1024))
		drained.Store(true)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.Get(context.Background(), "/", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !drained.Load() {
		t.Error("handler did not finish writing; body not drained")
	}
}

func TestDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Query: url.Values{"api_key": {"secret"}}}
	var out map[string]any
	err := c.Get(context.Background(), "/x", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want decode error", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("decode error leaks api_key: %v", err)
	}
}

func TestBadURL(t *testing.T) {
	c := &Client{BaseURL: "http://[::1"}
	if err := c.Get(context.Background(), "/", nil, nil); err == nil {
		t.Fatal("want error for malformed base URL")
	}
}
