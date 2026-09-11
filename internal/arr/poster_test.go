package arr

import "testing"

func TestPosterURL(t *testing.T) {
	tests := []struct {
		name   string
		images []image
		want   string
	}{
		{
			name: "tmdb original resized",
			images: []image{
				{CoverType: "fanart", RemoteURL: "https://image.tmdb.org/t/p/original/fan.jpg"},
				{CoverType: "poster", URL: "/MediaCover/38/poster.jpg", RemoteURL: "https://image.tmdb.org/t/p/original/6oom5QYQ2yQTMJIbnvbkBL9cHo6.jpg"},
			},
			want: "https://image.tmdb.org/t/p/w500/6oom5QYQ2yQTMJIbnvbkBL9cHo6.jpg",
		},
		{
			name:   "tvdb remote kept",
			images: []image{{CoverType: "poster", RemoteURL: "https://artworks.thetvdb.com/banners/posters/81189-10.jpg"}},
			want:   "https://artworks.thetvdb.com/banners/posters/81189-10.jpg",
		},
		{
			name:   "relative local cover dropped",
			images: []image{{CoverType: "poster", URL: "/MediaCover/38/poster.jpg"}},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := posterURL(tt.images); got != tt.want {
				t.Errorf("posterURL = %q, want %q", got, tt.want)
			}
		})
	}
}
