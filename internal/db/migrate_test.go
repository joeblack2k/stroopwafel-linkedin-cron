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

	var columnCount int
	if err := database.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('posts') WHERE name='next_retry_at'`).Scan(&columnCount); err != nil {
		t.Fatalf("query next_retry_at column: %v", err)
	}
	if columnCount != 1 {
		t.Fatalf("expected next_retry_at column to exist, got count=%d", columnCount)
	}

	repeatedStatus, err := Migrate(context.Background(), database)
	if err != nil {
		t.Fatalf("run migration twice: %v", err)
	}
	if repeatedStatus == "" {
		t.Fatal("expected repeated migration status")
	}
}
