package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"stroopwafel/internal/model"
)

type ContentTemplateInput struct {
	Name         string
	Description  *string
	Body         string
	ChannelType  *string
	MediaAssetID *int64
	Tags         []string
	IsActive     *bool
}

type ContentTemplateFilter struct {
	Query       string
	Tag         string
	ChannelType string
	IsActive    *bool
}

type ChannelDeliveryFilter struct {
	From *time.Time
	To   *time.Time
}

type ChannelDeliveryStat struct {
	ChannelID   int64
	ChannelType model.ChannelType
	DisplayName string
	SentCount   int
	FailedCount int
	RetryCount  int
	TotalCount  int
	SuccessRate float64
}

func (s *Store) CreateContentTemplate(ctx context.Context, input ContentTemplateInput) (model.ContentTemplate, error) {
	normalized, err := normalizeContentTemplateInput(input)
	if err != nil {
		return model.ContentTemplate{}, err
	}

	if normalized.MediaAssetID != nil {
		if _, err := s.GetMediaAsset(ctx, *normalized.MediaAssetID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return model.ContentTemplate{}, fmt.Errorf("media_asset_id does not exist")
			}
			return model.ContentTemplate{}, err
		}
	}

	now := time.Now().UTC()
	tagsJSON, err := json.Marshal(normalized.Tags)
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("encode template tags: %w", err)
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO content_templates(name, description, body, channel_type, media_asset_id, tags, is_active, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.Name,
		nullableString(normalized.Description),
		normalized.Body,
		nullableString(normalized.ChannelType),
		nullableInt64(normalized.MediaAssetID),
		string(tagsJSON),
		boolToInt(*normalized.IsActive),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("create content template %q: %w", normalized.Name, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("read content template insert id: %w", err)
	}

	return s.GetContentTemplate(ctx, id)
}

func (s *Store) GetContentTemplate(ctx context.Context, id int64) (model.ContentTemplate, error) {
	if id <= 0 {
		return model.ContentTemplate{}, fmt.Errorf("template id must be positive")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, description, body, channel_type, media_asset_id, tags, is_active, created_at, updated_at
		 FROM content_templates
		 WHERE id = ?`,
		id,
	)
	template, err := scanContentTemplate(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ContentTemplate{}, ErrNotFound
		}
		return model.ContentTemplate{}, fmt.Errorf("get content template %d: %w", id, err)
	}
	return template, nil
}

func (s *Store) UpdateContentTemplate(ctx context.Context, id int64, input ContentTemplateInput) (model.ContentTemplate, error) {
	if id <= 0 {
		return model.ContentTemplate{}, fmt.Errorf("template id must be positive")
	}

	existing, err := s.GetContentTemplate(ctx, id)
	if err != nil {
		return model.ContentTemplate{}, err
	}

	merged := ContentTemplateInput{
		Name:         fallbackString(strings.TrimSpace(input.Name), existing.Name),
		Body:         fallbackString(strings.TrimSpace(input.Body), existing.Body),
		Description:  existing.Description,
		ChannelType:  nil,
		MediaAssetID: existing.MediaAssetID,
		Tags:         existing.Tags,
		IsActive:     &existing.IsActive,
	}
	if input.Description != nil {
		merged.Description = input.Description
	}
	if input.ChannelType != nil {
		merged.ChannelType = input.ChannelType
	} else if existing.ChannelType != nil {
		value := string(*existing.ChannelType)
		merged.ChannelType = &value
	}
	if input.MediaAssetID != nil {
		merged.MediaAssetID = input.MediaAssetID
	}
	if len(input.Tags) > 0 {
		merged.Tags = input.Tags
	}
	if input.IsActive != nil {
		merged.IsActive = input.IsActive
	}

	normalized, err := normalizeContentTemplateInput(merged)
	if err != nil {
		return model.ContentTemplate{}, err
	}
	if normalized.MediaAssetID != nil {
		if _, err := s.GetMediaAsset(ctx, *normalized.MediaAssetID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return model.ContentTemplate{}, fmt.Errorf("media_asset_id does not exist")
			}
			return model.ContentTemplate{}, err
		}
	}
	tagsJSON, err := json.Marshal(normalized.Tags)
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("encode template tags: %w", err)
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE content_templates
		 SET name = ?,
		     description = ?,
		     body = ?,
		     channel_type = ?,
		     media_asset_id = ?,
		     tags = ?,
		     is_active = ?,
		     updated_at = ?
		 WHERE id = ?`,
		normalized.Name,
		nullableString(normalized.Description),
		normalized.Body,
		nullableString(normalized.ChannelType),
		nullableInt64(normalized.MediaAssetID),
		string(tagsJSON),
		boolToInt(*normalized.IsActive),
		formatDBTime(time.Now().UTC()),
		id,
	)
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("update content template %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("read rows affected for content template %d: %w", id, err)
	}
	if affected == 0 {
		return model.ContentTemplate{}, ErrNotFound
	}

	return s.GetContentTemplate(ctx, id)
}

func (s *Store) DeleteContentTemplate(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("template id must be positive")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM content_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete content template %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for content template %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListContentTemplates(ctx context.Context, filter ContentTemplateFilter, limit, offset int) ([]model.ContentTemplate, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildContentTemplatesQuery(filter, false)
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list content templates: %w", err)
	}
	defer rows.Close()

	templates := make([]model.ContentTemplate, 0)
	for rows.Next() {
		template, scanErr := scanContentTemplate(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan content template row: %w", scanErr)
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content template rows: %w", err)
	}

	return templates, nil
}

func (s *Store) CountContentTemplates(ctx context.Context, filter ContentTemplateFilter) (int, error) {
	query, args := buildContentTemplatesQuery(filter, true)
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count content templates: %w", err)
	}
	return total, nil
}

func (s *Store) ListChannelDeliveryStats(ctx context.Context, filter ChannelDeliveryFilter, limit, offset int) ([]ChannelDeliveryStat, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	dateFilterSQL := ""
	args := make([]any, 0, 4)
	if filter.From != nil {
		dateFilterSQL += ` AND pa.attempted_at >= ?`
		args = append(args, formatDBTime(filter.From.UTC()))
	}
	if filter.To != nil {
		dateFilterSQL += ` AND pa.attempted_at <= ?`
		args = append(args, formatDBTime(filter.To.UTC()))
	}

	query := `WITH filtered_attempts AS (
		SELECT pa.channel_id, pa.status
		FROM publish_attempts pa
		WHERE 1=1` + dateFilterSQL + `
	)
	SELECT
		c.id,
		c.type,
		c.display_name,
		COALESCE(SUM(CASE WHEN filtered_attempts.status = 'sent' THEN 1 ELSE 0 END), 0) AS sent_count,
		COALESCE(SUM(CASE WHEN filtered_attempts.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_count,
		COALESCE(SUM(CASE WHEN filtered_attempts.status = 'retry' THEN 1 ELSE 0 END), 0) AS retry_count,
		COALESCE(COUNT(filtered_attempts.status), 0) AS total_count
	FROM channels c
	LEFT JOIN filtered_attempts ON filtered_attempts.channel_id = c.id
	GROUP BY c.id, c.type, c.display_name
	ORDER BY total_count DESC, c.id ASC
	LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list channel delivery stats: %w", err)
	}
	defer rows.Close()

	stats := make([]ChannelDeliveryStat, 0)
	for rows.Next() {
		var (
			item        ChannelDeliveryStat
			channelType string
		)
		if scanErr := rows.Scan(&item.ChannelID, &channelType, &item.DisplayName, &item.SentCount, &item.FailedCount, &item.RetryCount, &item.TotalCount); scanErr != nil {
			return nil, fmt.Errorf("scan channel delivery stat row: %w", scanErr)
		}
		item.ChannelType = model.ChannelType(channelType)
		if item.TotalCount > 0 {
			item.SuccessRate = (float64(item.SentCount) / float64(item.TotalCount)) * 100
		}
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel delivery stats rows: %w", err)
	}

	return stats, nil
}

func (s *Store) CountChannelDeliveryStats(ctx context.Context) (int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM channels`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count channel delivery stats channels: %w", err)
	}
	return total, nil
}

func buildContentTemplatesQuery(filter ContentTemplateFilter, countOnly bool) (string, []any) {
	query := `SELECT id, name, description, body, channel_type, media_asset_id, tags, is_active, created_at, updated_at
	 FROM content_templates
	 WHERE 1=1`
	if countOnly {
		query = `SELECT COUNT(1)
	 FROM content_templates
	 WHERE 1=1`
	}

	args := make([]any, 0, 5)

	if trimmed := strings.TrimSpace(filter.Query); trimmed != "" {
		wildcard := "%" + strings.ToLower(trimmed) + "%"
		query += ` AND (LOWER(name) LIKE ? OR LOWER(body) LIKE ?)`
		args = append(args, wildcard, wildcard)
	}
	if trimmed := strings.ToLower(strings.TrimSpace(filter.ChannelType)); trimmed != "" {
		query += ` AND channel_type = ?`
		args = append(args, trimmed)
	}
	if filter.IsActive != nil {
		query += ` AND is_active = ?`
		args = append(args, boolToInt(*filter.IsActive))
	}
	if trimmed := strings.ToLower(strings.TrimSpace(filter.Tag)); trimmed != "" {
		query += ` AND LOWER(tags) LIKE ?`
		args = append(args, "%\""+trimmed+"\"%")
	}

	return query, args
}

func normalizeContentTemplateInput(input ContentTemplateInput) (ContentTemplateInput, error) {
	normalized := ContentTemplateInput{
		Name:         strings.TrimSpace(input.Name),
		Body:         strings.TrimSpace(input.Body),
		Description:  nil,
		ChannelType:  nil,
		MediaAssetID: input.MediaAssetID,
		Tags:         normalizeTags(input.Tags),
		IsActive:     input.IsActive,
	}

	if normalized.Name == "" {
		return ContentTemplateInput{}, fmt.Errorf("name is required")
	}
	if normalized.Body == "" {
		return ContentTemplateInput{}, fmt.Errorf("body is required")
	}
	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		normalized.Description = &trimmed
	}
	if input.ChannelType != nil {
		trimmed := strings.ToLower(strings.TrimSpace(*input.ChannelType))
		if trimmed != "" {
			channelType := model.ChannelType(trimmed)
			if !channelType.Valid() {
				return ContentTemplateInput{}, fmt.Errorf("channel_type is invalid")
			}
			normalized.ChannelType = &trimmed
		}
	}
	if normalized.MediaAssetID != nil && *normalized.MediaAssetID <= 0 {
		return ContentTemplateInput{}, fmt.Errorf("media_asset_id must be positive")
	}
	if normalized.IsActive == nil {
		value := true
		normalized.IsActive = &value
	}

	return normalized, nil
}

func scanContentTemplate(s scanner) (model.ContentTemplate, error) {
	var (
		template     model.ContentTemplate
		description  sql.NullString
		channelType  sql.NullString
		mediaAssetID sql.NullInt64
		tagsRaw      string
		isActiveRaw  int
		createdRaw   string
		updatedRaw   string
	)

	if err := s.Scan(
		&template.ID,
		&template.Name,
		&description,
		&template.Body,
		&channelType,
		&mediaAssetID,
		&tagsRaw,
		&isActiveRaw,
		&createdRaw,
		&updatedRaw,
	); err != nil {
		return model.ContentTemplate{}, err
	}

	template.Name = strings.TrimSpace(template.Name)
	template.Body = strings.TrimSpace(template.Body)
	template.IsActive = isActiveRaw == 1
	if description.Valid {
		value := strings.TrimSpace(description.String)
		template.Description = &value
	}
	if channelType.Valid {
		value := model.ChannelType(strings.ToLower(strings.TrimSpace(channelType.String)))
		template.ChannelType = &value
	}
	if mediaAssetID.Valid {
		value := mediaAssetID.Int64
		template.MediaAssetID = &value
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(tagsRaw)), &template.Tags); err != nil {
		template.Tags = nil
	}
	template.Tags = normalizeTags(template.Tags)

	createdAt, err := parseDBTime(strings.TrimSpace(createdRaw))
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("parse content template created_at: %w", err)
	}
	updatedAt, err := parseDBTime(strings.TrimSpace(updatedRaw))
	if err != nil {
		return model.ContentTemplate{}, fmt.Errorf("parse content template updated_at: %w", err)
	}
	template.CreatedAt = createdAt
	template.UpdatedAt = updatedAt

	return template, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
