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

// FullDetails is everything the detail view shows about one title. Directors,
// Cast, Genres and Streaming are never nil.
type FullDetails struct {
	ID          int          `json:"tmdb_id"`
	Kind        media.Kind   `json:"kind"`
	Title       string       `json:"title"`
	Year        int          `json:"year,omitempty"`
	Tagline     string       `json:"tagline,omitempty"`
	Overview    string       `json:"overview,omitempty"`
	Genres      []string     `json:"genres"`
	Runtime     int          `json:"runtime,omitempty"`      // minutes; first episode runtime for tv
	ReleaseDate string       `json:"release_date,omitempty"` // first air date for tv
	Status      string       `json:"status,omitempty"`
	Seasons     int          `json:"seasons,omitempty"`
	Episodes    int          `json:"episodes,omitempty"`
	PosterURL   string       `json:"poster_url,omitempty"`
	BackdropURL string       `json:"backdrop_url,omitempty"`
	Directors   []string     `json:"directors"` // creators for tv
	Cast        []CastMember `json:"cast"`      // top billed, at most MaxCast
	Trailer     *Trailer     `json:"trailer,omitempty"`
	Streaming   []string     `json:"streaming"` // flatrate providers in the requested region
	IMDBID      string       `json:"imdb_id,omitempty"`
	TVDBID      int          `json:"-"`
	Rating      float64      `json:"tmdb_rating,omitempty"`
	Votes       int          `json:"tmdb_votes,omitempty"`
}

type CastMember struct {
	Name       string `json:"name"`
	Character  string `json:"character,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// Trailer is a YouTube video; link it as https://www.youtube.com/watch?v=<key>.
type Trailer struct {
	Name       string `json:"name"`
	YouTubeKey string `json:"youtube_key"`
}

// MaxCast is the number of cast members FullDetails keeps.
const MaxCast = 12

const (
	imageBase    = "https://image.tmdb.org/t/p/w500"
	profileBase  = "https://image.tmdb.org/t/p/w185"
	backdropBase = "https://image.tmdb.org/t/p/w1280"
)

// PosterURL turns a poster_path into an absolute URL.
func PosterURL(path string) string {
	return imageURL(imageBase, path)
}

func imageURL(base, path string) string {
	if path == "" {
		return ""
	}
	return base + path
}
