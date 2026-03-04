package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	WebhookReplayStatusQueued     = "queued"
	WebhookReplayStatusProcessing = "processing"
	WebhookReplayStatusDelivered  = "delivered"
	WebhookReplayStatusFailed     = "failed"
	WebhookReplayStatusCancelled  = "cancelled"
)

type WebhookReplayInput struct {
	DeliveryID   *int64
	EventID      string
	EventName    string
	TargetURL    string
	Source       string
	Payload      string
	Headers      string
	Status       string
	AttemptCount int
	LastError    *string
	HTTPStatus   *int
	LastAttempt  *time.Time
	NextAttempt  *time.Time
}

type WebhookReplay struct {
	ID           int64
	DeliveryID   *int64
	EventID      string
	EventName    string
	TargetURL    string
	Source       string
	Payload      string
	Headers      string
	Status       string
	AttemptCount int
	LastError    *string
	HTTPStatus   *int
	LastAttempt  *time.Time
	NextAttempt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type WebhookReplayFilter struct {
	Status    string
	TargetURL string
	EventName string
	EventID   string
}

type WebhookDeadLetterFilter struct {
	TargetURL   string
	EventName   string
	EventID     string
	MinAttempts int
}

func (s *Store) InsertWebhookReplay(ctx context.Context, input WebhookReplayInput) (WebhookReplay, error) {
	eventID := strings.TrimSpace(input.EventID)
	eventName := strings.TrimSpace(input.EventName)
	targetURL := strings.TrimSpace(input.TargetURL)
	source := strings.TrimSpace(input.Source)
	payload := strings.TrimSpace(input.Payload)
	headers := strings.TrimSpace(input.Headers)
	status := normalizeWebhookReplayStatus(input.Status)
	if status == "" {
		status = WebhookReplayStatusQueued
	}

	if eventID == "" {
		return WebhookReplay{}, fmt.Errorf("event_id is required")
	}
	if eventName == "" {
		return WebhookReplay{}, fmt.Errorf("event_name is required")
	}
	if targetURL == "" {
		return WebhookReplay{}, fmt.Errorf("target_url is required")
	}
	if source == "" {
		source = "unknown"
	}
	if payload == "" {
		payload = "{}"
	}
	if headers == "" {
		headers = "{}"
	}
	if !isWebhookReplayStatus(status) {
		return WebhookReplay{}, fmt.Errorf("status %q is invalid", status)
	}

	attemptCount := input.AttemptCount
	if attemptCount < 0 {
		attemptCount = 0
	}

	now := time.Now().UTC()

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO webhook_replays(
			delivery_id,
			event_id,
			event_name,
			target_url,
			source,
			payload,
			headers,
			status,
			attempt_count,
			last_error,
			last_http_status,
			last_attempt_at,
			next_attempt_at,
			created_at,
			updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableInt64(input.DeliveryID),
		eventID,
		eventName,
		targetURL,
		source,
		payload,
		headers,
		status,
		attemptCount,
		nullableString(input.LastError),
		nullableInt(input.HTTPStatus),
		nullableTime(input.LastAttempt),
		nullableTime(input.NextAttempt),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("insert webhook replay: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("read webhook replay id: %w", err)
	}

	return s.GetWebhookReplay(ctx, id)
}

func (s *Store) GetWebhookReplay(ctx context.Context, id int64) (WebhookReplay, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT
			id,
			delivery_id,
			event_id,
			event_name,
			target_url,
			source,
			payload,
			headers,
			status,
			attempt_count,
			last_error,
			last_http_status,
			last_attempt_at,
			next_attempt_at,
			created_at,
			updated_at
		 FROM webhook_replays
		 WHERE id = ?`,
		id,
	)

	replay, err := scanWebhookReplay(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return WebhookReplay{}, ErrNotFound
		}
		return WebhookReplay{}, fmt.Errorf("get webhook replay %d: %w", id, err)
	}
	return replay, nil
}

func (s *Store) ListWebhookReplays(ctx context.Context, filter WebhookReplayFilter, limit, offset int) ([]WebhookReplay, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildWebhookReplayQuery(filter, false)
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list webhook replays: %w", err)
	}
	defer rows.Close()

	replays := make([]WebhookReplay, 0)
	for rows.Next() {
		item, scanErr := scanWebhookReplay(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan webhook replay row: %w", scanErr)
		}
		replays = append(replays, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook replay rows: %w", err)
	}
	return replays, nil
}

func (s *Store) CountWebhookReplays(ctx context.Context, filter WebhookReplayFilter) (int, error) {
	query, args := buildWebhookReplayQuery(filter, true)
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count webhook replays: %w", err)
	}
	return total, nil
}

func (s *Store) ListWebhookDeadLetters(ctx context.Context, filter WebhookDeadLetterFilter, limit, offset int) ([]WebhookReplay, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildWebhookDeadLetterQuery(filter, false)
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list webhook dead letters: %w", err)
	}
	defer rows.Close()

	deadLetters := make([]WebhookReplay, 0)
	for rows.Next() {
		item, scanErr := scanWebhookReplay(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan webhook dead letter row: %w", scanErr)
		}
		deadLetters = append(deadLetters, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook dead letter rows: %w", err)
	}
	return deadLetters, nil
}

func (s *Store) CountWebhookDeadLetters(ctx context.Context, filter WebhookDeadLetterFilter) (int, error) {
	query, args := buildWebhookDeadLetterQuery(filter, true)
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count webhook dead letters: %w", err)
	}
	return total, nil
}

func (s *Store) ListWebhookReplaysDue(ctx context.Context, now time.Time, limit int) ([]WebhookReplay, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			id,
			delivery_id,
			event_id,
			event_name,
			target_url,
			source,
			payload,
			headers,
			status,
			attempt_count,
			last_error,
			last_http_status,
			last_attempt_at,
			next_attempt_at,
			created_at,
			updated_at
		 FROM webhook_replays
		 WHERE status IN ('queued', 'failed')
		   AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		 ORDER BY created_at ASC, id ASC
		 LIMIT ?`,
		formatDBTime(now.UTC()),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due webhook replays: %w", err)
	}
	defer rows.Close()

	replays := make([]WebhookReplay, 0)
	for rows.Next() {
		item, scanErr := scanWebhookReplay(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan due webhook replay row: %w", scanErr)
		}
		replays = append(replays, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due webhook replay rows: %w", err)
	}
	return replays, nil
}

func (s *Store) UpdateWebhookReplayAfterAttempt(ctx context.Context, id int64, status string, httpStatus *int, lastError *string, nextAttempt *time.Time) (WebhookReplay, error) {
	normalizedStatus := normalizeWebhookReplayStatus(status)
	if !isWebhookReplayStatus(normalizedStatus) {
		return WebhookReplay{}, fmt.Errorf("status %q is invalid", status)
	}

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE webhook_replays
		 SET status = ?,
		     attempt_count = attempt_count + 1,
		     last_error = ?,
		     last_http_status = ?,
		     last_attempt_at = ?,
		     next_attempt_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		normalizedStatus,
		nullableString(lastError),
		nullableInt(httpStatus),
		formatDBTime(now),
		nullableTime(nextAttempt),
		formatDBTime(now),
		id,
	)
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("update webhook replay after attempt %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("read rows affected for webhook replay %d: %w", id, err)
	}
	if rowsAffected == 0 {
		return WebhookReplay{}, ErrNotFound
	}
	return s.GetWebhookReplay(ctx, id)
}

func (s *Store) UpdateWebhookReplayStatus(ctx context.Context, id int64, status string, lastError *string, nextAttempt *time.Time) (WebhookReplay, error) {
	normalizedStatus := normalizeWebhookReplayStatus(status)
	if !isWebhookReplayStatus(normalizedStatus) {
		return WebhookReplay{}, fmt.Errorf("status %q is invalid", status)
	}

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE webhook_replays
		 SET status = ?,
		     last_error = ?,
		     next_attempt_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		normalizedStatus,
		nullableString(lastError),
		nullableTime(nextAttempt),
		formatDBTime(now),
		id,
	)
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("update webhook replay status %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("read rows affected for webhook replay status %d: %w", id, err)
	}
	if rowsAffected == 0 {
		return WebhookReplay{}, ErrNotFound
	}
	return s.GetWebhookReplay(ctx, id)
}

func buildWebhookReplayQuery(filter WebhookReplayFilter, countOnly bool) (string, []any) {
	selectClause := `SELECT
		id,
		delivery_id,
		event_id,
		event_name,
		target_url,
		source,
		payload,
		headers,
		status,
		attempt_count,
		last_error,
		last_http_status,
		last_attempt_at,
		next_attempt_at,
		created_at,
		updated_at`
	if countOnly {
		selectClause = `SELECT COUNT(1)`
	}

	query := selectClause + `
		 FROM webhook_replays
		 WHERE 1 = 1`
	args := make([]any, 0, 6)

	if normalizedStatus := normalizeWebhookReplayStatus(filter.Status); normalizedStatus != "" {
		query += ` AND status = ?`
		args = append(args, normalizedStatus)
	}

	if trimmedTarget := strings.TrimSpace(filter.TargetURL); trimmedTarget != "" {
		query += ` AND target_url = ?`
		args = append(args, trimmedTarget)
	}

	if trimmedEvent := strings.TrimSpace(filter.EventName); trimmedEvent != "" {
		query += ` AND event_name = ?`
		args = append(args, trimmedEvent)
	}

	if trimmedEventID := strings.TrimSpace(filter.EventID); trimmedEventID != "" {
		query += ` AND event_id = ?`
		args = append(args, trimmedEventID)
	}

	return query, args
}

func buildWebhookDeadLetterQuery(filter WebhookDeadLetterFilter, countOnly bool) (string, []any) {
	selectClause := `SELECT
		id,
		delivery_id,
		event_id,
		event_name,
		target_url,
		source,
		payload,
		headers,
		status,
		attempt_count,
		last_error,
		last_http_status,
		last_attempt_at,
		next_attempt_at,
		created_at,
		updated_at`
	if countOnly {
		selectClause = `SELECT COUNT(1)`
	}

	minAttempts := filter.MinAttempts
	if minAttempts <= 0 {
		minAttempts = 3
	}

	query := selectClause + `
		 FROM webhook_replays
		 WHERE status = ?
		   AND next_attempt_at IS NULL
		   AND attempt_count >= ?`
	args := make([]any, 0, 6)
	args = append(args, WebhookReplayStatusFailed, minAttempts)

	if trimmedTarget := strings.TrimSpace(filter.TargetURL); trimmedTarget != "" {
		query += ` AND target_url = ?`
		args = append(args, trimmedTarget)
	}

	if trimmedEvent := strings.TrimSpace(filter.EventName); trimmedEvent != "" {
		query += ` AND event_name = ?`
		args = append(args, trimmedEvent)
	}

	if trimmedEventID := strings.TrimSpace(filter.EventID); trimmedEventID != "" {
		query += ` AND event_id = ?`
		args = append(args, trimmedEventID)
	}

	return query, args
}

func scanWebhookReplay(scanner scanner) (WebhookReplay, error) {
	var (
		replay         WebhookReplay
		deliveryIDRaw  sql.NullInt64
		lastErrorRaw   sql.NullString
		httpStatusRaw  sql.NullInt64
		lastAttemptRaw sql.NullString
		nextAttemptRaw sql.NullString
		createdAtRaw   string
		updatedAtRaw   string
	)

	if err := scanner.Scan(
		&replay.ID,
		&deliveryIDRaw,
		&replay.EventID,
		&replay.EventName,
		&replay.TargetURL,
		&replay.Source,
		&replay.Payload,
		&replay.Headers,
		&replay.Status,
		&replay.AttemptCount,
		&lastErrorRaw,
		&httpStatusRaw,
		&lastAttemptRaw,
		&nextAttemptRaw,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return WebhookReplay{}, err
	}

	createdAt, err := parseDBTime(createdAtRaw)
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("parse webhook replay created_at: %w", err)
	}
	updatedAt, err := parseDBTime(updatedAtRaw)
	if err != nil {
		return WebhookReplay{}, fmt.Errorf("parse webhook replay updated_at: %w", err)
	}
	replay.CreatedAt = createdAt
	replay.UpdatedAt = updatedAt

	if deliveryIDRaw.Valid {
		value := deliveryIDRaw.Int64
		replay.DeliveryID = &value
	}
	if lastErrorRaw.Valid {
		value := strings.TrimSpace(lastErrorRaw.String)
		if value != "" {
			replay.LastError = &value
		}
	}
	if httpStatusRaw.Valid {
		value := int(httpStatusRaw.Int64)
		replay.HTTPStatus = &value
	}
	if lastAttemptRaw.Valid {
		parsed, parseErr := parseDBTime(lastAttemptRaw.String)
		if parseErr != nil {
			return WebhookReplay{}, fmt.Errorf("parse webhook replay last_attempt_at: %w", parseErr)
		}
		replay.LastAttempt = &parsed
	}
	if nextAttemptRaw.Valid {
		parsed, parseErr := parseDBTime(nextAttemptRaw.String)
		if parseErr != nil {
			return WebhookReplay{}, fmt.Errorf("parse webhook replay next_attempt_at: %w", parseErr)
		}
		replay.NextAttempt = &parsed
	}

	return replay, nil
}

func isWebhookReplayStatus(value string) bool {
	switch normalizeWebhookReplayStatus(value) {
	case WebhookReplayStatusQueued, WebhookReplayStatusProcessing, WebhookReplayStatusDelivered, WebhookReplayStatusFailed, WebhookReplayStatusCancelled:
		return true
	default:
		return false
	}
}

func normalizeWebhookReplayStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return nil
	}
	return *value
}
