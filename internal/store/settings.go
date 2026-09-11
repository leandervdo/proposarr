package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Setting is one value saved from the web UI. Secret values are ciphertext;
// the store never sees them in the clear.
type Setting struct {
	Key       string
	Value     string
	Secret    bool
	UpdatedAt time.Time
}

func (s *SQLite) Settings(ctx context.Context) ([]Setting, error) {
	return querySettings(ctx, s.db)
}

// SaveSettings upserts and removes settings in one transaction.
func (s *SQLite) SaveSettings(ctx context.Context, upsert []Setting, remove []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: save settings: %w", err)
	}
	defer tx.Rollback()
	for _, st := range upsert {
		if st.UpdatedAt.IsZero() {
			st.UpdatedAt = s.now()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, secret, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, secret = excluded.secret, updated_at = excluded.updated_at`,
			st.Key, st.Value, st.Secret, fmtTime(st.UpdatedAt)); err != nil {
			return fmt.Errorf("store: save setting %s: %w", st.Key, err)
		}
	}
	for _, key := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("store: remove setting %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: save settings: %w", err)
	}
	return nil
}

// LoadSettingsFile reads saved settings from the database at path without
// migrating it or touching runs, so a CLI command can read them while
// `proposarr serve` owns the database. A database without settings returns nil.
func LoadSettingsFile(ctx context.Context, path string) ([]Setting, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'settings'`).Scan(&n); err != nil {
		return nil, fmt.Errorf("store: read settings: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	return querySettings(ctx, db)
}

func querySettings(ctx context.Context, db *sql.DB) ([]Setting, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value, secret, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("store: read settings: %w", err)
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var (
			st      Setting
			updated string
		)
		if err := rows.Scan(&st.Key, &st.Value, &st.Secret, &updated); err != nil {
			return nil, fmt.Errorf("store: read settings: %w", err)
		}
		if st.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, fmt.Errorf("store: read settings: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
