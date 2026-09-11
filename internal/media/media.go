// Package media holds the domain types shared by every adapter.
package media

import (
	"fmt"
	"strings"
	"time"
)

// Kind is the library a run targets.
type Kind string

const (
	Movies Kind = "movies"
	Series Kind = "series"
)

// ParseKind accepts the common spellings of movies and series.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "movie", "movies", "film", "films":
		return Movies, nil
	case "series", "show", "shows", "tv":
		return Series, nil
	}
	return "", fmt.Errorf("unknown kind %q (want movies or series)", s)
}

// Noun returns the plural noun used in prompts and output.
func (k Kind) Noun() string {
	if k == Series {
		return "TV shows"
	}
	return "movies"
}

// App returns the *arr application that manages this kind.
func (k Kind) App() string {
	if k == Series {
		return "Sonarr"
	}
	return "Radarr"
}

// Title is one library or history item.
type Title struct {
	TMDBID    int       `json:"tmdb_id,omitempty"`
	TVDBID    int       `json:"tvdb_id,omitempty"`
	Title     string    `json:"title"`
	Year      int       `json:"year,omitempty"`
	Genres    []string  `json:"genres,omitempty"`
	Added     time.Time `json:"added,omitempty"`
	PosterURL string    `json:"poster_url,omitempty"`
}

// Label renders "Title (Year)".
func (t Title) Label() string { return Label(t.Title, t.Year) }

// Label renders "Title (Year)", omitting an unknown year.
func Label(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}

// NormTitle lowercases and strips punctuation so titles from different
// sources can be compared. It is only used when no TMDB id is known.
func NormTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		case r == '&':
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("and")
			space = true
		default:
			space = true
		}
	}
	return strings.TrimPrefix(b.String(), "the ")
}
