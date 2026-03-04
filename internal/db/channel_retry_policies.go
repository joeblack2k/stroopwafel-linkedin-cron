package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"stroopwafel/internal/model"
)

type ChannelRetryPolicyInput struct {
	MaxRetries              int
	BackoffFirstSeconds     int
	BackoffSecondSeconds    int
	BackoffThirdSeconds     int
	RateLimitBackoffSeconds int
	MaxPostsPerDay          *int
}

func (s *Store) UpsertChannelRetryPolicy(ctx context.Context, channelID int64, input ChannelRetryPolicyInput) (model.ChannelRetryPolicy, error) {
	if channelID <= 0 {
		return model.ChannelRetryPolicy{}, fmt.Errorf("channel_id must be positive")
	}
	if _, err := s.GetChannel(ctx, channelID); err != nil {
		return model.ChannelRetryPolicy{}, err
	}

	normalized, err := normalizeChannelRetryPolicyInput(input)
	if err != nil {
		return model.ChannelRetryPolicy{}, err
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO channel_retry_policies(channel_id, max_retries, backoff_first_seconds, backoff_second_seconds, backoff_third_seconds, rate_limit_backoff_seconds, max_posts_per_day, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(channel_id) DO UPDATE SET
		   max_retries = excluded.max_retries,
		   backoff_first_seconds = excluded.backoff_first_seconds,
		   backoff_second_seconds = excluded.backoff_second_seconds,
		   backoff_third_seconds = excluded.backoff_third_seconds,
		   rate_limit_backoff_seconds = excluded.rate_limit_backoff_seconds,
		   max_posts_per_day = excluded.max_posts_per_day,
		   updated_at = excluded.updated_at`,
		channelID,
		normalized.MaxRetries,
		normalized.BackoffFirstSeconds,
		normalized.BackoffSecondSeconds,
		normalized.BackoffThirdSeconds,
		normalized.RateLimitBackoffSeconds,
		nullableInt(normalized.MaxPostsPerDay),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return model.ChannelRetryPolicy{}, fmt.Errorf("upsert channel retry policy for channel %d: %w", channelID, err)
	}

	policy, _, err := s.GetChannelRetryPolicy(ctx, channelID)
	if err != nil {
		return model.ChannelRetryPolicy{}, err
	}
	return policy, nil
}

func (s *Store) GetChannelRetryPolicy(ctx context.Context, channelID int64) (model.ChannelRetryPolicy, bool, error) {
	if channelID <= 0 {
		return model.ChannelRetryPolicy{}, false, fmt.Errorf("channel_id must be positive")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT channel_id, max_retries, backoff_first_seconds, backoff_second_seconds, backoff_third_seconds, rate_limit_backoff_seconds, max_posts_per_day, created_at, updated_at
		 FROM channel_retry_policies
		 WHERE channel_id = ?`,
		channelID,
	)

	policy, err := scanChannelRetryPolicy(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ChannelRetryPolicy{}, false, nil
		}
		return model.ChannelRetryPolicy{}, false, fmt.Errorf("get channel retry policy for channel %d: %w", channelID, err)
	}
	return policy, true, nil
}

func (s *Store) GetEffectiveChannelRetryPolicy(ctx context.Context, channelID int64) (model.ChannelRetryPolicy, error) {
	policy, found, err := s.GetChannelRetryPolicy(ctx, channelID)
	if err != nil {
		return model.ChannelRetryPolicy{}, err
	}
	if !found {
		return model.DefaultChannelRetryPolicy(channelID), nil
	}
	return policy, nil
}

func (s *Store) ListChannelRetryPoliciesByChannelIDs(ctx context.Context, ids []int64) (map[int64]model.ChannelRetryPolicy, error) {
	unique := uniqueIDs(ids)
	if len(unique) == 0 {
		return map[int64]model.ChannelRetryPolicy{}, nil
	}

	placeholders, err := inPlaceholders(len(unique))
	if err != nil {
		return nil, err
	}

	query := `SELECT channel_id, max_retries, backoff_first_seconds, backoff_second_seconds, backoff_third_seconds, rate_limit_backoff_seconds, max_posts_per_day, created_at, updated_at
	 FROM channel_retry_policies
	 WHERE channel_id IN (` + placeholders + `)`

	rows, err := s.db.QueryContext(ctx, query, int64Args(unique)...)
	if err != nil {
		return nil, fmt.Errorf("list channel retry policies by ids: %w", err)
	}
	defer rows.Close()

	policies := make(map[int64]model.ChannelRetryPolicy, len(unique))
	for rows.Next() {
		policy, scanErr := scanChannelRetryPolicy(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan channel retry policy row: %w", scanErr)
		}
		policies[policy.ChannelID] = policy
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel retry policy rows: %w", err)
	}

	return policies, nil
}

func (s *Store) CountSentPublishAttemptsForChannelBetween(ctx context.Context, channelID int64, start, end time.Time) (int, error) {
	if channelID <= 0 {
		return 0, fmt.Errorf("channel_id must be positive")
	}
	if end.Before(start) {
		start, end = end, start
	}

	var total int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM publish_attempts
		 WHERE channel_id = ?
		   AND status = ?
		   AND attempted_at >= ?
		   AND attempted_at < ?`,
		channelID,
		model.PublishAttemptStatusSent,
		formatDBTime(start.UTC()),
		formatDBTime(end.UTC()),
	).Scan(&total); err != nil {
		return 0, fmt.Errorf("count sent publish attempts for channel %d: %w", channelID, err)
	}
	return total, nil
}

func (s *Store) CountPlannedPostsForChannelBetween(ctx context.Context, channelID int64, start, end time.Time, excludePostID int64) (int, error) {
	if channelID <= 0 {
		return 0, fmt.Errorf("channel_id must be positive")
	}
	if end.Before(start) {
		start, end = end, start
	}

	query := `SELECT COUNT(1)
	 FROM posts p
	 INNER JOIN post_channels pc ON pc.post_id = p.id
	 WHERE pc.channel_id = ?
	   AND p.status IN ('scheduled', 'sent')
	   AND p.scheduled_at IS NOT NULL
	   AND p.scheduled_at >= ?
	   AND p.scheduled_at < ?`
	args := []any{channelID, formatDBTime(start.UTC()), formatDBTime(end.UTC())}
	if excludePostID > 0 {
		query += ` AND p.id != ?`
		args = append(args, excludePostID)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count planned posts for channel %d: %w", channelID, err)
	}
	return total, nil
}

func normalizeChannelRetryPolicyInput(input ChannelRetryPolicyInput) (ChannelRetryPolicyInput, error) {
	normalized := ChannelRetryPolicyInput{
		MaxRetries:              input.MaxRetries,
		BackoffFirstSeconds:     input.BackoffFirstSeconds,
		BackoffSecondSeconds:    input.BackoffSecondSeconds,
		BackoffThirdSeconds:     input.BackoffThirdSeconds,
		RateLimitBackoffSeconds: input.RateLimitBackoffSeconds,
		MaxPostsPerDay:          input.MaxPostsPerDay,
	}

	if normalized.MaxRetries < 0 || normalized.MaxRetries > 10 {
		return ChannelRetryPolicyInput{}, fmt.Errorf("max_retries must be between 0 and 10")
	}
	if normalized.BackoffFirstSeconds <= 0 {
		return ChannelRetryPolicyInput{}, fmt.Errorf("backoff_first_seconds must be greater than zero")
	}
	if normalized.BackoffSecondSeconds <= 0 {
		return ChannelRetryPolicyInput{}, fmt.Errorf("backoff_second_seconds must be greater than zero")
	}
	if normalized.BackoffThirdSeconds <= 0 {
		return ChannelRetryPolicyInput{}, fmt.Errorf("backoff_third_seconds must be greater than zero")
	}
	if normalized.RateLimitBackoffSeconds <= 0 {
		return ChannelRetryPolicyInput{}, fmt.Errorf("rate_limit_backoff_seconds must be greater than zero")
	}
	if normalized.MaxPostsPerDay != nil {
		if *normalized.MaxPostsPerDay <= 0 {
			return ChannelRetryPolicyInput{}, fmt.Errorf("max_posts_per_day must be greater than zero")
		}
		value := *normalized.MaxPostsPerDay
		normalized.MaxPostsPerDay = &value
	}

	return normalized, nil
}

func scanChannelRetryPolicy(s scanner) (model.ChannelRetryPolicy, error) {
	var (
		policy         model.ChannelRetryPolicy
		maxPostsPerDay sql.NullInt64
		createdAtRaw   string
		updatedAtRaw   string
	)

	if err := s.Scan(
		&policy.ChannelID,
		&policy.MaxRetries,
		&policy.BackoffFirstSeconds,
		&policy.BackoffSecondSeconds,
		&policy.BackoffThirdSeconds,
		&policy.RateLimitBackoffSeconds,
		&maxPostsPerDay,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return model.ChannelRetryPolicy{}, err
	}

	if maxPostsPerDay.Valid {
		value := int(maxPostsPerDay.Int64)
		policy.MaxPostsPerDay = &value
	}

	createdAt, err := parseDBTime(strings.TrimSpace(createdAtRaw))
	if err != nil {
		return model.ChannelRetryPolicy{}, fmt.Errorf("parse channel retry policy created_at: %w", err)
	}
	updatedAt, err := parseDBTime(strings.TrimSpace(updatedAtRaw))
	if err != nil {
		return model.ChannelRetryPolicy{}, fmt.Errorf("parse channel retry policy updated_at: %w", err)
	}
	policy.CreatedAt = createdAt
	policy.UpdatedAt = updatedAt

	return policy, nil
}
