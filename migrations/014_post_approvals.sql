ALTER TABLE posts ADD COLUMN approval_pending INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_posts_approval_pending ON posts(approval_pending, created_at DESC, id DESC);
