// Package tmdb is the TMDB v3 API client.
package tmdb

import "github.com/leandervdo/proposarr/internal/media"

// Item is a movie or TV result normalised across TMDB's movie and tv shapes.
type Item struct {
	ID         int        `json:"tmdb_id"`
	Kind       media.Kind `json:"kind"`
	Title      string     `json:"title"`
	Year       int        `json:"year,omitempty"`
	Overview   string     `json:"overview,omitempty"`
	Genres     []string   `json:"genres,omitempty"`
	Rating     float64    `json:"rating,omitempty"`
	Votes      int        `json:"votes,omitempty"`
	Popularity float64    `json:"popularity,omitempty"`
	PosterPath string     `json:"poster_path,omitempty"`
}

// Details is Item plus the fields only the details endpoint returns.
type Details struct {
	Item
	Status    string   `json:"status,omitempty"`
	Runtime   int      `json:"runtime,omitempty"`   // minutes; episode runtime for tv
	Providers []string `json:"providers,omitempty"` // flatrate streaming providers in the requested region
	TVDBID    int      `json:"tvdb_id,omitempty"`
	IMDBID    string   `json:"imdb_id,omitempty"`
}

const imageBase = "https://image.tmdb.org/t/p/w500"

// PosterURL turns a poster_path into an absolute URL.
func PosterURL(path string) string {
	if path == "" {
		return ""
	}
	return imageBase + path
}
