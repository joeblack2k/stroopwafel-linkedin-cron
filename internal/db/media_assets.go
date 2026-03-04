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

type MediaAssetInput struct {
	MediaURL   string
	MediaType  string
	Filename   *string
	SizeBytes  int64
	StoredPath *string
	Source     string
	Tags       []string
	Metadata   map[string]string
}

type MediaAssetFilter struct {
	Query     string
	Tag       string
	MediaType string
}

func (s *Store) UpsertMediaAssetByURL(ctx context.Context, input MediaAssetInput) (model.MediaAsset, error) {
	normalized, err := normalizeMediaAssetInput(input)
	if err != nil {
		return model.MediaAsset{}, err
	}

	now := time.Now().UTC()
	tagsJSON, err := json.Marshal(normalized.Tags)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media tags: %w", err)
	}
	metadataJSON, err := json.Marshal(normalized.Metadata)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media metadata: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO media_assets(media_url, media_type, filename, size_bytes, stored_path, source, tags, metadata, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(media_url) DO UPDATE SET
		   media_type = excluded.media_type,
		   filename = COALESCE(excluded.filename, media_assets.filename),
		   size_bytes = CASE WHEN excluded.size_bytes > 0 THEN excluded.size_bytes ELSE media_assets.size_bytes END,
		   stored_path = COALESCE(excluded.stored_path, media_assets.stored_path),
		   source = excluded.source,
		   tags = excluded.tags,
		   metadata = excluded.metadata,
		   updated_at = excluded.updated_at`,
		normalized.MediaURL,
		normalized.MediaType,
		nullableString(normalized.Filename),
		normalized.SizeBytes,
		nullableString(normalized.StoredPath),
		normalized.Source,
		string(tagsJSON),
		string(metadataJSON),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("upsert media asset for url %q: %w", normalized.MediaURL, err)
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, media_url, media_type, filename, size_bytes, stored_path, source, tags, metadata, created_at, updated_at
		 FROM media_assets
		 WHERE media_url = ?`,
		normalized.MediaURL,
	)
	asset, err := scanMediaAsset(row)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("load upserted media asset by url %q: %w", normalized.MediaURL, err)
	}
	return asset, nil
}

func (s *Store) CreateMediaAsset(ctx context.Context, input MediaAssetInput) (model.MediaAsset, error) {
	normalized, err := normalizeMediaAssetInput(input)
	if err != nil {
		return model.MediaAsset{}, err
	}

	now := time.Now().UTC()
	tagsJSON, err := json.Marshal(normalized.Tags)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media tags: %w", err)
	}
	metadataJSON, err := json.Marshal(normalized.Metadata)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media metadata: %w", err)
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO media_assets(media_url, media_type, filename, size_bytes, stored_path, source, tags, metadata, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.MediaURL,
		normalized.MediaType,
		nullableString(normalized.Filename),
		normalized.SizeBytes,
		nullableString(normalized.StoredPath),
		normalized.Source,
		string(tagsJSON),
		string(metadataJSON),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("create media asset for url %q: %w", normalized.MediaURL, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("read media asset insert id: %w", err)
	}

	return s.GetMediaAsset(ctx, id)
}

func (s *Store) GetMediaAsset(ctx context.Context, id int64) (model.MediaAsset, error) {
	if id <= 0 {
		return model.MediaAsset{}, fmt.Errorf("media asset id must be positive")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, media_url, media_type, filename, size_bytes, stored_path, source, tags, metadata, created_at, updated_at
		 FROM media_assets
		 WHERE id = ?`,
		id,
	)

	asset, err := scanMediaAsset(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.MediaAsset{}, ErrNotFound
		}
		return model.MediaAsset{}, fmt.Errorf("get media asset %d: %w", id, err)
	}
	return asset, nil
}

func (s *Store) UpdateMediaAsset(ctx context.Context, id int64, input MediaAssetInput) (model.MediaAsset, error) {
	if id <= 0 {
		return model.MediaAsset{}, fmt.Errorf("media asset id must be positive")
	}

	existing, err := s.GetMediaAsset(ctx, id)
	if err != nil {
		return model.MediaAsset{}, err
	}

	merged := MediaAssetInput{
		MediaURL:  fallbackString(strings.TrimSpace(input.MediaURL), existing.MediaURL),
		MediaType: fallbackString(strings.TrimSpace(input.MediaType), existing.MediaType),
		Source:    fallbackString(strings.TrimSpace(input.Source), existing.Source),
		SizeBytes: existing.SizeBytes,
		Tags:      existing.Tags,
		Metadata:  existing.Metadata,
	}
	if input.Filename != nil {
		merged.Filename = input.Filename
	} else {
		merged.Filename = existing.Filename
	}
	if input.StoredPath != nil {
		merged.StoredPath = input.StoredPath
	} else {
		merged.StoredPath = existing.StoredPath
	}
	if input.SizeBytes > 0 {
		merged.SizeBytes = input.SizeBytes
	}
	if len(input.Tags) > 0 {
		merged.Tags = input.Tags
	}
	if input.Metadata != nil {
		merged.Metadata = input.Metadata
	}

	normalized, err := normalizeMediaAssetInput(merged)
	if err != nil {
		return model.MediaAsset{}, err
	}

	tagsJSON, err := json.Marshal(normalized.Tags)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media tags: %w", err)
	}
	metadataJSON, err := json.Marshal(normalized.Metadata)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("encode media metadata: %w", err)
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE media_assets
		 SET media_url = ?,
		     media_type = ?,
		     filename = ?,
		     size_bytes = ?,
		     stored_path = ?,
		     source = ?,
		     tags = ?,
		     metadata = ?,
		     updated_at = ?
		 WHERE id = ?`,
		normalized.MediaURL,
		normalized.MediaType,
		nullableString(normalized.Filename),
		normalized.SizeBytes,
		nullableString(normalized.StoredPath),
		normalized.Source,
		string(tagsJSON),
		string(metadataJSON),
		formatDBTime(time.Now().UTC()),
		id,
	)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("update media asset %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("read media asset rows affected for %d: %w", id, err)
	}
	if affected == 0 {
		return model.MediaAsset{}, ErrNotFound
	}

	return s.GetMediaAsset(ctx, id)
}

func (s *Store) DeleteMediaAsset(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("media asset id must be positive")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM media_assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete media asset %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read media asset rows affected for %d: %w", id, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListMediaAssets(ctx context.Context, filter MediaAssetFilter, limit, offset int) ([]model.MediaAsset, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query, args := buildMediaAssetsQuery(filter, false)
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list media assets: %w", err)
	}
	defer rows.Close()

	assets := make([]model.MediaAsset, 0)
	for rows.Next() {
		asset, scanErr := scanMediaAsset(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan media asset row: %w", scanErr)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media asset rows: %w", err)
	}

	return assets, nil
}

func (s *Store) CountMediaAssets(ctx context.Context, filter MediaAssetFilter) (int, error) {
	query, args := buildMediaAssetsQuery(filter, true)
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count media assets: %w", err)
	}
	return total, nil
}

func buildMediaAssetsQuery(filter MediaAssetFilter, countOnly bool) (string, []any) {
	query := `SELECT id, media_url, media_type, filename, size_bytes, stored_path, source, tags, metadata, created_at, updated_at
	 FROM media_assets
	 WHERE 1=1`
	if countOnly {
		query = `SELECT COUNT(1)
	 FROM media_assets
	 WHERE 1=1`
	}

	args := make([]any, 0, 4)

	if trimmed := strings.TrimSpace(filter.Query); trimmed != "" {
		wildcard := "%" + strings.ToLower(trimmed) + "%"
		query += ` AND (LOWER(media_url) LIKE ? OR LOWER(COALESCE(filename, '')) LIKE ?)`
		args = append(args, wildcard, wildcard)
	}
	if trimmed := strings.ToLower(strings.TrimSpace(filter.MediaType)); trimmed != "" {
		query += ` AND media_type = ?`
		args = append(args, trimmed)
	}
	if trimmed := strings.ToLower(strings.TrimSpace(filter.Tag)); trimmed != "" {
		query += ` AND LOWER(tags) LIKE ?`
		args = append(args, "%\""+trimmed+"\"%")
	}

	return query, args
}

func normalizeMediaAssetInput(input MediaAssetInput) (MediaAssetInput, error) {
	normalized := MediaAssetInput{
		MediaURL:  strings.TrimSpace(input.MediaURL),
		MediaType: strings.ToLower(strings.TrimSpace(input.MediaType)),
		Source:    strings.ToLower(strings.TrimSpace(input.Source)),
		SizeBytes: input.SizeBytes,
		Tags:      normalizeTags(input.Tags),
		Metadata:  normalizeStringMap(input.Metadata),
	}

	if normalized.MediaURL == "" {
		return MediaAssetInput{}, fmt.Errorf("media_url is required")
	}
	if !model.IsSupportedMediaType(normalized.MediaType) {
		return MediaAssetInput{}, fmt.Errorf("media_type must be one of: link, image, video")
	}
	if normalized.Source == "" {
		normalized.Source = "upload"
	}
	if normalized.SizeBytes < 0 {
		return MediaAssetInput{}, fmt.Errorf("size_bytes must be zero or greater")
	}
	if input.Filename != nil {
		trimmed := strings.TrimSpace(*input.Filename)
		normalized.Filename = &trimmed
	}
	if input.StoredPath != nil {
		trimmed := strings.TrimSpace(*input.StoredPath)
		normalized.StoredPath = &trimmed
	}

	return normalized, nil
}

func scanMediaAsset(s scanner) (model.MediaAsset, error) {
	var (
		asset       model.MediaAsset
		filenameRaw sql.NullString
		storedRaw   sql.NullString
		tagsRaw     string
		metadataRaw string
		createdRaw  string
		updatedRaw  string
	)

	if err := s.Scan(
		&asset.ID,
		&asset.MediaURL,
		&asset.MediaType,
		&filenameRaw,
		&asset.SizeBytes,
		&storedRaw,
		&asset.Source,
		&tagsRaw,
		&metadataRaw,
		&createdRaw,
		&updatedRaw,
	); err != nil {
		return model.MediaAsset{}, err
	}

	asset.MediaType = strings.ToLower(strings.TrimSpace(asset.MediaType))
	asset.Source = strings.ToLower(strings.TrimSpace(asset.Source))
	if filenameRaw.Valid {
		value := strings.TrimSpace(filenameRaw.String)
		asset.Filename = &value
	}
	if storedRaw.Valid {
		value := strings.TrimSpace(storedRaw.String)
		asset.StoredPath = &value
	}

	if err := json.Unmarshal([]byte(strings.TrimSpace(tagsRaw)), &asset.Tags); err != nil {
		asset.Tags = nil
	}
	asset.Tags = normalizeTags(asset.Tags)

	asset.Metadata = map[string]string{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(metadataRaw)), &asset.Metadata); err != nil {
		asset.Metadata = map[string]string{}
	}
	asset.Metadata = normalizeStringMap(asset.Metadata)

	createdAt, err := parseDBTime(strings.TrimSpace(createdRaw))
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("parse media asset created_at: %w", err)
	}
	updatedAt, err := parseDBTime(strings.TrimSpace(updatedRaw))
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("parse media asset updated_at: %w", err)
	}
	asset.CreatedAt = createdAt
	asset.UpdatedAt = updatedAt

	return asset, nil
}

func normalizeTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	clean := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	clean := make(map[string]string, len(values))
	for key, value := range values {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		clean[trimmedKey] = strings.TrimSpace(value)
	}
	if len(clean) == 0 {
		return map[string]string{}
	}
	return clean
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
