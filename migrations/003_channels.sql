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
