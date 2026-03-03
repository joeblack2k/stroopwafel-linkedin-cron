package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"linkedin-cron/internal/model"
)

func TestPublishAttemptsDateRangeFilters(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "publish-attempts.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := NewStore(database)
	post, err := store.CreatePost(context.Background(), PostInput{Text: "attempt history", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	channel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	base := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	for index, attemptTime := range []time.Time{base, base.Add(1 * time.Hour), base.Add(2 * time.Hour)} {
		if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
			PostID:      post.ID,
			ChannelID:   channel.ID,
			AttemptNo:   index + 1,
			AttemptedAt: attemptTime,
			Status:      model.PublishAttemptStatusSent,
		}); err != nil {
			t.Fatalf("insert publish attempt %d: %v", index+1, err)
		}
	}

	attemptedFrom := ptrTimeAttempt(base.Add(30 * time.Minute))
	attemptedTo := ptrTimeAttempt(base.Add(90 * time.Minute))
	count, err := store.CountPublishAttemptsForPost(context.Background(), post.ID, nil, "", attemptedFrom, attemptedTo)
	if err != nil {
		t.Fatalf("count filtered attempts: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected filtered count=1, got %d", count)
	}

	attempts, err := store.ListPublishAttemptsForPost(context.Background(), post.ID, nil, "", attemptedFrom, attemptedTo, 10, 0)
	if err != nil {
		t.Fatalf("list filtered attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one filtered attempt, got %d", len(attempts))
	}
	if attempts[0].AttemptNo != 2 {
		t.Fatalf("expected attempt_no=2, got %d", attempts[0].AttemptNo)
	}

	fromOnly := ptrTimeAttempt(base.Add(1 * time.Hour))
	count, err = store.CountPublishAttemptsForPost(context.Background(), post.ID, nil, "", fromOnly, nil)
	if err != nil {
		t.Fatalf("count from-only attempts: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected from-only count=2, got %d", count)
	}
}

func ptrTimeAttempt(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}
