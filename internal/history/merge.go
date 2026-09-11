package history

import (
	"fmt"
	"sort"

	"github.com/leandervdo/proposarr/internal/media"
)

// seriesWatchedEpisodes is the distinct-episode count that marks a series Watched
// regardless of its length.
const seriesWatchedEpisodes = 6

func movieSignal(plays int) Signal {
	if plays >= 2 {
		return Rewatched
	}
	return Watched
}

// seriesSignal classifies a series from the distinct episodes watched in the
// window, its total episode count (0 when unknown) and the most plays of any
// single episode.
func seriesSignal(distinct, total, maxEpisodePlays int) Signal {
	switch {
	case maxEpisodePlays >= 2:
		return Rewatched
	case distinct >= seriesWatchedEpisodes, total > 0 && distinct*2 >= total:
		return Watched
	}
	return Partial
}

func signalRank(s Signal) int {
	switch s {
	case Rewatched:
		return 3
	case Watched:
		return 2
	case Partial:
		return 1
	}
	return 0
}

func mergeKey(e Entry) string {
	switch {
	case e.TMDBID > 0:
		return fmt.Sprintf("%s:tmdb:%d", e.Kind, e.TMDBID)
	case e.TVDBID > 0:
		return fmt.Sprintf("%s:tvdb:%d", e.Kind, e.TVDBID)
	}
	return fmt.Sprintf("%s:title:%s:%d", e.Kind, media.NormTitle(e.Title), e.Year)
}

// Merge combines entries for the same title across lists (users or servers):
// plays are summed, the most episodes, latest watch and strongest signal win,
// and missing ids and years are filled in. The result is sorted by LastWatched
// descending.
func Merge(lists ...[]Entry) []Entry {
	index := map[string]int{}
	var out []Entry
	for _, list := range lists {
		for _, e := range list {
			k := mergeKey(e)
			i, ok := index[k]
			if !ok {
				index[k] = len(out)
				out = append(out, e)
				continue
			}
			m := &out[i]
			m.Plays += e.Plays
			m.Episodes = max(m.Episodes, e.Episodes)
			if e.LastWatched.After(m.LastWatched) {
				m.LastWatched = e.LastWatched
			}
			if signalRank(e.Signal) > signalRank(m.Signal) {
				m.Signal = e.Signal
			}
			if m.TMDBID == 0 {
				m.TMDBID = e.TMDBID
			}
			if m.TVDBID == 0 {
				m.TVDBID = e.TVDBID
			}
			if m.Year == 0 {
				m.Year = e.Year
			}
			if m.Title == "" {
				m.Title = e.Title
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].LastWatched.After(out[b].LastWatched) })
	return out
}
