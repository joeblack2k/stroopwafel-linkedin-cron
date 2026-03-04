CREATE TABLE IF NOT EXISTS media_assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_url TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL,
    filename TEXT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    stored_path TEXT NULL,
    source TEXT NOT NULL DEFAULT 'upload',
    tags TEXT NOT NULL DEFAULT '[]',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_assets_updated ON media_assets(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_media_assets_type_updated ON media_assets(media_type, updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS content_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NULL,
    body TEXT NOT NULL,
    channel_type TEXT NULL,
    media_asset_id INTEGER NULL,
    tags TEXT NOT NULL DEFAULT '[]',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (media_asset_id) REFERENCES media_assets(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_content_templates_name ON content_templates(name);
CREATE INDEX IF NOT EXISTS idx_content_templates_updated ON content_templates(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_content_templates_channel_active ON content_templates(channel_type, is_active, updated_at DESC, id DESC);

