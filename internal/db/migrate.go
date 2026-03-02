package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const initialMigrationName = "001_init"

const initialMigrationSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);

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
`

func Migrate(ctx context.Context, database *sql.DB) (string, error) {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return "", fmt.Errorf("create schema_migrations table: %w", err)
	}

	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, initialMigrationName).Scan(&exists); err != nil {
		return "", fmt.Errorf("check migration status: %w", err)
	}
	if exists > 0 {
		return initialMigrationName + " already_applied", nil
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin migration tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, initialMigrationSQL); err != nil {
		return "", fmt.Errorf("apply migration %s: %w", initialMigrationName, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`, initialMigrationName, formatDBTime(time.Now().UTC())); err != nil {
		return "", fmt.Errorf("record migration %s: %w", initialMigrationName, err)
	}
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("commit migration tx: %w", err)
	}

	return initialMigrationName + " applied", nil
}
