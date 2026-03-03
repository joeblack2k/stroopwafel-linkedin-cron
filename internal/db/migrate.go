package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type migration struct {
	name string
	sql  string
}

var migrations = []migration{
	{
		name: "001_init",
		sql: `
CREATE TABLE IF NOT EXISTS posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scheduled_at TEXT NULL,
    text TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'scheduled', 'sent', 'failed')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    sent_at TEXT NULL,
    fail_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    media_url TEXT NULL,
    next_retry_at TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_posts_status_due ON posts(status, scheduled_at, next_retry_at);
`,
	},
	{
		name: "002_api_keys",
		sql: `
CREATE TABLE IF NOT EXISTS api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    last_used_at TEXT NULL,
    revoked_at TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
`,
	},
}

func Migrate(ctx context.Context, database *sql.DB) (string, error) {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return "", fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied := make([]string, 0)
	now := formatDBTime(time.Now().UTC())

	for _, item := range migrations {
		var exists int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, item.name).Scan(&exists); err != nil {
			return "", fmt.Errorf("check migration %s status: %w", item.name, err)
		}
		if exists > 0 {
			continue
		}

		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return "", fmt.Errorf("begin migration tx for %s: %w", item.name, err)
		}

		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`, item.name, now); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("record migration %s: %w", item.name, err)
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit migration tx for %s: %w", item.name, err)
		}

		applied = append(applied, item.name)
	}

	if len(applied) == 0 {
		return "all migrations already applied", nil
	}
	return "applied migrations: " + strings.Join(applied, ","), nil
}
