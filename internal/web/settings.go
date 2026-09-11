package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/leandervdo/proposarr/internal/settings"
)

// SettingsService reads, saves and tests the UI-editable settings. Update and
// Test return *settings.ValidationError for values the user must correct.
type SettingsService interface {
	View(ctx context.Context) (settings.View, error)
	Update(ctx context.Context, values map[string]json.RawMessage) (settings.View, error)
	Test(ctx context.Context, service string, values map[string]json.RawMessage) (TestResult, error)
}

type TestResult struct {
	Status           string `json:"status"` // ok | fail
	Detail           string `json:"detail"`
	DiscoveredAPIKey bool   `json:"discovered_api_key,omitempty"`
}

var testableServices = map[string]bool{"radarr": true, "sonarr": true, "plex": true, "jellyfin": true, "tmdb": true, "claude": true}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.o.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings are not available")
		return
	}
	v, err := s.o.Settings.View(r.Context())
	if err != nil {
		s.internalError(w, "load settings", err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	if s.o.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings are not available")
		return
	}
	var body struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Values == nil {
		writeError(w, http.StatusBadRequest, "values is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	v, err := s.o.Settings.Update(ctx, body.Values)
	if writeValidation(w, err) {
		return
	}
	if err != nil {
		s.internalError(w, "save settings", err)
		return
	}
	s.log.Info("settings saved", "keys", len(body.Values))
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) testSettings(w http.ResponseWriter, r *http.Request) {
	if s.o.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings are not available")
		return
	}
	var body struct {
		Service string                     `json:"service"`
		Values  map[string]json.RawMessage `json:"values"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !testableServices[body.Service] {
		writeError(w, http.StatusBadRequest, "service must be radarr, sonarr, plex, jellyfin, tmdb or claude")
		return
	}
	timeout := 25 * time.Second
	if body.Service == "claude" {
		timeout = 55 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	res, err := s.o.Settings.Test(ctx, body.Service, body.Values)
	if writeValidation(w, err) {
		return
	}
	if err != nil {
		s.internalError(w, "test "+body.Service, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeValidation(w http.ResponseWriter, err error) bool {
	var ve *settings.ValidationError
	if !errors.As(err, &ve) {
		return false
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": ve.Error(), "field_errors": ve.Fields})
	return true
}
