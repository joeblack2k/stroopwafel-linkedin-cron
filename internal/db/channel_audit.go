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

type ChannelAuditEventInput struct {
	ChannelID int64
	EventType string
	Actor     string
	Summary   string
	Metadata  *string
	CreatedAt time.Time
}

func (s *Store) InsertChannelAuditEvent(ctx context.Context, input ChannelAuditEventInput) (model.ChannelAuditEvent, error) {
	if input.ChannelID <= 0 {
		return model.ChannelAuditEvent{}, fmt.Errorf("channel_id must be positive")
	}

	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		return model.ChannelAuditEvent{}, fmt.Errorf("event_type is required")
	}

	actor := strings.TrimSpace(input.Actor)
	if actor == "" {
		actor = "unknown"
	}

	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		summary = "channel updated"
	}

	createdAt := input.CreatedAt.UTC()
	if input.CreatedAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO channel_audit_events(channel_id, event_type, actor, summary, metadata, created_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		input.ChannelID,
		eventType,
		actor,
		summary,
		nullableString(input.Metadata),
		formatDBTime(createdAt),
	)
	if err != nil {
		return model.ChannelAuditEvent{}, fmt.Errorf("insert channel audit event: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.ChannelAuditEvent{}, fmt.Errorf("read channel audit event id: %w", err)
	}

	return s.GetChannelAuditEvent(ctx, id)
}

func (s *Store) GetChannelAuditEvent(ctx context.Context, id int64) (model.ChannelAuditEvent, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, channel_id, event_type, actor, summary, metadata, created_at
		 FROM channel_audit_events
		 WHERE id = ?`,
		id,
	)
	event, err := scanChannelAuditEvent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ChannelAuditEvent{}, ErrNotFound
		}
		return model.ChannelAuditEvent{}, fmt.Errorf("get channel audit event %d: %w", id, err)
	}
	return event, nil
}

func (s *Store) ListChannelAuditEvents(ctx context.Context, channelID int64, limit, offset int) ([]model.ChannelAuditEvent, error) {
	if channelID <= 0 {
		return nil, fmt.Errorf("channel_id must be positive")
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, channel_id, event_type, actor, summary, metadata, created_at
		 FROM channel_audit_events
		 WHERE channel_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`,
		channelID,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list channel audit events for channel %d: %w", channelID, err)
	}
	defer rows.Close()

	events := make([]model.ChannelAuditEvent, 0)
	for rows.Next() {
		event, scanErr := scanChannelAuditEvent(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan channel audit event row: %w", scanErr)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel audit event rows: %w", err)
	}

	return events, nil
}

func (s *Store) CountChannelAuditEvents(ctx context.Context, channelID int64) (int, error) {
	if channelID <= 0 {
		return 0, fmt.Errorf("channel_id must be positive")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM channel_audit_events WHERE channel_id = ?`, channelID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count channel audit events for channel %d: %w", channelID, err)
	}
	return count, nil
}

func scanChannelAuditEvent(s scanner) (model.ChannelAuditEvent, error) {
	var (
		id        int64
		channelID int64
		eventType string
		actor     string
		summary   string
		metadata  sql.NullString
		createdAt string
	)
	if err := s.Scan(&id, &channelID, &eventType, &actor, &summary, &metadata, &createdAt); err != nil {
		return model.ChannelAuditEvent{}, err
	}

	eventTime, err := parseDBTime(createdAt)
	if err != nil {
		return model.ChannelAuditEvent{}, fmt.Errorf("parse created_at: %w", err)
	}

	event := model.ChannelAuditEvent{
		ID:        id,
		ChannelID: channelID,
		EventType: eventType,
		Actor:     actor,
		Summary:   summary,
		CreatedAt: eventTime,
	}
	if metadata.Valid {
		value := metadata.String
		event.Metadata = &value
	}

	return event, nil
}
