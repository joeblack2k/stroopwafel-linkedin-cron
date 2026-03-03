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
