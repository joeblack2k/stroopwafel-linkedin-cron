package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type APIIdempotencyInput struct {
	AuthScope      string
	IdempotencyKey string
	Method         string
	Path           string
	RequestHash    string
}

type APIIdempotencyRecord struct {
	AuthScope      string
	IdempotencyKey string
	Method         string
	Path           string
	RequestHash    string
	StatusCode     int
	ResponseBody   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *Store) ReserveAPIIdempotency(ctx context.Context, input APIIdempotencyInput) (APIIdempotencyRecord, bool, error) {
	authScope := strings.TrimSpace(input.AuthScope)
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	method := strings.TrimSpace(input.Method)
	path := strings.TrimSpace(input.Path)
	requestHash := strings.TrimSpace(input.RequestHash)

	if authScope == "" || idempotencyKey == "" || method == "" || path == "" || requestHash == "" {
		return APIIdempotencyRecord{}, false, fmt.Errorf("idempotency scope, key, method, path, and request hash are required")
	}

	now := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO api_idempotency(auth_scope, idempotency_key, method, path, request_hash, status_code, response_body, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, 0, '', ?, ?)`,
		authScope,
		idempotencyKey,
		method,
		path,
		requestHash,
		formatDBTime(now),
		formatDBTime(now),
	)
	if err == nil {
		return APIIdempotencyRecord{
			AuthScope:      authScope,
			IdempotencyKey: idempotencyKey,
			Method:         method,
			Path:           path,
			RequestHash:    requestHash,
			StatusCode:     0,
			ResponseBody:   "",
			CreatedAt:      now,
			UpdatedAt:      now,
		}, true, nil
	}

	if !isUniqueConstraintError(err) {
		return APIIdempotencyRecord{}, false, fmt.Errorf("insert idempotency reservation: %w", err)
	}

	existing, getErr := s.GetAPIIdempotency(ctx, authScope, idempotencyKey)
	if getErr != nil {
		return APIIdempotencyRecord{}, false, getErr
	}

	return existing, false, nil
}

func (s *Store) GetAPIIdempotency(ctx context.Context, authScope, idempotencyKey string) (APIIdempotencyRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT auth_scope, idempotency_key, method, path, request_hash, status_code, response_body, created_at, updated_at
		 FROM api_idempotency
		 WHERE auth_scope = ?
		   AND idempotency_key = ?`,
		strings.TrimSpace(authScope),
		strings.TrimSpace(idempotencyKey),
	)

	var (
		record               APIIdempotencyRecord
		createdAtRaw         string
		updatedAtRaw         string
		responseBodyNullable sql.NullString
	)
	if err := row.Scan(
		&record.AuthScope,
		&record.IdempotencyKey,
		&record.Method,
		&record.Path,
		&record.RequestHash,
		&record.StatusCode,
		&responseBodyNullable,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIIdempotencyRecord{}, ErrNotFound
		}
		return APIIdempotencyRecord{}, fmt.Errorf("get api idempotency record: %w", err)
	}

	createdAt, err := parseDBTime(createdAtRaw)
	if err != nil {
		return APIIdempotencyRecord{}, fmt.Errorf("parse idempotency created_at: %w", err)
	}
	updatedAt, err := parseDBTime(updatedAtRaw)
	if err != nil {
		return APIIdempotencyRecord{}, fmt.Errorf("parse idempotency updated_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt
	if responseBodyNullable.Valid {
		record.ResponseBody = responseBodyNullable.String
	}

	return record, nil
}

func (s *Store) CompleteAPIIdempotency(ctx context.Context, authScope, idempotencyKey string, statusCode int, responseBody string) error {
	affected, err := s.db.ExecContext(
		ctx,
		`UPDATE api_idempotency
		 SET status_code = ?, response_body = ?, updated_at = ?
		 WHERE auth_scope = ?
		   AND idempotency_key = ?
		   AND status_code = 0`,
		statusCode,
		responseBody,
		formatDBTime(time.Now().UTC()),
		strings.TrimSpace(authScope),
		strings.TrimSpace(idempotencyKey),
	)
	if err != nil {
		return fmt.Errorf("complete api idempotency record: %w", err)
	}

	rowsAffected, err := affected.RowsAffected()
	if err != nil {
		return fmt.Errorf("read idempotency rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
