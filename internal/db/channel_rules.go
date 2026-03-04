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

type ChannelRuleInput struct {
	MaxTextLength  *int
	MaxHashtags    *int
	RequiredPhrase *string
}

func (s *Store) UpsertChannelRule(ctx context.Context, channelID int64, input ChannelRuleInput) (model.ChannelRule, error) {
	if channelID <= 0 {
		return model.ChannelRule{}, fmt.Errorf("channel_id must be positive")
	}

	normalized, err := normalizeChannelRuleInput(input)
	if err != nil {
		return model.ChannelRule{}, err
	}

	if normalized.MaxTextLength == nil && normalized.MaxHashtags == nil && normalized.RequiredPhrase == nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM channel_rules WHERE channel_id = ?`, channelID); err != nil {
			return model.ChannelRule{}, fmt.Errorf("clear channel rule for channel %d: %w", channelID, err)
		}
		return model.ChannelRule{ChannelID: channelID}, nil
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO channel_rules(channel_id, max_text_length, max_hashtags, required_phrase, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(channel_id) DO UPDATE SET
		   max_text_length = excluded.max_text_length,
		   max_hashtags = excluded.max_hashtags,
		   required_phrase = excluded.required_phrase,
		   updated_at = excluded.updated_at`,
		channelID,
		nullableInt(normalized.MaxTextLength),
		nullableInt(normalized.MaxHashtags),
		nullableString(normalized.RequiredPhrase),
		formatDBTime(now),
		formatDBTime(now),
	)
	if err != nil {
		return model.ChannelRule{}, fmt.Errorf("upsert channel rule for channel %d: %w", channelID, err)
	}

	rule, _, err := s.GetChannelRule(ctx, channelID)
	if err != nil {
		return model.ChannelRule{}, err
	}
	return rule, nil
}

func (s *Store) GetChannelRule(ctx context.Context, channelID int64) (model.ChannelRule, bool, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT channel_id, max_text_length, max_hashtags, required_phrase, created_at, updated_at
		 FROM channel_rules
		 WHERE channel_id = ?`,
		channelID,
	)

	rule, err := scanChannelRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ChannelRule{}, false, nil
		}
		return model.ChannelRule{}, false, fmt.Errorf("get channel rule for channel %d: %w", channelID, err)
	}
	return rule, true, nil
}

func (s *Store) ListChannelRulesByChannelIDs(ctx context.Context, ids []int64) (map[int64]model.ChannelRule, error) {
	unique := uniqueIDs(ids)
	if len(unique) == 0 {
		return map[int64]model.ChannelRule{}, nil
	}

	placeholders, err := inPlaceholders(len(unique))
	if err != nil {
		return nil, err
	}

	query := `SELECT channel_id, max_text_length, max_hashtags, required_phrase, created_at, updated_at
	 FROM channel_rules
	 WHERE channel_id IN (` + placeholders + `)`

	rows, err := s.db.QueryContext(ctx, query, int64Args(unique)...)
	if err != nil {
		return nil, fmt.Errorf("list channel rules by ids: %w", err)
	}
	defer rows.Close()

	rules := make(map[int64]model.ChannelRule, len(unique))
	for rows.Next() {
		rule, scanErr := scanChannelRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan channel rule row: %w", scanErr)
		}
		rules[rule.ChannelID] = rule
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel rules rows: %w", err)
	}

	return rules, nil
}

func scanChannelRule(s scanner) (model.ChannelRule, error) {
	var (
		channelID      int64
		maxTextLength  sql.NullInt64
		maxHashtags    sql.NullInt64
		requiredPhrase sql.NullString
		createdAt      string
		updatedAt      string
	)
	if err := s.Scan(&channelID, &maxTextLength, &maxHashtags, &requiredPhrase, &createdAt, &updatedAt); err != nil {
		return model.ChannelRule{}, err
	}

	created, err := parseDBTime(createdAt)
	if err != nil {
		return model.ChannelRule{}, fmt.Errorf("parse channel rule created_at: %w", err)
	}
	updated, err := parseDBTime(updatedAt)
	if err != nil {
		return model.ChannelRule{}, fmt.Errorf("parse channel rule updated_at: %w", err)
	}

	rule := model.ChannelRule{
		ChannelID: channelID,
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if maxTextLength.Valid {
		value := int(maxTextLength.Int64)
		rule.MaxTextLength = &value
	}
	if maxHashtags.Valid {
		value := int(maxHashtags.Int64)
		rule.MaxHashtags = &value
	}
	if requiredPhrase.Valid {
		value := requiredPhrase.String
		rule.RequiredPhrase = &value
	}

	return rule, nil
}

func normalizeChannelRuleInput(input ChannelRuleInput) (ChannelRuleInput, error) {
	normalized := ChannelRuleInput{}

	if input.MaxTextLength != nil {
		if *input.MaxTextLength <= 0 {
			return ChannelRuleInput{}, fmt.Errorf("max_text_length must be greater than zero")
		}
		value := *input.MaxTextLength
		normalized.MaxTextLength = &value
	}
	if input.MaxHashtags != nil {
		if *input.MaxHashtags <= 0 {
			return ChannelRuleInput{}, fmt.Errorf("max_hashtags must be greater than zero")
		}
		value := *input.MaxHashtags
		normalized.MaxHashtags = &value
	}
	if input.RequiredPhrase != nil {
		trimmed := strings.TrimSpace(*input.RequiredPhrase)
		if trimmed != "" {
			normalized.RequiredPhrase = &trimmed
		}
	}

	return normalized, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
