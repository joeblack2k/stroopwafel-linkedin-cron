CREATE TABLE IF NOT EXISTS channel_retry_policies (
    channel_id INTEGER PRIMARY KEY,
    max_retries INTEGER NOT NULL DEFAULT 3,
    backoff_first_seconds INTEGER NOT NULL DEFAULT 60,
    backoff_second_seconds INTEGER NOT NULL DEFAULT 300,
    backoff_third_seconds INTEGER NOT NULL DEFAULT 900,
    rate_limit_backoff_seconds INTEGER NOT NULL DEFAULT 1800,
    max_posts_per_day INTEGER NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_channel_retry_policies_updated ON channel_retry_policies(updated_at DESC, channel_id DESC);
