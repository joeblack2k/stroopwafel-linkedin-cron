package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"linkedin-cron/internal/model"
)

type PublishAttemptInput struct {
	PostID      int64
	ChannelID   int64
	AttemptNo   int
	AttemptedAt time.Time
	Status      string
	Error       *string
	RetryAt     *time.Time
	ExternalID  *string
}

func (s *Store) InsertPublishAttempt(ctx context.Context, input PublishAttemptInput) (model.PublishAttempt, error) {
	if input.AttemptNo <= 0 {
		return model.PublishAttempt{}, fmt.Errorf("attempt_no must be positive")
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO publish_attempts(post_id, channel_id, attempt_no, attempted_at, status, error, retry_at, external_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		input.PostID,
		input.ChannelID,
		input.AttemptNo,
		formatDBTime(input.AttemptedAt.UTC()),
		input.Status,
		nullableString(input.Error),
		nullableTime(input.RetryAt),
		nullableString(input.ExternalID),
	)
	if err != nil {
		return model.PublishAttempt{}, fmt.Errorf("insert publish attempt: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.PublishAttempt{}, fmt.Errorf("read publish attempt id: %w", err)
	}
	return s.GetPublishAttempt(ctx, id)
}

func (s *Store) GetPublishAttempt(ctx context.Context, id int64) (model.PublishAttempt, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, retry_at, external_id
		 FROM publish_attempts
		 WHERE id = ?`,
		id,
	)
	attempt, err := scanPublishAttempt(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PublishAttempt{}, ErrNotFound
		}
		return model.PublishAttempt{}, fmt.Errorf("get publish attempt %d: %w", id, err)
	}
	return attempt, nil
}

func (s *Store) GetLatestPublishAttempt(ctx context.Context, postID, channelID int64) (model.PublishAttempt, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, retry_at, external_id
		 FROM publish_attempts
		 WHERE post_id = ?
		   AND channel_id = ?
		 ORDER BY attempt_no DESC
		 LIMIT 1`,
		postID,
		channelID,
	)
	attempt, err := scanPublishAttempt(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PublishAttempt{}, false, nil
		}
		return model.PublishAttempt{}, false, fmt.Errorf("get latest publish attempt for post=%d channel=%d: %w", postID, channelID, err)
	}
	return attempt, true, nil
}

func (s *Store) ListLatestPublishAttemptsForPost(ctx context.Context, postID int64) (map[int64]model.PublishAttempt, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT pa.id, pa.post_id, pa.channel_id, pa.attempt_no, pa.attempted_at, pa.status, pa.error, pa.retry_at, pa.external_id
		 FROM publish_attempts pa
		 INNER JOIN (
		   SELECT channel_id, MAX(attempt_no) AS max_attempt_no
		   FROM publish_attempts
		   WHERE post_id = ?
		   GROUP BY channel_id
		 ) latest ON latest.channel_id = pa.channel_id AND latest.max_attempt_no = pa.attempt_no
		 WHERE pa.post_id = ?`,
		postID,
		postID,
	)
	if err != nil {
		return nil, fmt.Errorf("list latest publish attempts for post %d: %w", postID, err)
	}
	defer rows.Close()

	attempts := make(map[int64]model.PublishAttempt)
	for rows.Next() {
		attempt, err := scanPublishAttempt(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest publish attempt row: %w", err)
		}
		attempts[attempt.ChannelID] = attempt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest publish attempts rows: %w", err)
	}

	return attempts, nil
}

func (s *Store) SetPostState(ctx context.Context, id int64, status model.PostStatus, sentAt *time.Time, failCount int, lastError *string, nextRetryAt *time.Time, updatedAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE posts
		 SET status = ?, sent_at = ?, fail_count = ?, last_error = ?, next_retry_at = ?, updated_at = ?
		 WHERE id = ?`,
		string(status),
		nullableTime(sentAt),
		failCount,
		nullableString(lastError),
		nullableTime(nextRetryAt),
		formatDBTime(updatedAt.UTC()),
		id,
	)
	if err != nil {
		return fmt.Errorf("set post state for %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for post state %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPublishAttempt(s scanner) (model.PublishAttempt, error) {
	var (
		id          int64
		postID      int64
		channelID   int64
		attemptNo   int
		attemptedAt string
		status      string
		errorText   sql.NullString
		retryAt     sql.NullString
		externalID  sql.NullString
	)
	if err := s.Scan(&id, &postID, &channelID, &attemptNo, &attemptedAt, &status, &errorText, &retryAt, &externalID); err != nil {
		return model.PublishAttempt{}, err
	}

	attemptTime, err := parseDBTime(attemptedAt)
	if err != nil {
		return model.PublishAttempt{}, fmt.Errorf("parse attempted_at: %w", err)
	}

	attempt := model.PublishAttempt{
		ID:          id,
		PostID:      postID,
		ChannelID:   channelID,
		AttemptNo:   attemptNo,
		AttemptedAt: attemptTime,
		Status:      status,
	}
	if errorText.Valid {
		value := errorText.String
		attempt.Error = &value
	}
	if retryAt.Valid {
		value, err := parseDBTime(retryAt.String)
		if err != nil {
			return model.PublishAttempt{}, fmt.Errorf("parse retry_at: %w", err)
		}
		attempt.RetryAt = &value
	}
	if externalID.Valid {
		value := externalID.String
		attempt.ExternalID = &value
	}
	return attempt, nil
}

func (s *Store) ListPublishAttemptsForPost(ctx context.Context, postID int64, channelID *int64, status string, limit int) ([]model.PublishAttempt, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}

	query := `SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, retry_at, external_id
	 FROM publish_attempts
	 WHERE post_id = ?`
	args := []any{postID}

	if channelID != nil && *channelID > 0 {
		query += ` AND channel_id = ?`
		args = append(args, *channelID)
	}
	trimmedStatus := strings.TrimSpace(status)
	if trimmedStatus != "" {
		query += ` AND status = ?`
		args = append(args, trimmedStatus)
	}

	query += ` ORDER BY attempted_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list publish attempts for post %d: %w", postID, err)
	}
	defer rows.Close()

	attempts := make([]model.PublishAttempt, 0)
	for rows.Next() {
		attempt, scanErr := scanPublishAttempt(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan publish attempt row: %w", scanErr)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publish attempts rows: %w", err)
	}

	return attempts, nil
}
