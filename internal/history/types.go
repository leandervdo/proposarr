// Package history reads watch history from Plex and Jellyfin.
package history

import (
	"context"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

// Signal is how strongly a title was watched in the history window.
type Signal string

const (
	Rewatched Signal = "rewatched" // a movie played twice, or an episode replayed
	Watched   Signal = "watched"   // a movie played, or a substantial part of a series
	Partial   Signal = "partial"   // a movie abandoned, or a few episodes of a series
)

// Entry is one title aggregated over the history window, merged across users.
type Entry struct {
	Kind        media.Kind `json:"kind"`
	TMDBID      int        `json:"tmdb_id,omitempty"`
	TVDBID      int        `json:"tvdb_id,omitempty"`
	Title       string     `json:"title"`
	Year        int        `json:"year,omitempty"`
	Plays       int        `json:"plays"`
	Episodes    int        `json:"episodes,omitempty"` // distinct episodes, series only
	LastWatched time.Time  `json:"last_watched"`
	Signal      Signal     `json:"signal"`
}

// Source is a media server that exposes watch history.
type Source interface {
	Name() string
	Ping(ctx context.Context) error
	History(ctx context.Context, kind media.Kind, since time.Time) ([]Entry, error)
}
