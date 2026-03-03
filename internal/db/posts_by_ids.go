package db

import (
	"context"
	"fmt"

	"linkedin-cron/internal/model"
)

func (s *Store) ListPostsByIDs(ctx context.Context, ids []int64) ([]model.Post, error) {
	unique := uniqueIDs(ids)
	if len(unique) == 0 {
		return []model.Post{}, nil
	}

	placeholders, err := inPlaceholders(len(unique))
	if err != nil {
		return nil, err
	}

	query := `SELECT id, scheduled_at, text, status, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 WHERE id IN (` + placeholders + `)
		 ORDER BY id ASC`

	rows, err := s.db.QueryContext(ctx, query, int64Args(unique)...)
	if err != nil {
		return nil, fmt.Errorf("list posts by ids: %w", err)
	}
	defer rows.Close()

	items := make([]model.Post, 0, len(unique))
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan posts by ids row: %w", scanErr)
		}
		items = append(items, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts by ids rows: %w", err)
	}
	return items, nil
}
