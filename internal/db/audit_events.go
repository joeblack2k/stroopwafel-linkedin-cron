package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type AuditEventInput struct {
	Action    string
	Resource  string
	AuthActor string
	Source    string
	Metadata  *string
	CreatedAt time.Time
}

type AuditEvent struct {
	ID        int64
	Action    string
	Resource  string
	AuthActor string
	Source    string
	Metadata  *string
	CreatedAt time.Time
}

type AuditEventFilter struct {
	Action    string
	Resource  string
	AuthActor string
	Source    string
}

func (s *Store) CreateAuditEvent(ctx context.Context, input AuditEventInput) (AuditEvent, error) {
	action := strings.TrimSpace(input.Action)
	if action == "" {
		return AuditEvent{}, fmt.Errorf("action is required")
	}

	resource := strings.TrimSpace(input.Resource)
	if resource == "" {
		return AuditEvent{}, fmt.Errorf("resource is required")
	}

	createdAt := input.CreatedAt.UTC()
	if input.CreatedAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_events(action, resource, auth_actor, source, metadata, created_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		action,
		resource,
		normalizeAuditActor(input.AuthActor),
		normalizeAuditSource(input.Source),
		nullableString(input.Metadata),
		formatDBTime(createdAt),
	)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("insert audit event: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return AuditEvent{}, fmt.Errorf("read audit event id: %w", err)
	}

	return s.GetAuditEvent(ctx, id)
}

func (s *Store) GetAuditEvent(ctx context.Context, id int64) (AuditEvent, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, action, resource, auth_actor, source, metadata, created_at
		 FROM audit_events
		 WHERE id = ?`,
		id,
	)

	event, err := scanAuditEvent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuditEvent{}, ErrNotFound
		}
		return AuditEvent{}, fmt.Errorf("get audit event %d: %w", id, err)
	}
	return event, nil
}

func (s *Store) ListAuditEvents(ctx context.Context, filter AuditEventFilter, limit, offset int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildAuditEventQuery(filter, false)
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0)
	for rows.Next() {
		event, scanErr := scanAuditEvent(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan audit event row: %w", scanErr)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events rows: %w", err)
	}

	return events, nil
}

func (s *Store) CountAuditEvents(ctx context.Context, filter AuditEventFilter) (int, error) {
	query, args := buildAuditEventQuery(filter, true)

	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count audit events: %w", err)
	}

	return total, nil
}

func buildAuditEventQuery(filter AuditEventFilter, countOnly bool) (string, []any) {
	query := `SELECT id, action, resource, auth_actor, source, metadata, created_at FROM audit_events WHERE 1=1`
	if countOnly {
		query = `SELECT COUNT(1) FROM audit_events WHERE 1=1`
	}

	args := make([]any, 0, 4)

	action := strings.TrimSpace(filter.Action)
	if action != "" {
		query += ` AND action = ?`
		args = append(args, action)
	}

	resource := strings.TrimSpace(filter.Resource)
	if resource != "" {
		query += ` AND resource = ?`
		args = append(args, resource)
	}

	authActor := strings.TrimSpace(filter.AuthActor)
	if authActor != "" {
		query += ` AND auth_actor = ?`
		args = append(args, authActor)
	}

	source := strings.TrimSpace(filter.Source)
	if source != "" {
		query += ` AND source = ?`
		args = append(args, source)
	}

	return query, args
}

func scanAuditEvent(s scanner) (AuditEvent, error) {
	var (
		id        int64
		action    string
		resource  string
		authActor string
		source    string
		metadata  sql.NullString
		createdAt string
	)

	if err := s.Scan(&id, &action, &resource, &authActor, &source, &metadata, &createdAt); err != nil {
		return AuditEvent{}, err
	}

	eventTime, err := parseDBTime(createdAt)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("parse audit event created_at: %w", err)
	}

	event := AuditEvent{
		ID:        id,
		Action:    action,
		Resource:  resource,
		AuthActor: authActor,
		Source:    source,
		CreatedAt: eventTime,
	}
	if metadata.Valid {
		value := metadata.String
		event.Metadata = &value
	}

	return event, nil
}
