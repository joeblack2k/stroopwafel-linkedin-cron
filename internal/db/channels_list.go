package db

import (
	"context"
	"fmt"
	"strings"

	"linkedin-cron/internal/model"
)

type ChannelListFilter struct {
	Type    model.ChannelType
	Status  string
	SearchQ string
}

func (s *Store) ListChannelsFiltered(ctx context.Context, filter ChannelListFilter, limit, offset int) ([]model.Channel, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildChannelListQuery(filter, false)
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list channels filtered: %w", err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0)
	for rows.Next() {
		channel, scanErr := scanChannel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan filtered channels row: %w", scanErr)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate filtered channels rows: %w", err)
	}
	return channels, nil
}

func (s *Store) CountChannelsFiltered(ctx context.Context, filter ChannelListFilter) (int, error) {
	query, args := buildChannelListQuery(filter, true)

	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count filtered channels: %w", err)
	}
	return total, nil
}

func buildChannelListQuery(filter ChannelListFilter, countOnly bool) (string, []any) {
	selectClause := `SELECT c.id, c.type, c.display_name, c.status, c.created_at, c.updated_at, c.last_test_at, c.last_error,
			c.linkedin_access_token, c.linkedin_author_urn, c.linkedin_api_base_url,
			c.facebook_page_access_token, c.facebook_page_id, c.facebook_api_base_url,
			c.instagram_access_token, c.instagram_business_account_id, c.instagram_api_base_url`
	if countOnly {
		selectClause = `SELECT COUNT(1)`
	}

	query := selectClause + `
		 FROM channels c
		 WHERE 1 = 1`
	args := make([]any, 0, 4)

	if filter.Type.Valid() {
		query += ` AND c.type = ?`
		args = append(args, string(filter.Type))
	}

	trimmedStatus := strings.ToLower(strings.TrimSpace(filter.Status))
	if trimmedStatus == model.ChannelStatusActive || trimmedStatus == model.ChannelStatusError || trimmedStatus == model.ChannelStatusDisabled {
		query += ` AND c.status = ?`
		args = append(args, trimmedStatus)
	}

	trimmedSearch := strings.TrimSpace(filter.SearchQ)
	if trimmedSearch != "" {
		query += ` AND c.display_name LIKE ?`
		args = append(args, "%"+trimmedSearch+"%")
	}

	return query, args
}
