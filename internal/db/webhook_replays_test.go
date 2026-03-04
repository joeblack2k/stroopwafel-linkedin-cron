package db

import (
	"context"
	"testing"
	"time"
)

func TestWebhookReplayCRUDAndFilters(t *testing.T) {
	t.Parallel()

	store := newWebhookDeliveryTestStore(t)
	ctx := context.Background()

	failedStatus := WebhookReplayStatusFailed
	queued, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:   "evt_queued",
		EventName: "publish.attempt.created",
		TargetURL: "https://example.com/hook-a",
		Source:    "server",
		Payload:   `{"post_id":1}`,
		Headers:   `{"X-Stroopwafel-Event":"publish.attempt.created"}`,
		Status:    WebhookReplayStatusQueued,
	})
	if err != nil {
		t.Fatalf("insert queued replay: %v", err)
	}

	failed, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:   "evt_failed",
		EventName: "post.state.changed",
		TargetURL: "https://example.com/hook-b",
		Source:    "scheduler",
		Payload:   `{"post_id":2}`,
		Headers:   `{"X-Stroopwafel-Event":"post.state.changed"}`,
		Status:    failedStatus,
	})
	if err != nil {
		t.Fatalf("insert failed replay: %v", err)
	}

	total, err := store.CountWebhookReplays(ctx, WebhookReplayFilter{})
	if err != nil {
		t.Fatalf("count replays: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}

	failedCount, err := store.CountWebhookReplays(ctx, WebhookReplayFilter{Status: WebhookReplayStatusFailed})
	if err != nil {
		t.Fatalf("count failed replays: %v", err)
	}
	if failedCount != 1 {
		t.Fatalf("expected failed count 1, got %d", failedCount)
	}

	list, err := store.ListWebhookReplays(ctx, WebhookReplayFilter{Status: WebhookReplayStatusFailed}, 10, 0)
	if err != nil {
		t.Fatalf("list failed replays: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one failed replay, got %d", len(list))
	}
	if list[0].ID != failed.ID {
		t.Fatalf("expected replay id %d, got %d", failed.ID, list[0].ID)
	}

	httpStatus := 502
	nextAttempt := time.Now().UTC().Add(5 * time.Minute)
	lastError := "gateway timeout"
	updated, err := store.UpdateWebhookReplayAfterAttempt(ctx, failed.ID, WebhookReplayStatusFailed, &httpStatus, &lastError, &nextAttempt)
	if err != nil {
		t.Fatalf("update replay attempt: %v", err)
	}
	if updated.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1, got %d", updated.AttemptCount)
	}
	if updated.HTTPStatus == nil || *updated.HTTPStatus != 502 {
		t.Fatalf("expected http status 502, got %+v", updated.HTTPStatus)
	}
	if updated.LastError == nil || *updated.LastError != lastError {
		t.Fatalf("expected last_error %q, got %+v", lastError, updated.LastError)
	}
	if updated.NextAttempt == nil {
		t.Fatalf("expected next_attempt_at to be set")
	}

	cancelled, err := store.UpdateWebhookReplayStatus(ctx, queued.ID, WebhookReplayStatusCancelled, nil, nil)
	if err != nil {
		t.Fatalf("cancel replay: %v", err)
	}
	if cancelled.Status != WebhookReplayStatusCancelled {
		t.Fatalf("expected status cancelled, got %q", cancelled.Status)
	}

	due, err := store.ListWebhookReplaysDue(ctx, time.Now().UTC().Add(10*time.Minute), 20)
	if err != nil {
		t.Fatalf("list due replays: %v", err)
	}
	if len(due) == 0 {
		t.Fatalf("expected at least one due replay")
	}
}
