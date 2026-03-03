package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReserveAndCompleteAPIIdempotency(t *testing.T) {
	t.Parallel()

	store := setupIdempotencyStore(t)
	ctx := context.Background()

	input := APIIdempotencyInput{
		AuthScope:      "api-key:1",
		IdempotencyKey: "idem-1",
		Method:         "POST",
		Path:           "/api/v1/posts",
		RequestHash:    "abc123",
	}

	record, created, err := store.ReserveAPIIdempotency(ctx, input)
	if err != nil {
		t.Fatalf("reserve idempotency: %v", err)
	}
	if !created {
		t.Fatal("expected first reservation to be created")
	}
	if record.StatusCode != 0 {
		t.Fatalf("expected pending status_code=0, got %d", record.StatusCode)
	}

	if err := store.CompleteAPIIdempotency(ctx, input.AuthScope, input.IdempotencyKey, 201, `{"ok":true}`); err != nil {
		t.Fatalf("complete idempotency: %v", err)
	}

	replayed, created, err := store.ReserveAPIIdempotency(ctx, input)
	if err != nil {
		t.Fatalf("reserve existing idempotency: %v", err)
	}
	if created {
		t.Fatal("expected existing reservation to be replayed")
	}
	if replayed.StatusCode != 201 {
		t.Fatalf("expected replay status_code=201, got %d", replayed.StatusCode)
	}
	if replayed.ResponseBody != `{"ok":true}` {
		t.Fatalf("unexpected replay response body: %q", replayed.ResponseBody)
	}
}

func setupIdempotencyStore(t *testing.T) *Store {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "idempotency.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	return NewStore(database)
}
