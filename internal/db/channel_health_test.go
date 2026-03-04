package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stroopwafel/internal/model"
)

func TestListChannelHealthSummaries(t *testing.T) {
	t.Parallel()

	store := newChannelHealthTestStore(t)
	ctx := context.Background()

	post, err := store.CreatePost(ctx, PostInput{Text: "health-check", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	configuredChannel, err := store.CreateChannel(ctx, ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Configured",
		LinkedInAccessToken: ptrChannelHealthString("token-a"),
		LinkedInAuthorURN:   ptrChannelHealthString("urn:li:organization:123"),
	})
	if err != nil {
		t.Fatalf("create configured channel: %v", err)
	}

	unconfiguredChannel, err := store.CreateChannel(ctx, ChannelInput{
		Type:              model.ChannelTypeLinkedIn,
		DisplayName:       "LinkedIn Missing Token",
		LinkedInAuthorURN: ptrChannelHealthString("urn:li:organization:456"),
	})
	if err != nil {
		t.Fatalf("create unconfigured channel: %v", err)
	}

	noAttemptsChannel, err := store.CreateChannel(ctx, ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry No Attempts"})
	if err != nil {
		t.Fatalf("create no-attempts channel: %v", err)
	}

	base := time.Date(2026, 3, 3, 8, 0, 0, 0, time.UTC)
	configuredSentAt := base.Add(15 * time.Minute)
	configuredRetryAt := base.Add(45 * time.Minute)
	configuredFailedAt := base.Add(90 * time.Minute)

	for attemptNo, item := range []struct {
		status      string
		attemptedAt time.Time
	}{
		{status: model.PublishAttemptStatusSent, attemptedAt: configuredSentAt},
		{status: model.PublishAttemptStatusRetry, attemptedAt: configuredRetryAt},
		{status: model.PublishAttemptStatusFailed, attemptedAt: configuredFailedAt},
	} {
		if _, err := store.InsertPublishAttempt(ctx, PublishAttemptInput{
			PostID:      post.ID,
			ChannelID:   configuredChannel.ID,
			AttemptNo:   attemptNo + 1,
			AttemptedAt: item.attemptedAt,
			Status:      item.status,
		}); err != nil {
			t.Fatalf("insert configured channel attempt %d: %v", attemptNo+1, err)
		}
	}

	unconfiguredFailedAt := base.Add(2 * time.Hour)
	unconfiguredSentAt := base.Add(3 * time.Hour)
	if _, err := store.InsertPublishAttempt(ctx, PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   unconfiguredChannel.ID,
		AttemptNo:   1,
		AttemptedAt: unconfiguredFailedAt,
		Status:      model.PublishAttemptStatusFailed,
	}); err != nil {
		t.Fatalf("insert unconfigured failed attempt: %v", err)
	}
	if _, err := store.InsertPublishAttempt(ctx, PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   unconfiguredChannel.ID,
		AttemptNo:   2,
		AttemptedAt: unconfiguredSentAt,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert unconfigured sent attempt: %v", err)
	}

	summaries, err := store.ListChannelHealthSummaries(ctx)
	if err != nil {
		t.Fatalf("list channel health summaries: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	summaryByID := make(map[int64]ChannelHealthSummary, len(summaries))
	for _, summary := range summaries {
		summaryByID[summary.ChannelID] = summary
	}

	configuredSummary, ok := summaryByID[configuredChannel.ID]
	if !ok {
		t.Fatalf("missing summary for configured channel id=%d", configuredChannel.ID)
	}
	if !configuredSummary.Configured {
		t.Fatal("expected configured channel summary to be configured=true")
	}
	if configuredSummary.SentCount != 1 || configuredSummary.FailedCount != 1 || configuredSummary.RetryCount != 1 {
		t.Fatalf("unexpected configured channel counters: %+v", configuredSummary)
	}
	assertChannelHealthTime(t, configuredSummary.LastAttemptAt, configuredFailedAt, "configured last_attempt_at")
	assertChannelHealthTime(t, configuredSummary.LastSuccessAt, configuredSentAt, "configured last_success_at")

	unconfiguredSummary, ok := summaryByID[unconfiguredChannel.ID]
	if !ok {
		t.Fatalf("missing summary for unconfigured channel id=%d", unconfiguredChannel.ID)
	}
	if unconfiguredSummary.Configured {
		t.Fatal("expected unconfigured channel summary to be configured=false")
	}
	if unconfiguredSummary.SentCount != 1 || unconfiguredSummary.FailedCount != 1 || unconfiguredSummary.RetryCount != 0 {
		t.Fatalf("unexpected unconfigured channel counters: %+v", unconfiguredSummary)
	}
	assertChannelHealthTime(t, unconfiguredSummary.LastAttemptAt, unconfiguredSentAt, "unconfigured last_attempt_at")
	assertChannelHealthTime(t, unconfiguredSummary.LastSuccessAt, unconfiguredSentAt, "unconfigured last_success_at")

	noAttemptsSummary, ok := summaryByID[noAttemptsChannel.ID]
	if !ok {
		t.Fatalf("missing summary for no-attempts channel id=%d", noAttemptsChannel.ID)
	}
	if !noAttemptsSummary.Configured {
		t.Fatal("expected dry-run channel to be configured=true")
	}
	if noAttemptsSummary.SentCount != 0 || noAttemptsSummary.FailedCount != 0 || noAttemptsSummary.RetryCount != 0 {
		t.Fatalf("unexpected no-attempt counters: %+v", noAttemptsSummary)
	}
	if noAttemptsSummary.LastAttemptAt != nil {
		t.Fatalf("expected no-attempts last_attempt_at=nil, got %v", noAttemptsSummary.LastAttemptAt)
	}
	if noAttemptsSummary.LastSuccessAt != nil {
		t.Fatalf("expected no-attempts last_success_at=nil, got %v", noAttemptsSummary.LastSuccessAt)
	}
}

func newChannelHealthTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "channel_health.db")
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

func ptrChannelHealthString(value string) *string {
	return &value
}

func assertChannelHealthTime(t *testing.T, actual *time.Time, expected time.Time, label string) {
	t.Helper()
	if actual == nil {
		t.Fatalf("expected %s to be set", label)
	}
	if !actual.Equal(expected.UTC()) {
		t.Fatalf("expected %s=%s, got %s", label, expected.UTC(), actual.UTC())
	}
}
