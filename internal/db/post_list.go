package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"linkedin-cron/internal/model"
)

type PostListFilter struct {
	Status        model.PostStatus
	SearchQuery   string
	ChannelID     *int64
	ScheduledFrom *time.Time
	ScheduledTo   *time.Time
}

func (s *Store) ListPostsFiltered(ctx context.Context, filter PostListFilter, limit, offset int) ([]model.Post, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildPostListQuery(filter, false)
	query += ` ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list posts filtered: %w", err)
	}
	defer rows.Close()

	posts := make([]model.Post, 0)
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan filtered posts row: %w", scanErr)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate filtered posts rows: %w", err)
	}

	return posts, nil
}

func (s *Store) CountPostsFiltered(ctx context.Context, filter PostListFilter) (int, error) {
	query, args := buildPostListQuery(filter, true)

	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count filtered posts: %w", err)
	}
	return total, nil
}

func buildPostListQuery(filter PostListFilter, countOnly bool) (string, []any) {
	selectClause := `SELECT p.id, p.scheduled_at, p.text, p.status, p.created_at, p.updated_at, p.sent_at, p.fail_count, p.last_error, p.media_url, p.next_retry_at`
	if countOnly {
		selectClause = `SELECT COUNT(1)`
	}
	query := selectClause + `
		 FROM posts p
		 WHERE 1 = 1`
	args := make([]any, 0, 8)

	if filter.Status.Valid() {
		query += ` AND p.status = ?`
		args = append(args, string(filter.Status))
	}

	trimmedSearch := strings.TrimSpace(filter.SearchQuery)
	if trimmedSearch != "" {
		query += ` AND p.text LIKE ?`
		args = append(args, "%"+trimmedSearch+"%")
	}

	if filter.ChannelID != nil && *filter.ChannelID > 0 {
		query += ` AND EXISTS (SELECT 1 FROM post_channels pc WHERE pc.post_id = p.id AND pc.channel_id = ?)`
		args = append(args, *filter.ChannelID)
	}

	if filter.ScheduledFrom != nil {
		query += ` AND p.scheduled_at IS NOT NULL AND p.scheduled_at >= ?`
		args = append(args, formatDBTime(filter.ScheduledFrom.UTC()))
	}
	if filter.ScheduledTo != nil {
		query += ` AND p.scheduled_at IS NOT NULL AND p.scheduled_at <= ?`
		args = append(args, formatDBTime(filter.ScheduledTo.UTC()))
	}

	return query, args
}
