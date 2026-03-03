package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"linkedin-cron/internal/model"

	_ "modernc.org/sqlite"
)

const dbTimeLayout = time.RFC3339

var ErrNotFound = errors.New("record not found")

type Store struct {
	db *sql.DB
}

type PostInput struct {
	ScheduledAt *time.Time
	Text        string
	Status      model.PostStatus
	MediaURL    *string
}

const apiKeyTokenPrefix = "lcak_"

func EnsureDBDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}
	return database, nil
}

func NewStore(database *sql.DB) *Store {
	return &Store{db: database}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreatePost(ctx context.Context, input PostInput) (model.Post, error) {
	now := time.Now().UTC()
	status := string(input.Status)

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO posts(scheduled_at, text, status, created_at, updated_at, media_url) VALUES(?, ?, ?, ?, ?, ?)`,
		nullableTime(input.ScheduledAt),
		strings.TrimSpace(input.Text),
		status,
		formatDBTime(now),
		formatDBTime(now),
		nullableString(input.MediaURL),
	)
	if err != nil {
		return model.Post{}, fmt.Errorf("insert post: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.Post{}, fmt.Errorf("read inserted id: %w", err)
	}
	return s.GetPost(ctx, id)
}

func (s *Store) UpdatePost(ctx context.Context, id int64, input PostInput) (model.Post, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE posts
		 SET scheduled_at = ?, text = ?, status = ?, media_url = ?, updated_at = ?
		 WHERE id = ?`,
		nullableTime(input.ScheduledAt),
		strings.TrimSpace(input.Text),
		string(input.Status),
		nullableString(input.MediaURL),
		formatDBTime(now),
		id,
	)
	if err != nil {
		return model.Post{}, fmt.Errorf("update post %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Post{}, fmt.Errorf("read rows affected for post %d: %w", id, err)
	}
	if affected == 0 {
		return model.Post{}, ErrNotFound
	}
	return s.GetPost(ctx, id)
}

func (s *Store) GetPost(ctx context.Context, id int64) (model.Post, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, scheduled_at, text, status, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 WHERE id = ?`,
		id,
	)
	post, err := scanPost(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Post{}, ErrNotFound
		}
		return model.Post{}, fmt.Errorf("get post %d: %w", id, err)
	}
	return post, nil
}

func (s *Store) DeletePost(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete post %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for delete %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPosts(ctx context.Context) ([]model.Post, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, scheduled_at, text, status, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	posts := make([]model.Post, 0)
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan list posts row: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate list posts rows: %w", err)
	}
	return posts, nil
}

func (s *Store) ListPostsByScheduledRange(ctx context.Context, start, end time.Time) ([]model.Post, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, scheduled_at, text, status, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 WHERE scheduled_at IS NOT NULL
		   AND scheduled_at >= ?
		   AND scheduled_at < ?
		 ORDER BY scheduled_at ASC, id ASC`,
		formatDBTime(start.UTC()),
		formatDBTime(end.UTC()),
	)
	if err != nil {
		return nil, fmt.Errorf("list posts by range: %w", err)
	}
	defer rows.Close()

	posts := make([]model.Post, 0)
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan posts by range row: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts by range rows: %w", err)
	}
	return posts, nil
}

func (s *Store) ListDuePosts(ctx context.Context, now time.Time, limit int) ([]model.Post, error) {
	if limit <= 0 {
		limit = 100
	}
	nowFormatted := formatDBTime(now.UTC())

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, scheduled_at, text, status, created_at, updated_at, sent_at, fail_count, last_error, media_url, next_retry_at
		 FROM posts
		 WHERE status = 'scheduled'
		   AND (
			 (next_retry_at IS NULL AND scheduled_at IS NOT NULL AND scheduled_at <= ?)
			 OR
			 (next_retry_at IS NOT NULL AND next_retry_at <= ?)
		   )
		 ORDER BY COALESCE(next_retry_at, scheduled_at) ASC, id ASC
		 LIMIT ?`,
		nowFormatted,
		nowFormatted,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due posts: %w", err)
	}
	defer rows.Close()

	posts := make([]model.Post, 0)
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan due post row: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due posts rows: %w", err)
	}
	return posts, nil
}

func (s *Store) MarkSent(ctx context.Context, id int64, sentAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE posts
		 SET status = 'sent', sent_at = ?, updated_at = ?, last_error = NULL, next_retry_at = NULL
		 WHERE id = ?`,
		formatDBTime(sentAt.UTC()),
		formatDBTime(sentAt.UTC()),
		id,
	)
	if err != nil {
		return fmt.Errorf("mark post %d as sent: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for mark sent %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordFailure(ctx context.Context, id int64, failCount int, lastError string, status model.PostStatus, nextRetryAt *time.Time, updatedAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE posts
		 SET fail_count = ?, last_error = ?, status = ?, next_retry_at = ?, updated_at = ?
		 WHERE id = ?`,
		failCount,
		lastError,
		string(status),
		nullableTime(nextRetryAt),
		formatDBTime(updatedAt.UTC()),
		id,
	)
	if err != nil {
		return fmt.Errorf("record failure for post %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for failure %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateAPIKey(ctx context.Context, name string) (model.APIKey, string, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return model.APIKey{}, "", fmt.Errorf("api key name is required")
	}

	token, err := generateAPIKeyToken()
	if err != nil {
		return model.APIKey{}, "", fmt.Errorf("generate api key token: %w", err)
	}

	now := time.Now().UTC()
	hash := hashAPIKeyToken(token)
	prefix := visiblePrefix(token)

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO api_keys(name, key_prefix, key_hash, created_at) VALUES(?, ?, ?, ?)`,
		trimmedName,
		prefix,
		hash,
		formatDBTime(now),
	)
	if err != nil {
		return model.APIKey{}, "", fmt.Errorf("insert api key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.APIKey{}, "", fmt.Errorf("read inserted api key id: %w", err)
	}

	created, err := s.GetAPIKey(ctx, id)
	if err != nil {
		return model.APIKey{}, "", err
	}
	return created, token, nil
}

func (s *Store) GetAPIKey(ctx context.Context, id int64) (model.APIKey, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, key_prefix, created_at, last_used_at, revoked_at
		 FROM api_keys
		 WHERE id = ?`,
		id,
	)
	key, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.APIKey{}, ErrNotFound
		}
		return model.APIKey{}, fmt.Errorf("get api key %d: %w", id, err)
	}
	return key, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]model.APIKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, name, key_prefix, created_at, last_used_at, revoked_at
		 FROM api_keys
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	keys := make([]model.APIKey, 0)
	for rows.Next() {
		item, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		keys = append(keys, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys rows: %w", err)
	}
	return keys, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE api_keys
		 SET revoked_at = COALESCE(revoked_at, ?)
		 WHERE id = ?`,
		formatDBTime(time.Now().UTC()),
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke api key %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for revoke api key %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AuthenticateAPIKey(ctx context.Context, token string) (model.APIKey, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return model.APIKey{}, ErrNotFound
	}

	hash := hashAPIKeyToken(trimmed)
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, key_prefix, created_at, last_used_at, revoked_at
		 FROM api_keys
		 WHERE key_hash = ?
		   AND revoked_at IS NULL`,
		hash,
	)
	key, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.APIKey{}, ErrNotFound
		}
		return model.APIKey{}, fmt.Errorf("find api key by hash: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, formatDBTime(now), key.ID); err != nil {
		return model.APIKey{}, fmt.Errorf("update api key last_used_at: %w", err)
	}
	key.LastUsedAt = &now
	return key, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPost(s scanner) (model.Post, error) {
	var (
		id          int64
		scheduledAt sql.NullString
		text        string
		status      string
		createdAt   string
		updatedAt   string
		sentAt      sql.NullString
		failCount   int
		lastError   sql.NullString
		mediaURL    sql.NullString
		nextRetryAt sql.NullString
	)
	if err := s.Scan(&id, &scheduledAt, &text, &status, &createdAt, &updatedAt, &sentAt, &failCount, &lastError, &mediaURL, &nextRetryAt); err != nil {
		return model.Post{}, err
	}

	created, err := parseDBTime(createdAt)
	if err != nil {
		return model.Post{}, fmt.Errorf("parse created_at: %w", err)
	}
	updated, err := parseDBTime(updatedAt)
	if err != nil {
		return model.Post{}, fmt.Errorf("parse updated_at: %w", err)
	}

	post := model.Post{
		ID:        id,
		Text:      text,
		Status:    model.PostStatus(status),
		CreatedAt: created,
		UpdatedAt: updated,
		FailCount: failCount,
	}
	if scheduledAt.Valid {
		value, err := parseDBTime(scheduledAt.String)
		if err != nil {
			return model.Post{}, fmt.Errorf("parse scheduled_at: %w", err)
		}
		post.ScheduledAt = &value
	}
	if sentAt.Valid {
		value, err := parseDBTime(sentAt.String)
		if err != nil {
			return model.Post{}, fmt.Errorf("parse sent_at: %w", err)
		}
		post.SentAt = &value
	}
	if nextRetryAt.Valid {
		value, err := parseDBTime(nextRetryAt.String)
		if err != nil {
			return model.Post{}, fmt.Errorf("parse next_retry_at: %w", err)
		}
		post.NextRetryAt = &value
	}
	if lastError.Valid {
		value := lastError.String
		post.LastError = &value
	}
	if mediaURL.Valid {
		value := mediaURL.String
		post.MediaURL = &value
	}

	return post, nil
}

func scanAPIKey(s scanner) (model.APIKey, error) {
	var (
		id         int64
		name       string
		keyPrefix  string
		createdAt  string
		lastUsedAt sql.NullString
		revokedAt  sql.NullString
	)
	if err := s.Scan(&id, &name, &keyPrefix, &createdAt, &lastUsedAt, &revokedAt); err != nil {
		return model.APIKey{}, err
	}

	created, err := parseDBTime(createdAt)
	if err != nil {
		return model.APIKey{}, fmt.Errorf("parse api key created_at: %w", err)
	}

	item := model.APIKey{
		ID:        id,
		Name:      name,
		KeyPrefix: keyPrefix,
		CreatedAt: created,
	}
	if lastUsedAt.Valid {
		parsed, err := parseDBTime(lastUsedAt.String)
		if err != nil {
			return model.APIKey{}, fmt.Errorf("parse api key last_used_at: %w", err)
		}
		item.LastUsedAt = &parsed
	}
	if revokedAt.Valid {
		parsed, err := parseDBTime(revokedAt.String)
		if err != nil {
			return model.APIKey{}, fmt.Errorf("parse api key revoked_at: %w", err)
		}
		item.RevokedAt = &parsed
	}
	return item, nil
}

func formatDBTime(value time.Time) string {
	return value.UTC().Format(dbTimeLayout)
}

func parseDBTime(value string) (time.Time, error) {
	parsed, err := time.Parse(dbTimeLayout, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatDBTime(*value)
}

func generateAPIKeyToken() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return apiKeyTokenPrefix + hex.EncodeToString(randomBytes), nil
}

func hashAPIKeyToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func visiblePrefix(token string) string {
	trimmed := strings.TrimSpace(token)
	if len(trimmed) <= 10 {
		return trimmed
	}
	return trimmed[:10]
}
