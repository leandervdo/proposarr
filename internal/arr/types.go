// Package arr holds the Sonarr and Radarr v3 API clients.
package arr

type QualityProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type RootFolder struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Accessible bool   `json:"accessible"`
	FreeSpace  int64  `json:"freeSpace"`
}

type SystemStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// Lookup is a title returned by an *arr lookup endpoint, ready to add.
// LibraryID is non-zero when the title is already in the library.
type Lookup struct {
	LibraryID int
	Title     string
	Year      int
	TMDBID    int
	TVDBID    int
	Ratings   Ratings
	Raw       map[string]any // the lookup resource, posted back on add
}

// Ratings are the third-party ratings of a lookup; zero means unknown.
// Sonarr only carries the IMDb rating.
type Ratings struct {
	IMDB           float64 // 0-10
	IMDBVotes      int
	RottenTomatoes int // critic score, 0-100
	Metacritic     int // 0-100
}

// rating is one *arr rating value.
type rating struct {
	Value float64 `json:"value"`
	Votes int     `json:"votes"`
}

// AddOptions are the choices made for one title before it is added.
type AddOptions struct {
	QualityProfileID    int
	RootFolderPath      string
	MinimumAvailability string // Radarr only
	Search              bool
}

type Added struct {
	ID    int
	Title string
}

// ListMovie is one entry of Radarr's import-list / discover feed.
type ListMovie struct {
	TMDBID   int
	Title    string
	Year     int
	Overview string
	Genres   []string
	Rating   float64
	Votes    int
}
