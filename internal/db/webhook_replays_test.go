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

func TestWebhookDeadLetterFilters(t *testing.T) {
	t.Parallel()

	store := newWebhookDeliveryTestStore(t)
	ctx := context.Background()

	if _, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:      "evt_dead_default",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/a",
		Source:       "scheduler",
		Payload:      `{"post_id":1}`,
		Status:       WebhookReplayStatusFailed,
		AttemptCount: 3,
	}); err != nil {
		t.Fatalf("insert default dead letter: %v", err)
	}

	if _, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:      "evt_not_enough_attempts",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/a",
		Source:       "scheduler",
		Payload:      `{"post_id":2}`,
		Status:       WebhookReplayStatusFailed,
		AttemptCount: 2,
	}); err != nil {
		t.Fatalf("insert low attempt replay: %v", err)
	}

	nextAttempt := time.Now().UTC().Add(10 * time.Minute)
	if _, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:      "evt_retryable",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/b",
		Source:       "scheduler",
		Payload:      `{"post_id":3}`,
		Status:       WebhookReplayStatusFailed,
		AttemptCount: 5,
		NextAttempt:  &nextAttempt,
	}); err != nil {
		t.Fatalf("insert replay with next attempt: %v", err)
	}

	if _, err := store.InsertWebhookReplay(ctx, WebhookReplayInput{
		EventID:      "evt_delivered",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/a",
		Source:       "scheduler",
		Payload:      `{"post_id":4}`,
		Status:       WebhookReplayStatusDelivered,
		AttemptCount: 4,
	}); err != nil {
		t.Fatalf("insert delivered replay: %v", err)
	}

	defaultCount, err := store.CountWebhookDeadLetters(ctx, WebhookDeadLetterFilter{})
	if err != nil {
		t.Fatalf("count dead letters default: %v", err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected default dead letter count 1, got %d", defaultCount)
	}

	defaultList, err := store.ListWebhookDeadLetters(ctx, WebhookDeadLetterFilter{}, 20, 0)
	if err != nil {
		t.Fatalf("list dead letters default: %v", err)
	}
	if len(defaultList) != 1 {
		t.Fatalf("expected default dead letter list length 1, got %d", len(defaultList))
	}
	if defaultList[0].EventID != "evt_dead_default" {
		t.Fatalf("expected evt_dead_default, got %q", defaultList[0].EventID)
	}

	customCount, err := store.CountWebhookDeadLetters(ctx, WebhookDeadLetterFilter{MinAttempts: 2})
	if err != nil {
		t.Fatalf("count dead letters min_attempts=2: %v", err)
	}
	if customCount != 2 {
		t.Fatalf("expected dead letter count 2 for min_attempts=2, got %d", customCount)
	}

	filteredList, err := store.ListWebhookDeadLetters(ctx, WebhookDeadLetterFilter{
		TargetURL:   "https://example.com/a",
		EventName:   "post.state.changed",
		MinAttempts: 2,
	}, 20, 0)
	if err != nil {
		t.Fatalf("list filtered dead letters: %v", err)
	}
	if len(filteredList) != 2 {
		t.Fatalf("expected filtered dead letters length 2, got %d", len(filteredList))
	}

	eventIDCount, err := store.CountWebhookDeadLetters(ctx, WebhookDeadLetterFilter{
		EventID:     "evt_dead_default",
		MinAttempts: 2,
	})
	if err != nil {
		t.Fatalf("count dead letters by event_id: %v", err)
	}
	if eventIDCount != 1 {
		t.Fatalf("expected dead letter count 1 for event_id filter, got %d", eventIDCount)
	}
}
