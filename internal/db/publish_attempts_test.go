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

func TestPublishAttemptsGlobalListAndCountFilters(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "publish-attempts-global.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := NewStore(database)
	postA, err := store.CreatePost(context.Background(), PostInput{Text: "attempt filter A", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	postB, err := store.CreatePost(context.Background(), PostInput{Text: "attempt filter B", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post B: %v", err)
	}

	channelA, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry A"})
	if err != nil {
		t.Fatalf("create channel A: %v", err)
	}
	channelB, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry B"})
	if err != nil {
		t.Fatalf("create channel B: %v", err)
	}

	base := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	for index, item := range []struct {
		postID      int64
		channelID   int64
		attemptedAt time.Time
		status      string
	}{
		{postID: postA.ID, channelID: channelA.ID, attemptedAt: base, status: model.PublishAttemptStatusSent},
		{postID: postA.ID, channelID: channelA.ID, attemptedAt: base.Add(1 * time.Hour), status: model.PublishAttemptStatusRetry},
		{postID: postA.ID, channelID: channelB.ID, attemptedAt: base.Add(2 * time.Hour), status: model.PublishAttemptStatusFailed},
		{postID: postB.ID, channelID: channelA.ID, attemptedAt: base.Add(3 * time.Hour), status: model.PublishAttemptStatusFailed},
	} {
		attemptNo := 1
		if item.postID == postA.ID && item.channelID == channelA.ID {
			attemptNo = index + 1
		}

		if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
			PostID:      item.postID,
			ChannelID:   item.channelID,
			AttemptNo:   attemptNo,
			AttemptedAt: item.attemptedAt,
			Status:      item.status,
		}); err != nil {
			t.Fatalf("insert attempt %d: %v", index+1, err)
		}
	}

	channelFilter := PublishAttemptFilter{ChannelID: &channelA.ID}
	channelCount, err := store.CountPublishAttempts(context.Background(), channelFilter)
	if err != nil {
		t.Fatalf("count channel-filtered attempts: %v", err)
	}
	if channelCount != 3 {
		t.Fatalf("expected channel-filtered count=3, got %d", channelCount)
	}

	statusRangeFilter := PublishAttemptFilter{
		ChannelID:     &channelA.ID,
		Status:        " retry ",
		AttemptedFrom: ptrTimeAttempt(base.Add(30 * time.Minute)),
		AttemptedTo:   ptrTimeAttempt(base.Add(90 * time.Minute)),
	}
	statusRangeCount, err := store.CountPublishAttempts(context.Background(), statusRangeFilter)
	if err != nil {
		t.Fatalf("count status+range filtered attempts: %v", err)
	}
	if statusRangeCount != 1 {
		t.Fatalf("expected status+range count=1, got %d", statusRangeCount)
	}

	statusRangeList, err := store.ListPublishAttempts(context.Background(), statusRangeFilter, 10, 0)
	if err != nil {
		t.Fatalf("list status+range filtered attempts: %v", err)
	}
	if len(statusRangeList) != 1 {
		t.Fatalf("expected one status+range attempt, got %d", len(statusRangeList))
	}
	if statusRangeList[0].PostID != postA.ID || statusRangeList[0].ChannelID != channelA.ID || statusRangeList[0].AttemptNo != 2 {
		t.Fatalf("unexpected status+range attempt row: %+v", statusRangeList[0])
	}

	rangeOnlyFilter := PublishAttemptFilter{
		AttemptedFrom: ptrTimeAttempt(base.Add(90 * time.Minute)),
		AttemptedTo:   ptrTimeAttempt(base.Add(3 * time.Hour)),
	}
	rangeOnlyCount, err := store.CountPublishAttempts(context.Background(), rangeOnlyFilter)
	if err != nil {
		t.Fatalf("count range-only attempts: %v", err)
	}
	if rangeOnlyCount != 2 {
		t.Fatalf("expected range-only count=2, got %d", rangeOnlyCount)
	}

	rangeOnlyList, err := store.ListPublishAttempts(context.Background(), rangeOnlyFilter, 10, 0)
	if err != nil {
		t.Fatalf("list range-only attempts: %v", err)
	}
	if len(rangeOnlyList) != 2 {
		t.Fatalf("expected two range-only attempts, got %d", len(rangeOnlyList))
	}
	if !rangeOnlyList[0].AttemptedAt.Equal(base.Add(3 * time.Hour)) {
		t.Fatalf("expected first range-only attempt at %s, got %s", base.Add(3*time.Hour), rangeOnlyList[0].AttemptedAt)
	}
	if !rangeOnlyList[1].AttemptedAt.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("expected second range-only attempt at %s, got %s", base.Add(2*time.Hour), rangeOnlyList[1].AttemptedAt)
	}
}

func ptrTimeAttempt(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}
