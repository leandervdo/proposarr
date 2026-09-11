// Package httpjson is the small JSON-over-HTTP helper every adapter uses.
// Errors never include the full URL, so query-string credentials stay out of logs.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client issues JSON requests against one base URL.
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	Header     http.Header
	Query      url.Values // added to every request, e.g. an api_key
	MaxRetries int        // retries on HTTP 429
}

// StatusError is a non-2xx response.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Body   string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Code)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// IsStatus reports whether err is a StatusError with the given code.
func IsStatus(err error, code int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == code
}

var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, q, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, nil, body, out)
}

// Do sends one request and decodes a JSON response into out (if non-nil).
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + path)
	if err != nil {
		return fmt.Errorf("%s %s: bad url: %w", method, path, err)
	}
	vals := u.Query()
	for k, vs := range c.Query {
		for _, v := range vs {
			vals.Add(k, v)
		}
	}
	for k, vs := range q {
		for _, v := range vs {
			vals.Add(k, v)
		}
	}
	u.RawQuery = vals.Encode()

	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("%s %s: encode: %w", method, path, err)
		}
	}
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTP
	}

	for attempt := 0; ; attempt++ {
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
		for k, vs := range c.Header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := hc.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return fmt.Errorf("%s %s: %w", method, u.Path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return &StatusError{Method: method, Path: u.Path, Code: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
		}
		if out == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, u.Path, err)
		}
		return nil
	}
}

func retryAfter(h string, attempt int) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(attempt+1) * time.Second
}
