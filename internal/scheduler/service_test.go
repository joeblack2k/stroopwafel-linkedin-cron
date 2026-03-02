package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
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
