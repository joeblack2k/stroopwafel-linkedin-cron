package db

import (
	"context"
	"fmt"
	"time"

	"stroopwafel/internal/model"
)

func (s *Store) ListScheduledPostsInWindow(ctx context.Context, start, end time.Time, excludePostID int64) ([]model.Post, error) {
	if end.Before(start) {
		start, end = end, start
	}

	query := `SELECT id, scheduled_at, text, status, approval_pending, planning_approved, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
	 FROM posts
	 WHERE status = 'scheduled'
	   AND approval_pending = 0
	   AND scheduled_at IS NOT NULL
	   AND scheduled_at >= ?
	   AND scheduled_at <= ?`
	args := []any{formatDBTime(start.UTC()), formatDBTime(end.UTC())}
	if excludePostID > 0 {
		query += ` AND id != ?`
		args = append(args, excludePostID)
	}
	query += ` ORDER BY scheduled_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list scheduled posts in window: %w", err)
	}
	defer rows.Close()

	posts := make([]model.Post, 0)
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan scheduled window post row: %w", scanErr)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduled window posts rows: %w", err)
	}

	return posts, nil
}
