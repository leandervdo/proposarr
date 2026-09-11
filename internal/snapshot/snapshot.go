// Package snapshot caches the Sonarr and Radarr library on disk.
package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/leandervdo/proposarr/internal/media"
)

type MovieSource interface {
	Movies(ctx context.Context) ([]media.Title, error)
}

type SeriesSource interface {
	Series(ctx context.Context) ([]media.Title, error)
}

type TVDBFinder interface {
	FindByTVDB(ctx context.Context, tvdbID int) (int, error)
}

type Library struct {
	Dir     string // cache lives in Dir/cache; empty disables caching
	TTL     time.Duration
	Radarr  MovieSource
	Sonarr  SeriesSource
	TMDB    TVDBFinder
	Refresh bool
	Now     func() time.Time
}

// cacheVersion is bumped whenever media.Title gains fields, so caches written
// by an older build are refetched instead of served without them.
const cacheVersion = 2

type cacheFile struct {
	Version   int           `json:"version"`
	FetchedAt time.Time     `json:"fetched_at"`
	Titles    []media.Title `json:"titles"`
}

const resolveConcurrency = 6

// Titles returns the library for kind, from cache when it is younger than TTL.
func (l *Library) Titles(ctx context.Context, kind media.Kind) ([]media.Title, error) {
	var fetch func(context.Context) ([]media.Title, error)
	switch kind {
	case media.Movies:
		if l.Radarr != nil {
			fetch = l.Radarr.Movies
		}
	case media.Series:
		if l.Sonarr != nil {
			fetch = l.Sonarr.Series
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	if fetch == nil {
		return nil, fmt.Errorf("%s is not configured", kind.App())
	}

	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	path := l.path(kind)

	if path != "" && !l.Refresh && l.TTL > 0 {
		if c, ok := readCache(path); ok {
			if age := now().Sub(c.FetchedAt); age >= 0 && age < l.TTL {
				return c.Titles, nil
			}
		}
	}

	titles, err := fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch %s library: %w", kind.App(), err)
	}
	if kind == media.Series && l.TMDB != nil {
		l.resolveTMDB(ctx, titles)
	}
	if path != "" {
		// A failed cache write only costs a refetch next run.
		_ = writeCache(path, cacheFile{FetchedAt: now(), Titles: titles})
	}
	return titles, nil
}

func (l *Library) path(kind media.Kind) string {
	if l.Dir == "" {
		return ""
	}
	return filepath.Join(l.Dir, "cache", "library-"+string(kind)+".json")
}

func (l *Library) resolveTMDB(ctx context.Context, titles []media.Title) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, resolveConcurrency)
	for i := range titles {
		if titles[i].TMDBID != 0 || titles[i].TVDBID <= 0 {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if id, err := l.TMDB.FindByTVDB(ctx, titles[i].TVDBID); err == nil {
				titles[i].TMDBID = id
			}
		}(i)
	}
	wg.Wait()
}

func readCache(path string) (cacheFile, bool) {
	var c cacheFile
	b, err := os.ReadFile(path)
	if err != nil {
		return c, false
	}
	if err := json.Unmarshal(b, &c); err != nil || c.FetchedAt.IsZero() || c.Version != cacheVersion {
		return c, false
	}
	return c, true
}

func writeCache(path string, c cacheFile) error {
	c.Version = cacheVersion
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".library-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
