CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    auth_actor TEXT NOT NULL,
    source TEXT NOT NULL,
    metadata TEXT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action_created ON audit_events(action, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource_created ON audit_events(resource, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_created ON audit_events(auth_actor, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_source_created ON audit_events(source, created_at DESC, id DESC);
