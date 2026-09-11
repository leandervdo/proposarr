package arr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/leandervdo/proposarr/internal/httpjson"
)

// FetchAPIKey reads the API key from the web UI's /initialize.json. This only
// works when Sonarr/Radarr do not require a login for the caller's address
// (Authentication Required: Disabled for Local Addresses). It is not an
// official API, so a configured key always takes precedence.
func FetchAPIKey(ctx context.Context, baseURL string, hc *http.Client) (string, error) {
	c := &httpjson.Client{BaseURL: baseURL, HTTP: hc}
	var v struct {
		APIKey string `json:"apiKey"`
	}
	hint := "set api_key, or allow local access without login in Settings → General → Authentication Required"
	if err := c.Get(ctx, "/initialize.json", nil, &v); err != nil {
		if httpjson.IsStatus(err, http.StatusUnauthorized) || httpjson.IsStatus(err, http.StatusForbidden) {
			err = errors.New("login required")
		}
		return "", fmt.Errorf("read API key from %s/initialize.json: %w (%s)", strings.TrimRight(baseURL, "/"), err, hint)
	}
	if v.APIKey == "" {
		return "", fmt.Errorf("%s/initialize.json has no apiKey (%s)", strings.TrimRight(baseURL, "/"), hint)
	}
	return v.APIKey, nil
}
