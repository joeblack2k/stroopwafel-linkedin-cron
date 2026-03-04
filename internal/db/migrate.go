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
	{
		name: "003_channels",
		sql: `
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_test_at TEXT NULL,
    last_error TEXT NULL,
    linkedin_access_token TEXT NULL,
    linkedin_author_urn TEXT NULL,
    linkedin_api_base_url TEXT NULL,
    facebook_page_access_token TEXT NULL,
    facebook_page_id TEXT NULL,
    facebook_api_base_url TEXT NULL
);

CREATE TABLE IF NOT EXISTS post_channels (
    post_id INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (post_id, channel_id),
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_post_channels_channel_id ON post_channels(channel_id);
`,
	},
	{
		name: "004_publish_attempts",
		sql: `
CREATE TABLE IF NOT EXISTS publish_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    attempt_no INTEGER NOT NULL,
    attempted_at TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT NULL,
    retry_at TEXT NULL,
    external_id TEXT NULL,
    UNIQUE(post_id, channel_id, attempt_no),
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_publish_attempts_post_channel ON publish_attempts(post_id, channel_id, attempt_no DESC);
CREATE INDEX IF NOT EXISTS idx_publish_attempts_retry_at ON publish_attempts(retry_at);
`,
	},
	{
		name: "005_channel_audit_events",
		sql: `
CREATE TABLE IF NOT EXISTS channel_audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL,
    summary TEXT NOT NULL,
    metadata TEXT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_channel_audit_events_channel_created ON channel_audit_events(channel_id, created_at DESC, id DESC);
`,
	},
	{
		name: "006_proof_log_and_channel_rules",
		sql: `
ALTER TABLE publish_attempts ADD COLUMN permalink TEXT NULL;
ALTER TABLE publish_attempts ADD COLUMN error_category TEXT NULL;
ALTER TABLE publish_attempts ADD COLUMN screenshot_url TEXT NULL;

CREATE TABLE IF NOT EXISTS channel_rules (
    channel_id INTEGER PRIMARY KEY,
    max_text_length INTEGER NULL,
    max_hashtags INTEGER NULL,
    required_phrase TEXT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);
`,
	},
	{
		name: "007_instagram_and_post_media_type",
		sql: `
ALTER TABLE channels ADD COLUMN instagram_access_token TEXT NULL;
ALTER TABLE channels ADD COLUMN instagram_business_account_id TEXT NULL;
ALTER TABLE channels ADD COLUMN instagram_api_base_url TEXT NULL;

ALTER TABLE posts ADD COLUMN media_type TEXT NULL;
`,
	},
	{
		name: "008_api_idempotency",
		sql: `
CREATE TABLE IF NOT EXISTS api_idempotency (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    auth_scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 0,
    response_body TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_idempotency_scope_key
ON api_idempotency(auth_scope, idempotency_key);
`,
	},
	{
		name: "009_webhook_deliveries",
		sql: `
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL,
    event_name TEXT NOT NULL,
    target_url TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('delivered', 'failed')),
    http_status INTEGER NULL,
    error TEXT NULL,
    source TEXT NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    occurred_at TEXT NOT NULL,
    delivered_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_recent ON webhook_deliveries(delivered_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_target ON webhook_deliveries(target_url, delivered_at DESC, id DESC);
`,
	},
	{
		name: "010_webhook_replays",
		sql: `
CREATE TABLE IF NOT EXISTS webhook_replays (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    delivery_id INTEGER NULL,
    event_id TEXT NOT NULL,
    event_name TEXT NOT NULL,
    target_url TEXT NOT NULL,
    source TEXT NOT NULL,
    payload TEXT NOT NULL,
    headers TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL CHECK (status IN ('queued', 'processing', 'delivered', 'failed', 'cancelled')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    last_http_status INTEGER NULL,
    last_attempt_at TEXT NULL,
    next_attempt_at TEXT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (delivery_id) REFERENCES webhook_deliveries(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_replays_status_next ON webhook_replays(status, next_attempt_at ASC, created_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_webhook_replays_target_created ON webhook_replays(target_url, created_at DESC, id DESC);
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
