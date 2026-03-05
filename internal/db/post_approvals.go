package db

import (
	"context"
	"fmt"
	"time"

	"stroopwafel/internal/model"
)

func (s *Store) ListPendingApprovalPosts(ctx context.Context, limit int) ([]model.Post, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, scheduled_at, text, status, approval_pending, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 WHERE approval_pending = 1
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending approval posts: %w", err)
	}
	defer rows.Close()

	items := make([]model.Post, 0)
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan pending approval post row: %w", scanErr)
		}
		items = append(items, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending approval posts rows: %w", err)
	}

	return items, nil
}

func (s *Store) AcceptPostPlanning(ctx context.Context, id int64) (model.Post, error) {
	post, err := s.GetPost(ctx, id)
	if err != nil {
		return model.Post{}, err
	}
	if !post.ApprovalPending {
		return model.Post{}, fmt.Errorf("post does not require approval")
	}
	if post.ScheduledAt == nil {
		return model.Post{}, fmt.Errorf("post has no scheduled_at to plan")
	}
	if post.Status == model.StatusSent {
		return model.Post{}, fmt.Errorf("sent posts cannot be approved for planning")
	}

	now := time.Now().UTC()
	status := post.Status
	if status == model.StatusDraft || status == model.StatusFailed {
		status = model.StatusScheduled
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE posts
		 SET status = ?, approval_pending = 0, updated_at = ?
		 WHERE id = ?`,
		string(status),
		formatDBTime(now),
		id,
	)
	if err != nil {
		return model.Post{}, fmt.Errorf("accept post planning %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return model.Post{}, fmt.Errorf("read rows affected for accept post planning %d: %w", id, err)
	}
	if rowsAffected == 0 {
		return model.Post{}, ErrNotFound
	}

	return s.GetPost(ctx, id)
}
