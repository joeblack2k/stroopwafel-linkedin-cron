package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditEventCRUDListCountFilters(t *testing.T) {
	t.Parallel()

	store := newAuditEventTestStore(t)
	ctx := context.Background()

	metadata := `{"field":"display_name"}`
	firstCreatedAt := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	first, err := store.CreateAuditEvent(ctx, AuditEventInput{
		Action:    " channel.updated ",
		Resource:  " channel:7 ",
		AuthActor: " Admin User ",
		Source:    " API ",
		Metadata:  &metadata,
		CreatedAt: firstCreatedAt,
	})
	if err != nil {
		t.Fatalf("create first audit event: %v", err)
	}
	if first.Action != "channel.updated" {
		t.Fatalf("expected trimmed action, got %q", first.Action)
	}
	if first.Resource != "channel:7" {
		t.Fatalf("expected trimmed resource, got %q", first.Resource)
	}
	if first.AuthActor != "Admin User" {
		t.Fatalf("expected actor Admin User, got %q", first.AuthActor)
	}
	if first.Source != "api" {
		t.Fatalf("expected lowercase source api, got %q", first.Source)
	}
	if first.Metadata == nil || *first.Metadata != metadata {
		t.Fatalf("expected metadata %q, got %+v", metadata, first.Metadata)
	}
	if !first.CreatedAt.Equal(firstCreatedAt) {
		t.Fatalf("expected created_at %s, got %s", firstCreatedAt, first.CreatedAt)
	}

	secondCreatedAt := firstCreatedAt.Add(1 * time.Hour)
	second, err := store.CreateAuditEvent(ctx, AuditEventInput{
		Action:    "post.published",
		Resource:  "post:42",
		AuthActor: " ",
		Source:    " ",
		CreatedAt: secondCreatedAt,
	})
	if err != nil {
		t.Fatalf("create second audit event: %v", err)
	}
	if second.AuthActor != "unknown" {
		t.Fatalf("expected default auth_actor unknown, got %q", second.AuthActor)
	}
	if second.Source != "unknown" {
		t.Fatalf("expected default source unknown, got %q", second.Source)
	}

	got, err := store.GetAuditEvent(ctx, first.ID)
	if err != nil {
		t.Fatalf("get audit event: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("expected id %d, got %d", first.ID, got.ID)
	}
	if got.Action != first.Action || got.Resource != first.Resource {
		t.Fatalf("unexpected read-back event: %+v", got)
	}

	allCount, err := store.CountAuditEvents(ctx, AuditEventFilter{})
	if err != nil {
		t.Fatalf("count all audit events: %v", err)
	}
	if allCount != 2 {
		t.Fatalf("expected total count 2, got %d", allCount)
	}

	allEvents, err := store.ListAuditEvents(ctx, AuditEventFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("list all audit events: %v", err)
	}
	if len(allEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(allEvents))
	}
	if allEvents[0].ID != second.ID || allEvents[1].ID != first.ID {
		t.Fatalf("expected DESC created_at order [%d,%d], got [%d,%d]", second.ID, first.ID, allEvents[0].ID, allEvents[1].ID)
	}

	pagedEvents, err := store.ListAuditEvents(ctx, AuditEventFilter{}, 1, 1)
	if err != nil {
		t.Fatalf("list paged audit events: %v", err)
	}
	if len(pagedEvents) != 1 || pagedEvents[0].ID != first.ID {
		t.Fatalf("expected one paged event with id %d, got %+v", first.ID, pagedEvents)
	}

	combinedFilter := AuditEventFilter{
		Action:    first.Action,
		Resource:  first.Resource,
		AuthActor: first.AuthActor,
		Source:    first.Source,
	}
	filteredCount, err := store.CountAuditEvents(ctx, combinedFilter)
	if err != nil {
		t.Fatalf("count filtered audit events: %v", err)
	}
	if filteredCount != 1 {
		t.Fatalf("expected filtered count=1, got %d", filteredCount)
	}

	filteredEvents, err := store.ListAuditEvents(ctx, combinedFilter, 10, 0)
	if err != nil {
		t.Fatalf("list filtered audit events: %v", err)
	}
	if len(filteredEvents) != 1 || filteredEvents[0].ID != first.ID {
		t.Fatalf("expected filtered event id=%d, got %+v", first.ID, filteredEvents)
	}

	unknownFilteredCount, err := store.CountAuditEvents(ctx, AuditEventFilter{AuthActor: "unknown", Source: "unknown"})
	if err != nil {
		t.Fatalf("count unknown/default filtered audit events: %v", err)
	}
	if unknownFilteredCount != 1 {
		t.Fatalf("expected unknown/default filtered count=1, got %d", unknownFilteredCount)
	}

	if _, err := store.GetAuditEvent(ctx, second.ID+999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing id, got %v", err)
	}
}

func newAuditEventTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit_events.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return NewStore(database)
}
