package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

var retryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type Service struct {
	store     *db.Store
	publisher publisher.Publisher
	logger    *slog.Logger
	now       func() time.Time
	batchSize int
}

func NewService(store *db.Store, pub publisher.Publisher, logger *slog.Logger) *Service {
	return &Service{
		store:     store,
		publisher: pub,
		logger:    logger,
		now: func() time.Time {
			return time.Now().UTC()
		},
		batchSize: 100,
	}
}

func (s *Service) SetNow(nowFn func() time.Time) {
	s.now = nowFn
}

func (s *Service) SetBatchSize(value int) {
	if value > 0 {
		s.batchSize = value
	}
}

func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now().UTC()
	posts, err := s.store.ListDuePosts(ctx, now, s.batchSize)
	if err != nil {
		return 0, fmt.Errorf("list due posts: %w", err)
	}

	processed := 0
	for _, post := range posts {
		if err := s.attemptPublish(ctx, post); err != nil {
			s.logger.LogAttrs(
				ctx,
				slog.LevelError,
				"failed to process scheduled post",
				slog.String("component", "scheduler"),
				slog.Int64("post_id", post.ID),
				slog.String("publisher", s.publisher.Mode()),
				slog.String("error", err.Error()),
			)
		}
		processed++
	}

	return processed, nil
}

func (s *Service) SendNow(ctx context.Context, id int64) error {
	post, err := s.store.GetPost(ctx, id)
	if err != nil {
		return err
	}
	if post.Status == model.StatusSent {
		return fmt.Errorf("post %d is already sent", id)
	}
	return s.attemptPublish(ctx, post)
}

func (s *Service) attemptPublish(ctx context.Context, post model.Post) error {
	now := s.now().UTC()
	_, err := s.publisher.Publish(ctx, post)
	if err == nil {
		if updateErr := s.store.MarkSent(ctx, post.ID, now); updateErr != nil {
			return fmt.Errorf("mark sent for post %d: %w", post.ID, updateErr)
		}
		s.logger.LogAttrs(
			ctx,
			slog.LevelInfo,
			"post sent",
			slog.String("component", "scheduler"),
			slog.Int64("post_id", post.ID),
			slog.String("publisher", s.publisher.Mode()),
		)
		return nil
	}

	failCount := post.FailCount + 1
	nextRetry, keepScheduled := scheduleRetry(now, failCount)
	status := model.StatusFailed
	if keepScheduled {
		status = model.StatusScheduled
	}
	if !publisher.IsRetryable(err) {
		status = model.StatusFailed
		nextRetry = nil
	}

	if updateErr := s.store.RecordFailure(ctx, post.ID, failCount, err.Error(), status, nextRetry, now); updateErr != nil {
		return fmt.Errorf("record failure for post %d: %w", post.ID, updateErr)
	}

	level := slog.LevelWarn
	if status == model.StatusFailed {
		level = slog.LevelError
	}
	attrs := []slog.Attr{
		slog.String("component", "scheduler"),
		slog.Int64("post_id", post.ID),
		slog.String("publisher", s.publisher.Mode()),
		slog.String("status", string(status)),
		slog.Int("fail_count", failCount),
		slog.String("error", err.Error()),
	}
	if nextRetry != nil {
		attrs = append(attrs, slog.String("next_retry_at", nextRetry.Format(time.RFC3339)))
	}

	s.logger.LogAttrs(ctx, level, "post publish failed", attrs...)
	return err
}

func scheduleRetry(now time.Time, failCount int) (*time.Time, bool) {
	if failCount <= 0 {
		return nil, false
	}
	index := failCount - 1
	if index >= len(retryBackoff) {
		return nil, false
	}
	next := now.UTC().Add(retryBackoff[index])
	return &next, true
}
