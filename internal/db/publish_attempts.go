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
	PostID        int64
	ChannelID     int64
	AttemptNo     int
	AttemptedAt   time.Time
	Status        string
	Error         *string
	ErrorCategory *string
	RetryAt       *time.Time
	ExternalID    *string
	Permalink     *string
	ScreenshotURL *string
}

type PublishAttemptFilter struct {
	PostID        *int64
	ChannelID     *int64
	Status        string
	AttemptedFrom *time.Time
	AttemptedTo   *time.Time
}

func (s *Store) InsertPublishAttempt(ctx context.Context, input PublishAttemptInput) (model.PublishAttempt, error) {
	if input.AttemptNo <= 0 {
		return model.PublishAttempt{}, fmt.Errorf("attempt_no must be positive")
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO publish_attempts(post_id, channel_id, attempt_no, attempted_at, status, error, error_category, retry_at, external_id, permalink, screenshot_url)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.PostID,
		input.ChannelID,
		input.AttemptNo,
		formatDBTime(input.AttemptedAt.UTC()),
		input.Status,
		nullableString(input.Error),
		nullableString(input.ErrorCategory),
		nullableTime(input.RetryAt),
		nullableString(input.ExternalID),
		nullableString(input.Permalink),
		nullableString(input.ScreenshotURL),
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
		`SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, error_category, retry_at, external_id, permalink, screenshot_url
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

func (s *Store) SetPublishAttemptScreenshot(ctx context.Context, id int64, screenshotURL string) (model.PublishAttempt, error) {
	trimmed := strings.TrimSpace(screenshotURL)
	if trimmed != "" {
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return model.PublishAttempt{}, fmt.Errorf("screenshot_url must be absolute http(s) URL")
		}
	}

	var screenshotValue any
	if trimmed != "" {
		screenshotValue = trimmed
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE publish_attempts
		 SET screenshot_url = ?
		 WHERE id = ?`,
		screenshotValue,
		id,
	)
	if err != nil {
		return model.PublishAttempt{}, fmt.Errorf("set publish attempt screenshot %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.PublishAttempt{}, fmt.Errorf("read rows affected for screenshot update %d: %w", id, err)
	}
	if affected == 0 {
		return model.PublishAttempt{}, ErrNotFound
	}
	return s.GetPublishAttempt(ctx, id)
}

func (s *Store) GetLatestPublishAttempt(ctx context.Context, postID, channelID int64) (model.PublishAttempt, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, error_category, retry_at, external_id, permalink, screenshot_url
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
		`SELECT pa.id, pa.post_id, pa.channel_id, pa.attempt_no, pa.attempted_at, pa.status, pa.error, pa.error_category, pa.retry_at, pa.external_id, pa.permalink, pa.screenshot_url
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
		attempt, scanErr := scanPublishAttempt(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan latest publish attempt row: %w", scanErr)
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
		id            int64
		postID        int64
		channelID     int64
		attemptNo     int
		attemptedAt   string
		status        string
		errorText     sql.NullString
		errorCategory sql.NullString
		retryAt       sql.NullString
		externalID    sql.NullString
		permalink     sql.NullString
		screenshotURL sql.NullString
	)
	if err := s.Scan(&id, &postID, &channelID, &attemptNo, &attemptedAt, &status, &errorText, &errorCategory, &retryAt, &externalID, &permalink, &screenshotURL); err != nil {
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
	if errorCategory.Valid {
		value := errorCategory.String
		attempt.ErrorCategory = &value
	}
	if retryAt.Valid {
		value, parseErr := parseDBTime(retryAt.String)
		if parseErr != nil {
			return model.PublishAttempt{}, fmt.Errorf("parse retry_at: %w", parseErr)
		}
		attempt.RetryAt = &value
	}
	if externalID.Valid {
		value := externalID.String
		attempt.ExternalID = &value
	}
	if permalink.Valid {
		value := permalink.String
		attempt.Permalink = &value
	}
	if screenshotURL.Valid {
		value := screenshotURL.String
		attempt.ScreenshotURL = &value
	}
	return attempt, nil
}

func (s *Store) ListPublishAttempts(ctx context.Context, filter PublishAttemptFilter, limit, offset int) ([]model.PublishAttempt, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildPublishAttemptsQuery(filter, false)
	query += ` ORDER BY attempted_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list publish attempts: %w", err)
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

func (s *Store) CountPublishAttempts(ctx context.Context, filter PublishAttemptFilter) (int, error) {
	query, args := buildPublishAttemptsQuery(filter, true)

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count publish attempts: %w", err)
	}

	return count, nil
}

func buildPublishAttemptsQuery(filter PublishAttemptFilter, countOnly bool) (string, []any) {
	query := `SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, error_category, retry_at, external_id, permalink, screenshot_url
	 FROM publish_attempts
	 WHERE 1 = 1`
	if countOnly {
		query = `SELECT COUNT(1)
	 FROM publish_attempts
	 WHERE 1 = 1`
	}

	args := make([]any, 0, 5)

	if filter.PostID != nil {
		query += ` AND post_id = ?`
		args = append(args, *filter.PostID)
	}
	if filter.ChannelID != nil {
		query += ` AND channel_id = ?`
		args = append(args, *filter.ChannelID)
	}

	trimmedStatus := strings.TrimSpace(filter.Status)
	if trimmedStatus != "" {
		query += ` AND status = ?`
		args = append(args, trimmedStatus)
	}
	if filter.AttemptedFrom != nil {
		query += ` AND attempted_at >= ?`
		args = append(args, formatDBTime(filter.AttemptedFrom.UTC()))
	}
	if filter.AttemptedTo != nil {
		query += ` AND attempted_at <= ?`
		args = append(args, formatDBTime(filter.AttemptedTo.UTC()))
	}

	return query, args
}

func (s *Store) ListPublishAttemptsForPost(ctx context.Context, postID int64, channelID *int64, status string, attemptedFrom, attemptedTo *time.Time, limit, offset int) ([]model.PublishAttempt, error) {
	filter := PublishAttemptFilter{PostID: &postID, Status: status, AttemptedFrom: attemptedFrom, AttemptedTo: attemptedTo}
	if channelID != nil && *channelID > 0 {
		filter.ChannelID = channelID
	}

	attempts, err := s.ListPublishAttempts(ctx, filter, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list publish attempts for post %d: %w", postID, err)
	}

	return attempts, nil
}

func (s *Store) CountPublishAttemptsForPost(ctx context.Context, postID int64, channelID *int64, status string, attemptedFrom, attemptedTo *time.Time) (int, error) {
	filter := PublishAttemptFilter{PostID: &postID, Status: status, AttemptedFrom: attemptedFrom, AttemptedTo: attemptedTo}
	if channelID != nil && *channelID > 0 {
		filter.ChannelID = channelID
	}

	count, err := s.CountPublishAttempts(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count publish attempts for post %d: %w", postID, err)
	}

	return count, nil
}

func (s *Store) ListPublishAttemptsByRange(ctx context.Context, start, end time.Time) ([]model.PublishAttempt, error) {
	if end.Before(start) {
		start, end = end, start
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, post_id, channel_id, attempt_no, attempted_at, status, error, error_category, retry_at, external_id, permalink, screenshot_url
		 FROM publish_attempts
		 WHERE attempted_at >= ?
		   AND attempted_at < ?
		 ORDER BY attempted_at DESC, id DESC`,
		formatDBTime(start.UTC()),
		formatDBTime(end.UTC()),
	)
	if err != nil {
		return nil, fmt.Errorf("list publish attempts by range: %w", err)
	}
	defer rows.Close()

	attempts := make([]model.PublishAttempt, 0)
	for rows.Next() {
		attempt, scanErr := scanPublishAttempt(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan publish attempt by range row: %w", scanErr)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publish attempts by range rows: %w", err)
	}
	return attempts, nil
}
