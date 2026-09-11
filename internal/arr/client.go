package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/leandervdo/proposarr/internal/httpjson"
)

// APIError is a *arr error response with its validation messages extracted.
type APIError struct {
	App      string
	Messages []string
	Err      *httpjson.StatusError
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s: %s %s: HTTP %d", e.App, e.Err.Method, e.Err.Path, e.Err.Code)
	if len(e.Messages) > 0 {
		return msg + ": " + strings.Join(e.Messages, "; ")
	}
	if e.Err.Body != "" {
		return msg + ": " + e.Err.Body
	}
	return msg
}

func (e *APIError) Unwrap() error { return e.Err }

func newHTTP(baseURL, apiKey string, hc *http.Client) *httpjson.Client {
	return &httpjson.Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    hc,
		Header:  http.Header{"X-Api-Key": []string{apiKey}},
	}
}

type client struct {
	app string
	hc  *httpjson.Client
}

func (c client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.wrap(c.hc.Get(ctx, path, q, out))
}

func (c client) post(ctx context.Context, path string, body, out any) error {
	return c.wrap(c.hc.Post(ctx, path, body, out))
}

func (c client) status(ctx context.Context) (SystemStatus, error) {
	var s SystemStatus
	err := c.get(ctx, "/api/v3/system/status", nil, &s)
	return s, err
}

func (c client) qualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	var ps []QualityProfile
	err := c.get(ctx, "/api/v3/qualityprofile", nil, &ps)
	return ps, err
}

func (c client) rootFolders(ctx context.Context) ([]RootFolder, error) {
	var fs []RootFolder
	err := c.get(ctx, "/api/v3/rootfolder", nil, &fs)
	return fs, err
}

func (c client) wrap(err error) error {
	if err == nil {
		return nil
	}
	var se *httpjson.StatusError
	if !errors.As(err, &se) {
		return fmt.Errorf("%s: %w", c.app, err)
	}
	return &APIError{App: c.app, Messages: validationMessages(se.Body), Err: se}
}

var errorMessageRE = regexp.MustCompile(`"errorMessage"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// validationMessages pulls errorMessage values out of a (possibly truncated) body.
func validationMessages(body string) []string {
	var items []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal([]byte(body), &items) == nil {
		var out []string
		for _, it := range items {
			if it.ErrorMessage != "" {
				out = append(out, it.ErrorMessage)
			}
		}
		return out
	}
	var out []string
	for _, m := range errorMessageRE.FindAllStringSubmatch(body, -1) {
		var s string
		if json.Unmarshal([]byte(`"`+m[1]+`"`), &s) != nil {
			s = m[1]
		}
		out = append(out, s)
	}
	return out
}

func validateAdd(app string, l *Lookup, o AddOptions) error {
	switch {
	case l == nil || l.Raw == nil:
		return fmt.Errorf("%s: add: no lookup result", app)
	case o.QualityProfileID <= 0:
		return fmt.Errorf("%s: add %q: a quality profile must be chosen", app, l.Title)
	case o.RootFolderPath == "":
		return fmt.Errorf("%s: add %q: a root folder must be chosen", app, l.Title)
	}
	return nil
}

func copyRaw(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw)+6)
	for k, v := range raw {
		out[k] = v
	}
	return out
}

type added struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}
