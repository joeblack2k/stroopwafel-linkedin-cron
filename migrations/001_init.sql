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
