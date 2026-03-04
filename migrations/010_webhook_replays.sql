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
