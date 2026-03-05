package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateBootstrapsSchema(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	status, err := Migrate(context.Background(), database)
	if err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if status == "" {
		t.Fatal("expected migration status")
	}

	var tableCount int
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='posts'`).Scan(&tableCount); err != nil {
		t.Fatalf("query posts table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected posts table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='api_keys'`).Scan(&tableCount); err != nil {
		t.Fatalf("query api_keys table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected api_keys table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='channels'`).Scan(&tableCount); err != nil {
		t.Fatalf("query channels table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected channels table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='post_channels'`).Scan(&tableCount); err != nil {
		t.Fatalf("query post_channels table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected post_channels table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='publish_attempts'`).Scan(&tableCount); err != nil {
		t.Fatalf("query publish_attempts table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected publish_attempts table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='channel_audit_events'`).Scan(&tableCount); err != nil {
		t.Fatalf("query channel_audit_events table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected channel_audit_events table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='api_idempotency'`).Scan(&tableCount); err != nil {
		t.Fatalf("query api_idempotency table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected api_idempotency table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='webhook_deliveries'`).Scan(&tableCount); err != nil {
		t.Fatalf("query webhook_deliveries table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected webhook_deliveries table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='webhook_replays'`).Scan(&tableCount); err != nil {
		t.Fatalf("query webhook_replays table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected webhook_replays table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='audit_events'`).Scan(&tableCount); err != nil {
		t.Fatalf("query audit_events table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected audit_events table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='channel_retry_policies'`).Scan(&tableCount); err != nil {
		t.Fatalf("query channel_retry_policies table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected channel_retry_policies table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='media_assets'`).Scan(&tableCount); err != nil {
		t.Fatalf("query media_assets table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected media_assets table to exist, got count=%d", tableCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='content_templates'`).Scan(&tableCount); err != nil {
		t.Fatalf("query content_templates table: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected content_templates table to exist, got count=%d", tableCount)
	}

	var columnCount int
	if err := database.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('posts') WHERE name='next_retry_at'`).Scan(&columnCount); err != nil {
		t.Fatalf("query next_retry_at column: %v", err)
	}
	if columnCount != 1 {
		t.Fatalf("expected next_retry_at column to exist, got count=%d", columnCount)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('posts') WHERE name='approval_pending'`).Scan(&columnCount); err != nil {
		t.Fatalf("query approval_pending column: %v", err)
	}
	if columnCount != 1 {
		t.Fatalf("expected approval_pending column to exist, got count=%d", columnCount)
	}

	repeatedStatus, err := Migrate(context.Background(), database)
	if err != nil {
		t.Fatalf("run migration twice: %v", err)
	}
	if repeatedStatus == "" {
		t.Fatal("expected repeated migration status")
	}

	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if migrationCount < 5 {
		t.Fatalf("expected at least 5 migrations recorded, got %d", migrationCount)
	}
}
