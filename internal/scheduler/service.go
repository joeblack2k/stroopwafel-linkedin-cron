package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

var retryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type ChannelPublisherResolver func(channel model.Channel) publisher.Publisher

type Service struct {
	store             *db.Store
	fallbackPublisher publisher.Publisher
	resolvePublisher  ChannelPublisherResolver
	logger            *slog.Logger
	now               func() time.Time
	batchSize         int
}

func NewService(store *db.Store, pub publisher.Publisher, logger *slog.Logger) *Service {
	resolver := func(channel model.Channel) publisher.Publisher {
		return pub
	}
	return &Service{
		store:             store,
		fallbackPublisher: pub,
		resolvePublisher:  resolver,
		logger:            logger,
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

func (s *Service) SetChannelPublisherResolver(resolver ChannelPublisherResolver) {
	if resolver == nil {
		return
	}
	s.resolvePublisher = resolver
}

func (s *Service) RunDue(ctx context.Context) (int, error) {
	now := s.now().UTC()
	posts, err := s.store.ListDuePosts(ctx, now, s.batchSize)
	if err != nil {
		return 0, fmt.Errorf("list due posts: %w", err)
	}

	processed := 0
	for _, post := range posts {
		if err := s.processPost(ctx, post, false); err != nil {
			s.logger.LogAttrs(
				ctx,
				slog.LevelError,
				"failed to process scheduled post",
				slog.String("component", "scheduler"),
				slog.Int64("post_id", post.ID),
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
	return s.processPost(ctx, post, true)
}

func (s *Service) processPost(ctx context.Context, post model.Post, force bool) error {
	channels, err := s.store.ListChannelsForPost(ctx, post.ID)
	if err != nil {
		return fmt.Errorf("list channels for post %d: %w", post.ID, err)
	}
	if len(channels) == 0 {
		return s.attemptLegacyPublish(ctx, post)
	}

	eligibleChannels := make([]model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.Status == model.ChannelStatusDisabled {
			continue
		}
		eligibleChannels = append(eligibleChannels, channel)
	}

	if len(eligibleChannels) == 0 {
		now := s.now().UTC()
		message := "all assigned channels are disabled"
		if updateErr := s.store.SetPostState(ctx, post.ID, model.StatusFailed, nil, post.FailCount+1, &message, nil, now); updateErr != nil {
			return fmt.Errorf("mark post %d failed for disabled channels: %w", post.ID, updateErr)
		}
		s.logger.LogAttrs(
			ctx,
			slog.LevelWarn,
			"post skipped because all channels are disabled",
			slog.String("component", "scheduler"),
			slog.Int64("post_id", post.ID),
			slog.String("status", string(model.StatusFailed)),
			slog.String("error", message),
		)
		return errors.New(message)
	}

	return s.processChannelTargets(ctx, post, eligibleChannels, force)
}

func (s *Service) processChannelTargets(ctx context.Context, post model.Post, channels []model.Channel, force bool) error {
	now := s.now().UTC()
	errorsSeen := make([]string, 0)

	for _, channel := range channels {
		latest, hasLatest, err := s.store.GetLatestPublishAttempt(ctx, post.ID, channel.ID)
		if err != nil {
			return fmt.Errorf("load latest publish attempt for post=%d channel=%d: %w", post.ID, channel.ID, err)
		}
		if !shouldAttemptChannel(force, post, latest, hasLatest, now) {
			continue
		}
		if err := s.attemptChannelPublish(ctx, post, channel, latest, hasLatest, now); err != nil {
			errorsSeen = append(errorsSeen, err.Error())
		}
	}

	if err := s.reconcilePostStateFromChannels(ctx, post, channels, now); err != nil {
		return err
	}
	if len(errorsSeen) > 0 {
		return fmt.Errorf("channel publish errors: %s", strings.Join(errorsSeen, "; "))
	}
	return nil
}

func (s *Service) attemptChannelPublish(ctx context.Context, post model.Post, channel model.Channel, latest model.PublishAttempt, hasLatest bool, now time.Time) error {
	activePublisher := s.resolvePublisher(channel)
	if activePublisher == nil {
		activePublisher = s.fallbackPublisher
	}

	attemptNo := 1
	if hasLatest {
		attemptNo = latest.AttemptNo + 1
	}

	publishResult, err := activePublisher.Publish(ctx, post)
	if err == nil {
		_, insertErr := s.store.InsertPublishAttempt(ctx, db.PublishAttemptInput{
			PostID:      post.ID,
			ChannelID:   channel.ID,
			AttemptNo:   attemptNo,
			AttemptedAt: now,
			Status:      model.PublishAttemptStatusSent,
			ExternalID:  optionalString(publishResult.ExternalID),
		})
		if insertErr != nil {
			return fmt.Errorf("insert sent publish attempt for post=%d channel=%d: %w", post.ID, channel.ID, insertErr)
		}
		s.logger.LogAttrs(
			ctx,
			slog.LevelInfo,
			"channel publish succeeded",
			slog.String("component", "scheduler"),
			slog.Int64("post_id", post.ID),
			slog.Int64("channel_id", channel.ID),
			slog.String("channel_type", string(channel.Type)),
			slog.String("channel_name", channel.DisplayName),
			slog.String("publisher", activePublisher.Mode()),
		)
		return nil
	}

	status := model.PublishAttemptStatusFailed
	nextRetry := (*time.Time)(nil)
	if publisher.IsRetryable(err) {
		candidate, keepRetry := scheduleRetry(now, attemptNo)
		if keepRetry {
			status = model.PublishAttemptStatusRetry
			nextRetry = candidate
		}
	}
	errText := err.Error()
	_, insertErr := s.store.InsertPublishAttempt(ctx, db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   attemptNo,
		AttemptedAt: now,
		Status:      status,
		Error:       &errText,
		RetryAt:     nextRetry,
	})
	if insertErr != nil {
		return fmt.Errorf("insert failed publish attempt for post=%d channel=%d: %w", post.ID, channel.ID, insertErr)
	}

	level := slog.LevelWarn
	if status == model.PublishAttemptStatusFailed {
		level = slog.LevelError
	}
	attrs := []slog.Attr{
		slog.String("component", "scheduler"),
		slog.Int64("post_id", post.ID),
		slog.Int64("channel_id", channel.ID),
		slog.String("channel_type", string(channel.Type)),
		slog.String("channel_name", channel.DisplayName),
		slog.String("publisher", activePublisher.Mode()),
		slog.Int("attempt_no", attemptNo),
		slog.String("status", status),
		slog.String("error", err.Error()),
	}
	if nextRetry != nil {
		attrs = append(attrs, slog.String("next_retry_at", nextRetry.Format(time.RFC3339)))
	}
	s.logger.LogAttrs(ctx, level, "channel publish failed", attrs...)
	return err
}

func (s *Service) reconcilePostStateFromChannels(ctx context.Context, post model.Post, channels []model.Channel, now time.Time) error {
	latestAttempts, err := s.store.ListLatestPublishAttemptsForPost(ctx, post.ID)
	if err != nil {
		return fmt.Errorf("list latest publish attempts for post %d: %w", post.ID, err)
	}

	allSent := true
	anyPending := false
	anyRetry := false
	anyFailed := false
	failCount := 0
	var earliestRetry *time.Time
	var lastError *string

	for _, channel := range channels {
		attempt, exists := latestAttempts[channel.ID]
		if !exists {
			allSent = false
			anyPending = true
			continue
		}

		switch attempt.Status {
		case model.PublishAttemptStatusSent:
			// no-op
		case model.PublishAttemptStatusRetry:
			allSent = false
			anyRetry = true
			failCount++
			if attempt.RetryAt != nil && (earliestRetry == nil || attempt.RetryAt.Before(*earliestRetry)) {
				next := attempt.RetryAt.UTC()
				earliestRetry = &next
			}
			if attempt.Error != nil {
				lastError = copyStringPointer(attempt.Error)
			}
		case model.PublishAttemptStatusFailed:
			allSent = false
			anyFailed = true
			failCount++
			if attempt.Error != nil {
				lastError = copyStringPointer(attempt.Error)
			}
		default:
			allSent = false
			anyPending = true
		}
	}

	if allSent {
		return s.store.SetPostState(ctx, post.ID, model.StatusSent, &now, 0, nil, nil, now)
	}
	if anyRetry || anyPending {
		return s.store.SetPostState(ctx, post.ID, model.StatusScheduled, nil, failCount, lastError, earliestRetry, now)
	}
	if anyFailed {
		return s.store.SetPostState(ctx, post.ID, model.StatusFailed, nil, failCount, lastError, nil, now)
	}
	return nil
}

func (s *Service) attemptLegacyPublish(ctx context.Context, post model.Post) error {
	now := s.now().UTC()
	_, err := s.fallbackPublisher.Publish(ctx, post)
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
			slog.String("publisher", s.fallbackPublisher.Mode()),
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
		slog.String("publisher", s.fallbackPublisher.Mode()),
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

func shouldAttemptChannel(force bool, post model.Post, latest model.PublishAttempt, hasLatest bool, now time.Time) bool {
	if force {
		if hasLatest && latest.Status == model.PublishAttemptStatusSent {
			return false
		}
		return true
	}

	if hasLatest {
		switch latest.Status {
		case model.PublishAttemptStatusSent, model.PublishAttemptStatusFailed:
			return false
		case model.PublishAttemptStatusRetry:
			if latest.RetryAt == nil {
				return false
			}
			return !latest.RetryAt.After(now)
		default:
			return false
		}
	}

	if post.ScheduledAt == nil {
		return false
	}
	return !post.ScheduledAt.After(now)
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

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func copyStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
