package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAPIKeyLifecycle(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "apikeys.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := NewStore(database)
	created, token, err := store.CreateAPIKey(context.Background(), "agent-key")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty api key token")
	}
	if created.ID == 0 {
		t.Fatal("expected api key id")
	}

	authenticated, err := store.AuthenticateAPIKey(context.Background(), token)
	if err != nil {
		t.Fatalf("authenticate api key: %v", err)
	}
	if authenticated.ID != created.ID {
		t.Fatalf("expected authenticated id=%d, got %d", created.ID, authenticated.ID)
	}
	if authenticated.LastUsedAt == nil {
		t.Fatal("expected last_used_at to be updated")
	}

	keys, err := store.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected one api key, got %d", len(keys))
	}

	if err := store.RevokeAPIKey(context.Background(), created.ID); err != nil {
		t.Fatalf("revoke api key: %v", err)
	}

	_, err = store.AuthenticateAPIKey(context.Background(), token)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for revoked key, got %v", err)
	}
}
