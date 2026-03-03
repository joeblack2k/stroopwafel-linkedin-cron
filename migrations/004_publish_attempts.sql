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
