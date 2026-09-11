package arr

import "strings"

const (
	tmdbOriginal = "://image.tmdb.org/t/p/original/"
	tmdbW500     = "://image.tmdb.org/t/p/w500/"
)

// sizedPoster swaps TMDB's full-size original image, often several MB, for
// the 500px rendition a poster grid needs. Other URLs are returned unchanged.
func sizedPoster(u string) string {
	return strings.Replace(u, tmdbOriginal, tmdbW500, 1)
}
