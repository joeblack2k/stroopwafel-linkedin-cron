package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type WebhookDeliveryInput struct {
	EventID     string
	EventName   string
	TargetURL   string
	Status      string
	HTTPStatus  *int
	Error       *string
	Source      string
	DurationMS  int64
	OccurredAt  time.Time
	DeliveredAt time.Time
}

type WebhookDelivery struct {
	ID          int64
	EventID     string
	EventName   string
	TargetURL   string
	Status      string
	HTTPStatus  *int
	Error       *string
	Source      string
	DurationMS  int64
	OccurredAt  time.Time
	DeliveredAt time.Time
}

type WebhookTargetStat struct {
	TargetURL       string
	Total           int
	Delivered       int
	Failed          int
	LastStatus      string
	LastHTTPStatus  *int
	LastError       *string
	LastEventName   string
	LastDeliveredAt *time.Time
}

func (s *Store) InsertWebhookDelivery(ctx context.Context, input WebhookDeliveryInput) (WebhookDelivery, error) {
	createdAt := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO webhook_deliveries(
			event_id,
			event_name,
			target_url,
			status,
			http_status,
			error,
			source,
			duration_ms,
			occurred_at,
			delivered_at,
			created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.EventID,
		input.EventName,
		input.TargetURL,
		input.Status,
		nullableInt(input.HTTPStatus),
		nullableString(input.Error),
		input.Source,
		input.DurationMS,
		formatDBTime(input.OccurredAt.UTC()),
		formatDBTime(input.DeliveredAt.UTC()),
		formatDBTime(createdAt),
	)
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("insert webhook delivery: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("read webhook delivery id: %w", err)
	}

	return s.GetWebhookDelivery(ctx, id)
}

func (s *Store) GetWebhookDelivery(ctx context.Context, id int64) (WebhookDelivery, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, event_id, event_name, target_url, status, http_status, error, source, duration_ms, occurred_at, delivered_at
		 FROM webhook_deliveries
		 WHERE id = ?`,
		id,
	)

	delivery, err := scanWebhookDelivery(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return WebhookDelivery{}, ErrNotFound
		}
		return WebhookDelivery{}, fmt.Errorf("get webhook delivery %d: %w", id, err)
	}
	return delivery, nil
}

func (s *Store) ListRecentWebhookDeliveries(ctx context.Context, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, event_id, event_name, target_url, status, http_status, error, source, duration_ms, occurred_at, delivered_at
		 FROM webhook_deliveries
		 ORDER BY delivered_at DESC, id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent webhook deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]WebhookDelivery, 0)
	for rows.Next() {
		item, scanErr := scanWebhookDelivery(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan webhook delivery row: %w", scanErr)
		}
		deliveries = append(deliveries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook delivery rows: %w", err)
	}
	return deliveries, nil
}

func (s *Store) ListWebhookTargetStats(ctx context.Context) ([]WebhookTargetStat, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			wd.target_url,
			COUNT(1) AS total,
			COALESCE(SUM(CASE WHEN wd.status = 'delivered' THEN 1 ELSE 0 END), 0) AS delivered,
			COALESCE(SUM(CASE WHEN wd.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed,
			last.status,
			last.http_status,
			last.error,
			last.event_name,
			last.delivered_at
		 FROM webhook_deliveries wd
		 LEFT JOIN webhook_deliveries last
		   ON last.id = (
			   SELECT inner_wd.id
			   FROM webhook_deliveries inner_wd
			   WHERE inner_wd.target_url = wd.target_url
			   ORDER BY inner_wd.delivered_at DESC, inner_wd.id DESC
			   LIMIT 1
		   )
		 GROUP BY wd.target_url
		 ORDER BY last.delivered_at DESC, wd.target_url ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list webhook target stats: %w", err)
	}
	defer rows.Close()

	stats := make([]WebhookTargetStat, 0)
	for rows.Next() {
		var (
			item               WebhookTargetStat
			httpStatusRaw      sql.NullInt64
			lastErrorRaw       sql.NullString
			lastDeliveredAtRaw sql.NullString
		)
		if scanErr := rows.Scan(
			&item.TargetURL,
			&item.Total,
			&item.Delivered,
			&item.Failed,
			&item.LastStatus,
			&httpStatusRaw,
			&lastErrorRaw,
			&item.LastEventName,
			&lastDeliveredAtRaw,
		); scanErr != nil {
			return nil, fmt.Errorf("scan webhook target stats row: %w", scanErr)
		}
		if httpStatusRaw.Valid {
			value := int(httpStatusRaw.Int64)
			item.LastHTTPStatus = &value
		}
		if lastErrorRaw.Valid {
			value := lastErrorRaw.String
			item.LastError = &value
		}
		if lastDeliveredAtRaw.Valid {
			parsed, parseErr := parseDBTime(lastDeliveredAtRaw.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse webhook last_delivered_at: %w", parseErr)
			}
			item.LastDeliveredAt = &parsed
		}
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook target stats rows: %w", err)
	}
	return stats, nil
}

func scanWebhookDelivery(scanner interface{ Scan(dest ...any) error }) (WebhookDelivery, error) {
	var (
		item           WebhookDelivery
		httpStatusRaw  sql.NullInt64
		errorRaw       sql.NullString
		occurredAtRaw  string
		deliveredAtRaw string
	)
	if err := scanner.Scan(
		&item.ID,
		&item.EventID,
		&item.EventName,
		&item.TargetURL,
		&item.Status,
		&httpStatusRaw,
		&errorRaw,
		&item.Source,
		&item.DurationMS,
		&occurredAtRaw,
		&deliveredAtRaw,
	); err != nil {
		return WebhookDelivery{}, err
	}

	occurredAt, err := parseDBTime(occurredAtRaw)
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("parse webhook occurred_at: %w", err)
	}
	deliveredAt, err := parseDBTime(deliveredAtRaw)
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("parse webhook delivered_at: %w", err)
	}
	item.OccurredAt = occurredAt
	item.DeliveredAt = deliveredAt

	if httpStatusRaw.Valid {
		value := int(httpStatusRaw.Int64)
		item.HTTPStatus = &value
	}
	if errorRaw.Valid {
		value := errorRaw.String
		item.Error = &value
	}

	return item, nil
}
