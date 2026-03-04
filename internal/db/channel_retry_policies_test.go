package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stroopwafel/internal/model"
)

func TestChannelRetryPolicyUpsertAndRead(t *testing.T) {
	t.Parallel()

	store := newChannelRetryPolicyTestStore(t)
	channel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry Policy"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	_, found, err := store.GetChannelRetryPolicy(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get retry policy before upsert: %v", err)
	}
	if found {
		t.Fatal("expected no retry policy before upsert")
	}

	maxPostsPerDay := 3
	updated, err := store.UpsertChannelRetryPolicy(context.Background(), channel.ID, ChannelRetryPolicyInput{
		MaxRetries:              5,
		BackoffFirstSeconds:     120,
		BackoffSecondSeconds:    480,
		BackoffThirdSeconds:     1800,
		RateLimitBackoffSeconds: 2400,
		MaxPostsPerDay:          &maxPostsPerDay,
	})
	if err != nil {
		t.Fatalf("upsert retry policy: %v", err)
	}
	if updated.MaxRetries != 5 {
		t.Fatalf("expected max_retries=5, got %d", updated.MaxRetries)
	}
	if updated.BackoffFirstSeconds != 120 || updated.BackoffSecondSeconds != 480 || updated.BackoffThirdSeconds != 1800 {
		t.Fatalf("unexpected backoff values: %+v", updated)
	}
	if updated.RateLimitBackoffSeconds != 2400 {
		t.Fatalf("expected rate_limit_backoff_seconds=2400, got %d", updated.RateLimitBackoffSeconds)
	}
	if updated.MaxPostsPerDay == nil || *updated.MaxPostsPerDay != 3 {
		t.Fatalf("expected max_posts_per_day=3, got %+v", updated.MaxPostsPerDay)
	}

	loaded, found, err := store.GetChannelRetryPolicy(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get retry policy: %v", err)
	}
	if !found {
		t.Fatal("expected retry policy to be found")
	}
	if loaded.ChannelID != channel.ID {
		t.Fatalf("expected channel_id=%d, got %d", channel.ID, loaded.ChannelID)
	}

	updated, err = store.UpsertChannelRetryPolicy(context.Background(), channel.ID, ChannelRetryPolicyInput{
		MaxRetries:              2,
		BackoffFirstSeconds:     60,
		BackoffSecondSeconds:    300,
		BackoffThirdSeconds:     900,
		RateLimitBackoffSeconds: 1200,
		MaxPostsPerDay:          nil,
	})
	if err != nil {
		t.Fatalf("upsert retry policy clear max_posts_per_day: %v", err)
	}
	if updated.MaxPostsPerDay != nil {
		t.Fatalf("expected max_posts_per_day=nil after clear, got %+v", updated.MaxPostsPerDay)
	}

	policies, err := store.ListChannelRetryPoliciesByChannelIDs(context.Background(), []int64{channel.ID})
	if err != nil {
		t.Fatalf("list retry policies by ids: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected one policy in list, got %d", len(policies))
	}
}

func TestChannelRetryPolicyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	store := newChannelRetryPolicyTestStore(t)
	channel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry Invalid"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	if _, err := store.UpsertChannelRetryPolicy(context.Background(), channel.ID, ChannelRetryPolicyInput{
		MaxRetries:              -1,
		BackoffFirstSeconds:     60,
		BackoffSecondSeconds:    300,
		BackoffThirdSeconds:     900,
		RateLimitBackoffSeconds: 1200,
	}); err == nil {
		t.Fatal("expected validation error for negative max_retries")
	}

	invalidMaxPosts := 0
	if _, err := store.UpsertChannelRetryPolicy(context.Background(), channel.ID, ChannelRetryPolicyInput{
		MaxRetries:              3,
		BackoffFirstSeconds:     60,
		BackoffSecondSeconds:    300,
		BackoffThirdSeconds:     900,
		RateLimitBackoffSeconds: 1200,
		MaxPostsPerDay:          &invalidMaxPosts,
	}); err == nil {
		t.Fatal("expected validation error for max_posts_per_day=0")
	}
}

func TestChannelRetryPolicyCounters(t *testing.T) {
	t.Parallel()

	store := newChannelRetryPolicyTestStore(t)
	channel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry Counters"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	postA, err := store.CreatePost(context.Background(), PostInput{
		Text:        "A",
		Status:      model.StatusScheduled,
		ScheduledAt: ptrRetryPolicyTime(time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	postB, err := store.CreatePost(context.Background(), PostInput{
		Text:        "B",
		Status:      model.StatusSent,
		ScheduledAt: ptrRetryPolicyTime(time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("create post B: %v", err)
	}
	if err := store.ReplacePostChannels(context.Background(), postA.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channel to post A: %v", err)
	}
	if err := store.ReplacePostChannels(context.Background(), postB.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channel to post B: %v", err)
	}

	base := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
		PostID:      postA.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: base,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert sent attempt: %v", err)
	}
	if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
		PostID:      postB.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: base.Add(1 * time.Hour),
		Status:      model.PublishAttemptStatusFailed,
	}); err != nil {
		t.Fatalf("insert failed attempt: %v", err)
	}

	dayStart := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	sentCount, err := store.CountSentPublishAttemptsForChannelBetween(context.Background(), channel.ID, dayStart, dayEnd)
	if err != nil {
		t.Fatalf("count sent attempts by channel/day: %v", err)
	}
	if sentCount != 1 {
		t.Fatalf("expected sent attempts count=1, got %d", sentCount)
	}

	plannedCount, err := store.CountPlannedPostsForChannelBetween(context.Background(), channel.ID, dayStart, dayEnd, 0)
	if err != nil {
		t.Fatalf("count planned posts by channel/day: %v", err)
	}
	if plannedCount != 2 {
		t.Fatalf("expected planned posts count=2, got %d", plannedCount)
	}

	plannedCount, err = store.CountPlannedPostsForChannelBetween(context.Background(), channel.ID, dayStart, dayEnd, postA.ID)
	if err != nil {
		t.Fatalf("count planned posts by channel/day excluding post A: %v", err)
	}
	if plannedCount != 1 {
		t.Fatalf("expected planned posts count=1 when excluding post A, got %d", plannedCount)
	}
}

func newChannelRetryPolicyTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "channel_retry_policies.db")
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

func ptrRetryPolicyTime(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}
