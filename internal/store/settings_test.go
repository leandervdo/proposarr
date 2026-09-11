package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	s, path := openTest(t)
	ctx := context.Background()
	if err := s.SaveSettings(ctx, []Setting{
		{Key: "radarr.url", Value: "http://r"},
		{Key: "plex.token", Value: "v1:abc", Secret: true},
		{Key: "tmdb.api_key", Value: "v1:def", Secret: true},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ctx, []Setting{{Key: "radarr.url", Value: "http://r2"}}, []string{"plex.token", "never.saved"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "radarr.url" || got[0].Value != "http://r2" || got[0].Secret || got[0].UpdatedAt.IsZero() ||
		got[1].Key != "tmdb.api_key" || !got[1].Secret {
		t.Fatalf("settings = %+v", got)
	}

	// A CLI process reads the same rows while the server holds the database.
	fromFile, err := LoadSettingsFile(ctx, path)
	if err != nil || len(fromFile) != 2 || fromFile[1].Value != "v1:def" {
		t.Fatalf("LoadSettingsFile = %+v, %v", fromFile, err)
	}
}

func TestLoadSettingsFileWithoutSettingsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE runs (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	rows, err := LoadSettingsFile(context.Background(), path)
	if err != nil || rows != nil {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
}
