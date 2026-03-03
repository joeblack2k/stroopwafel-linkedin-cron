package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

type stubPublisher struct {
	err error
	ids []int64
}

func (s *stubPublisher) Mode() string     { return "stub" }
func (s *stubPublisher) Configured() bool { return true }
func (s *stubPublisher) Publish(ctx context.Context, post model.Post) (publisher.PublishResult, error) {
	s.ids = append(s.ids, post.ID)
	if s.err != nil {
		return publisher.PublishResult{}, s.err
	}
	return publisher.PublishResult{ExternalID: "stub"}, nil
}

func TestRunDueOnlyProcessesEligibleScheduledPosts(t *testing.T) {
	t.Parallel()

	store, database := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	due := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-5*time.Minute)))
	_ = mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(2*time.Hour)))
	_ = mustCreatePost(t, store, model.StatusDraft, ptrTime(now.Add(-10*time.Minute)))
	retryDue := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(3*time.Hour)))
	retryFuture := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-10*time.Minute)))

	if _, err := database.Exec(
		`UPDATE posts SET next_retry_at = ? WHERE id = ?`,
		now.Add(-2*time.Minute).UTC().Format(time.RFC3339),
		retryDue.ID,
	); err != nil {
		t.Fatalf("set retry due: %v", err)
	}
	if _, err := database.Exec(
		`UPDATE posts SET next_retry_at = ? WHERE id = ?`,
		now.Add(20*time.Minute).UTC().Format(time.RFC3339),
		retryFuture.ID,
	); err != nil {
		t.Fatalf("set retry future: %v", err)
	}

	pub := &stubPublisher{}
	service := NewService(store, pub, testLogger())
	service.SetNow(func() time.Time { return now })

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 2 {
		t.Fatalf("expected 2 processed posts, got %d", processed)
	}

	if len(pub.ids) != 2 {
		t.Fatalf("expected 2 publish attempts, got %d", len(pub.ids))
	}

	duePost, err := store.GetPost(context.Background(), due.ID)
	if err != nil {
		t.Fatalf("load due post: %v", err)
	}
	if duePost.Status != model.StatusSent {
		t.Fatalf("expected due post sent, got %s", duePost.Status)
	}
}

func TestRetryBookkeepingSetsNextRetryAt(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-1*time.Minute)))

	pub := &stubPublisher{err: &publisher.PublishError{Err: errors.New("temporary upstream error"), Retryable: true}}
	service := NewService(store, pub, testLogger())
	service.SetNow(func() time.Time { return now })

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed post, got %d", processed)
	}

	updated, err := store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("get updated post: %v", err)
	}
	if updated.FailCount != 1 {
		t.Fatalf("expected fail_count=1, got %d", updated.FailCount)
	}
	if updated.Status != model.StatusScheduled {
		t.Fatalf("expected status scheduled, got %s", updated.Status)
	}
	if updated.NextRetryAt == nil {
		t.Fatal("expected next_retry_at to be set")
	}
	expected := now.Add(1 * time.Minute)
	if !updated.NextRetryAt.Equal(expected) {
		t.Fatalf("expected next_retry_at=%s, got %s", expected.Format(time.RFC3339), updated.NextRetryAt.Format(time.RFC3339))
	}
}

func TestDryRunPublisherMarksPostSent(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-5*time.Minute)))

	service := NewService(store, publisher.NewDryRunPublisher(testLogger()), testLogger())
	service.SetNow(func() time.Time { return now })

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed post, got %d", processed)
	}

	updated, err := store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if updated.Status != model.StatusSent {
		t.Fatalf("expected sent status, got %s", updated.Status)
	}
	if updated.SentAt == nil {
		t.Fatal("expected sent_at to be set")
	}
}

func newTestStore(t *testing.T) (*db.Store, *sql.DB) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "scheduler.db")
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db.NewStore(database), database
}

func mustCreatePost(t *testing.T, store *db.Store, status model.PostStatus, scheduledAt *time.Time) model.Post {
	t.Helper()
	post, err := store.CreatePost(context.Background(), db.PostInput{
		ScheduledAt: scheduledAt,
		Text:        "test post",
		Status:      status,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	return post
}

func ptrTime(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

type sequenceStubPublisher struct {
	results []error
	calls   int
	ids     []int64
}

func (s *sequenceStubPublisher) Mode() string     { return "sequence" }
func (s *sequenceStubPublisher) Configured() bool { return true }
func (s *sequenceStubPublisher) Publish(ctx context.Context, post model.Post) (publisher.PublishResult, error) {
	s.ids = append(s.ids, post.ID)
	index := s.calls
	s.calls++
	if index < len(s.results) {
		if err := s.results[index]; err != nil {
			return publisher.PublishResult{}, err
		}
	}
	return publisher.PublishResult{ExternalID: "ok"}, nil
}

func TestRunDueProcessesChannelRetriesIndependently(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-5*time.Minute)))
	channelA := mustCreateChannel(t, store, "Channel A")
	channelB := mustCreateChannel(t, store, "Channel B")
	if err := store.ReplacePostChannels(context.Background(), post.ID, []int64{channelA.ID, channelB.ID}); err != nil {
		t.Fatalf("replace post channels: %v", err)
	}

	channelAPublisher := &stubPublisher{}
	channelBPublisher := &sequenceStubPublisher{results: []error{
		&publisher.PublishError{Err: errors.New("temporary outage"), Retryable: true},
		nil,
	}}

	service := NewService(store, publisher.NewDryRunPublisher(testLogger()), testLogger())
	service.SetChannelPublisherResolver(func(channel model.Channel) publisher.Publisher {
		switch channel.ID {
		case channelA.ID:
			return channelAPublisher
		case channelB.ID:
			return channelBPublisher
		default:
			return nil
		}
	})

	service.SetNow(func() time.Time { return now })
	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due (first): %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed post on first run, got %d", processed)
	}

	updated, err := store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("get post after first run: %v", err)
	}
	if updated.Status != model.StatusScheduled {
		t.Fatalf("expected scheduled status after partial channel failure, got %s", updated.Status)
	}
	if updated.NextRetryAt == nil {
		t.Fatal("expected next_retry_at after retryable channel failure")
	}
	expectedRetry := now.Add(1 * time.Minute)
	if !updated.NextRetryAt.Equal(expectedRetry) {
		t.Fatalf("expected next_retry_at=%s, got %s", expectedRetry.Format(time.RFC3339), updated.NextRetryAt.Format(time.RFC3339))
	}

	latestA, ok, err := store.GetLatestPublishAttempt(context.Background(), post.ID, channelA.ID)
	if err != nil {
		t.Fatalf("latest attempt channel A: %v", err)
	}
	if !ok || latestA.Status != model.PublishAttemptStatusSent {
		t.Fatalf("expected sent attempt for channel A, got exists=%v status=%s", ok, latestA.Status)
	}

	latestB, ok, err := store.GetLatestPublishAttempt(context.Background(), post.ID, channelB.ID)
	if err != nil {
		t.Fatalf("latest attempt channel B: %v", err)
	}
	if !ok || latestB.Status != model.PublishAttemptStatusRetry {
		t.Fatalf("expected retry attempt for channel B, got exists=%v status=%s", ok, latestB.Status)
	}

	service.SetNow(func() time.Time { return now.Add(30 * time.Second) })
	processed, err = service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due (before retry window): %v", err)
	}
	if processed != 0 {
		t.Fatalf("expected zero processed posts before retry window, got %d", processed)
	}

	service.SetNow(func() time.Time { return now.Add(1 * time.Minute) })
	processed, err = service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due (retry window): %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed post at retry window, got %d", processed)
	}

	finalPost, err := store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("get post after retry run: %v", err)
	}
	if finalPost.Status != model.StatusSent {
		t.Fatalf("expected sent status after channel retries succeed, got %s", finalPost.Status)
	}
	if finalPost.SentAt == nil {
		t.Fatal("expected sent_at after all channels succeeded")
	}
	if finalPost.NextRetryAt != nil {
		t.Fatalf("expected cleared next_retry_at after success, got %s", finalPost.NextRetryAt.Format(time.RFC3339))
	}

	latestB, ok, err = store.GetLatestPublishAttempt(context.Background(), post.ID, channelB.ID)
	if err != nil {
		t.Fatalf("latest attempt channel B after retry: %v", err)
	}
	if !ok {
		t.Fatal("expected latest attempt for channel B")
	}
	if latestB.Status != model.PublishAttemptStatusSent {
		t.Fatalf("expected sent status for channel B after retry, got %s", latestB.Status)
	}
	if latestB.AttemptNo != 2 {
		t.Fatalf("expected channel B attempt_no=2, got %d", latestB.AttemptNo)
	}
}

func mustCreateChannel(t *testing.T, store *db.Store, name string) model.Channel {
	t.Helper()
	channel, err := store.CreateChannel(context.Background(), db.ChannelInput{
		Type:        model.ChannelTypeDryRun,
		DisplayName: name,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func TestRunDueFailsWhenAllAssignedChannelsDisabled(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-2*time.Minute)))
	channel := mustCreateChannel(t, store, "Disabled channel")
	if _, err := store.SetChannelStatus(context.Background(), channel.ID, model.ChannelStatusDisabled, nil); err != nil {
		t.Fatalf("disable channel: %v", err)
	}
	if err := store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("replace post channels: %v", err)
	}

	service := NewService(store, publisher.NewDryRunPublisher(testLogger()), testLogger())
	service.SetNow(func() time.Time { return now })

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected one processed post, got %d", processed)
	}

	updated, err := store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if updated.Status != model.StatusFailed {
		t.Fatalf("expected failed status, got %s", updated.Status)
	}
	if updated.LastError == nil || !strings.Contains(*updated.LastError, "disabled") {
		t.Fatalf("expected disabled-channel error, got %v", updated.LastError)
	}

	_, exists, err := store.GetLatestPublishAttempt(context.Background(), post.ID, channel.ID)
	if err != nil {
		t.Fatalf("get latest publish attempt: %v", err)
	}
	if exists {
		t.Fatal("expected no publish attempts for disabled channels")
	}
}

func TestRunDueEmitsLifecycleEvents(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-2*time.Minute)))
	channel := mustCreateChannel(t, store, "Lifecycle Channel")
	if err := store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("replace post channels: %v", err)
	}

	pub := &stubPublisher{}
	service := NewService(store, publisher.NewDryRunPublisher(testLogger()), testLogger())
	service.SetNow(func() time.Time { return now })
	service.SetChannelPublisherResolver(func(ch model.Channel) publisher.Publisher {
		if ch.ID == channel.ID {
			return pub
		}
		return nil
	})

	var (
		mu         sync.Mutex
		eventNames []string
	)
	service.SetEventNotifier(func(ctx context.Context, eventName string, payload map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		eventNames = append(eventNames, eventName)
	})

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected processed=1, got %d", processed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(eventNames) < 2 {
		t.Fatalf("expected at least 2 lifecycle events, got %d", len(eventNames))
	}

	containsAttempt := false
	containsPostState := false
	for _, eventName := range eventNames {
		if eventName == "publish.attempt.created" {
			containsAttempt = true
		}
		if eventName == "post.state.changed" {
			containsPostState = true
		}
	}
	if !containsAttempt {
		t.Fatalf("expected publish.attempt.created event, got %+v", eventNames)
	}
	if !containsPostState {
		t.Fatalf("expected post.state.changed event, got %+v", eventNames)
	}
}

func TestRunDueNotifierCanForwardToWebhookEndpoint(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	post := mustCreatePost(t, store, model.StatusScheduled, ptrTime(now.Add(-2*time.Minute)))
	channel := mustCreateChannel(t, store, "Webhook Channel")
	if err := store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("replace post channels: %v", err)
	}

	pub := &stubPublisher{}
	service := NewService(store, publisher.NewDryRunPublisher(testLogger()), testLogger())
	service.SetNow(func() time.Time { return now })
	service.SetChannelPublisherResolver(func(ch model.Channel) publisher.Publisher {
		if ch.ID == channel.ID {
			return pub
		}
		return nil
	})

	var (
		mu       sync.Mutex
		received []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			mu.Lock()
			received = append(received, payload)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service.SetEventNotifier(func(ctx context.Context, eventName string, payload map[string]any) {
		envelope := map[string]any{"event": eventName, "payload": payload}
		body, _ := json.Marshal(envelope)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		_, _ = http.DefaultClient.Do(req)
	})

	processed, err := service.RunDue(context.Background())
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected processed=1, got %d", processed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatalf("expected forwarded webhook payloads")
	}
}
