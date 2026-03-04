package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestInsertAndListWebhookDeliveries(t *testing.T) {
	t.Parallel()

	store := newWebhookDeliveryTestStore(t)
	ctx := context.Background()

	httpStatus := 200
	errText := "timeout"
	_, err := store.InsertWebhookDelivery(ctx, WebhookDeliveryInput{
		EventID:     "evt_1",
		EventName:   "publish.attempt.created",
		TargetURL:   "https://example.com/hook-a",
		Status:      "delivered",
		HTTPStatus:  &httpStatus,
		Source:      "scheduler",
		DurationMS:  42,
		OccurredAt:  time.Now().UTC().Add(-1 * time.Minute),
		DeliveredAt: time.Now().UTC().Add(-1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("insert delivery 1: %v", err)
	}

	_, err = store.InsertWebhookDelivery(ctx, WebhookDeliveryInput{
		EventID:     "evt_2",
		EventName:   "post.state.changed",
		TargetURL:   "https://example.com/hook-a",
		Status:      "failed",
		HTTPStatus:  nil,
		Error:       &errText,
		Source:      "server",
		DurationMS:  18,
		OccurredAt:  time.Now().UTC(),
		DeliveredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("insert delivery 2: %v", err)
	}

	deliveries, err := store.ListRecentWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list recent deliveries: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliveries))
	}

	stats, err := store.ListWebhookTargetStats(ctx)
	if err != nil {
		t.Fatalf("list webhook stats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 target stat, got %d", len(stats))
	}
	if stats[0].TargetURL != "https://example.com/hook-a" {
		t.Fatalf("unexpected target url: %q", stats[0].TargetURL)
	}
	if stats[0].Total != 2 || stats[0].Delivered != 1 || stats[0].Failed != 1 {
		t.Fatalf("unexpected aggregate counts: %+v", stats[0])
	}
	if stats[0].LastStatus != "failed" {
		t.Fatalf("expected last status failed, got %q", stats[0].LastStatus)
	}
}

func newWebhookDeliveryTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "webhooks.db")
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
