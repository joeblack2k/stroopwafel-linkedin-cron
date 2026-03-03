package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"linkedin-cron/internal/model"
)

type ChannelInput struct {
	Type                       model.ChannelType
	DisplayName                string
	LinkedInAccessToken        *string
	LinkedInAuthorURN          *string
	LinkedInAPIBaseURL         *string
	FacebookPageAccessToken    *string
	FacebookPageID             *string
	FacebookAPIBaseURL         *string
	InstagramAccessToken       *string
	InstagramBusinessAccountID *string
	InstagramAPIBaseURL        *string
}

func (s *Store) CreateChannel(ctx context.Context, input ChannelInput) (model.Channel, error) {
	if err := model.ValidateChannelInput(input.Type, input.DisplayName); err != nil {
		return model.Channel{}, err
	}

	now := time.Now().UTC()
	status, validationError := validateChannelConfig(input)
	lastError := nullableValidationError(validationError)

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO channels(
			 type,
			 display_name,
			 status,
			 created_at,
			 updated_at,
			 last_test_at,
			 last_error,
			 linkedin_access_token,
			 linkedin_author_urn,
			 linkedin_api_base_url,
			 facebook_page_access_token,
			 facebook_page_id,
			 facebook_api_base_url,
			 instagram_access_token,
			 instagram_business_account_id,
			 instagram_api_base_url
		 ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(input.Type),
		strings.TrimSpace(input.DisplayName),
		status,
		formatDBTime(now),
		formatDBTime(now),
		formatDBTime(now),
		lastError,
		nullableString(input.LinkedInAccessToken),
		nullableString(input.LinkedInAuthorURN),
		nullableString(input.LinkedInAPIBaseURL),
		nullableString(input.FacebookPageAccessToken),
		nullableString(input.FacebookPageID),
		nullableString(input.FacebookAPIBaseURL),
		nullableString(input.InstagramAccessToken),
		nullableString(input.InstagramBusinessAccountID),
		nullableString(input.InstagramAPIBaseURL),
	)
	if err != nil {
		return model.Channel{}, fmt.Errorf("insert channel: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.Channel{}, fmt.Errorf("read inserted channel id: %w", err)
	}
	return s.GetChannel(ctx, id)
}

func (s *Store) GetChannel(ctx context.Context, id int64) (model.Channel, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, type, display_name, status, created_at, updated_at, last_test_at, last_error,
			linkedin_access_token, linkedin_author_urn, linkedin_api_base_url,
			facebook_page_access_token, facebook_page_id, facebook_api_base_url,
			instagram_access_token, instagram_business_account_id, instagram_api_base_url
		 FROM channels
		 WHERE id = ?`,
		id,
	)
	channel, err := scanChannel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Channel{}, ErrNotFound
		}
		return model.Channel{}, fmt.Errorf("get channel %d: %w", id, err)
	}
	return channel, nil
}

func (s *Store) ListChannels(ctx context.Context) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, type, display_name, status, created_at, updated_at, last_test_at, last_error,
			linkedin_access_token, linkedin_author_urn, linkedin_api_base_url,
			facebook_page_access_token, facebook_page_id, facebook_api_base_url,
			instagram_access_token, instagram_business_account_id, instagram_api_base_url
		 FROM channels
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0)
	for rows.Next() {
		channel, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan channel row: %w", err)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels rows: %w", err)
	}
	return channels, nil
}

func (s *Store) ListChannelsForPost(ctx context.Context, postID int64) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT c.id, c.type, c.display_name, c.status, c.created_at, c.updated_at, c.last_test_at, c.last_error,
			c.linkedin_access_token, c.linkedin_author_urn, c.linkedin_api_base_url,
			c.facebook_page_access_token, c.facebook_page_id, c.facebook_api_base_url,
			c.instagram_access_token, c.instagram_business_account_id, c.instagram_api_base_url
		 FROM channels c
		 INNER JOIN post_channels pc ON pc.channel_id = c.id
		 WHERE pc.post_id = ?
		 ORDER BY c.id ASC`,
		postID,
	)
	if err != nil {
		return nil, fmt.Errorf("list channels for post %d: %w", postID, err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0)
	for rows.Next() {
		channel, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan channel for post row: %w", err)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels for post rows: %w", err)
	}
	return channels, nil
}

func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete channel tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_channels WHERE channel_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete channel links: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete channel %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("read rows affected for delete channel %d: %w", id, err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete channel tx: %w", err)
	}
	return nil
}

func (s *Store) TestChannel(ctx context.Context, id int64) (model.Channel, error) {
	channel, err := s.GetChannel(ctx, id)
	if err != nil {
		return model.Channel{}, err
	}

	status := model.ChannelStatusActive
	var lastError *string
	if validationErr := validateLoadedChannel(channel); validationErr != nil {
		status = model.ChannelStatusError
		message := validationErr.Error()
		lastError = &message
	}

	return s.SetChannelTestResult(ctx, id, status, lastError)
}

func (s *Store) SetChannelTestResult(ctx context.Context, id int64, status string, lastError *string) (model.Channel, error) {
	now := time.Now().UTC()
	var lastErrorValue any
	if lastError != nil {
		trimmed := strings.TrimSpace(*lastError)
		if trimmed != "" {
			lastErrorValue = trimmed
		}
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE channels
		 SET status = ?, updated_at = ?, last_test_at = ?, last_error = ?
		 WHERE id = ?`,
		status,
		formatDBTime(now),
		formatDBTime(now),
		lastErrorValue,
		id,
	)
	if err != nil {
		return model.Channel{}, fmt.Errorf("update test status for channel %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Channel{}, fmt.Errorf("read rows affected for test status channel %d: %w", id, err)
	}
	if affected == 0 {
		return model.Channel{}, ErrNotFound
	}

	return s.GetChannel(ctx, id)
}

func (s *Store) ReplacePostChannels(ctx context.Context, postID int64, channelIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace post channels tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_channels WHERE post_id = ?`, postID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete post channel links: %w", err)
	}

	unique := uniqueIDs(channelIDs)
	now := formatDBTime(time.Now().UTC())
	for _, channelID := range unique {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM channels WHERE id = ?`, channelID).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("validate channel %d: %w", channelID, err)
		}
		if exists == 0 {
			_ = tx.Rollback()
			return fmt.Errorf("channel %d not found", channelID)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO post_channels(post_id, channel_id, created_at) VALUES(?, ?, ?)`, postID, channelID, now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert post channel link post=%d channel=%d: %w", postID, channelID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace post channels tx: %w", err)
	}
	return nil
}

func (s *Store) ListPostChannelIDs(ctx context.Context, postID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT channel_id
		 FROM post_channels
		 WHERE post_id = ?
		 ORDER BY channel_id ASC`,
		postID,
	)
	if err != nil {
		return nil, fmt.Errorf("list post channels: %w", err)
	}
	defer rows.Close()

	channelIDs := make([]int64, 0)
	for rows.Next() {
		var channelID int64
		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("scan post channel row: %w", err)
		}
		channelIDs = append(channelIDs, channelID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate post channels rows: %w", err)
	}
	return channelIDs, nil
}

func scanChannel(s scanner) (model.Channel, error) {
	var (
		id                      int64
		channelType             string
		displayName             string
		status                  string
		createdAt               string
		updatedAt               string
		lastTestAt              sql.NullString
		lastError               sql.NullString
		linkedinAccessToken     sql.NullString
		linkedinAuthorURN       sql.NullString
		linkedinAPIBaseURL      sql.NullString
		facebookPageAccessToken sql.NullString
		facebookPageID          sql.NullString
		facebookAPIBaseURL      sql.NullString
		instagramAccessToken    sql.NullString
		instagramBusinessID     sql.NullString
		instagramAPIBaseURL     sql.NullString
	)

	if err := s.Scan(
		&id,
		&channelType,
		&displayName,
		&status,
		&createdAt,
		&updatedAt,
		&lastTestAt,
		&lastError,
		&linkedinAccessToken,
		&linkedinAuthorURN,
		&linkedinAPIBaseURL,
		&facebookPageAccessToken,
		&facebookPageID,
		&facebookAPIBaseURL,
		&instagramAccessToken,
		&instagramBusinessID,
		&instagramAPIBaseURL,
	); err != nil {
		return model.Channel{}, err
	}

	created, err := parseDBTime(createdAt)
	if err != nil {
		return model.Channel{}, fmt.Errorf("parse channel created_at: %w", err)
	}
	updated, err := parseDBTime(updatedAt)
	if err != nil {
		return model.Channel{}, fmt.Errorf("parse channel updated_at: %w", err)
	}

	channel := model.Channel{
		ID:          id,
		Type:        model.ChannelType(channelType),
		DisplayName: displayName,
		Status:      status,
		CreatedAt:   created,
		UpdatedAt:   updated,
	}
	if lastTestAt.Valid {
		value, err := parseDBTime(lastTestAt.String)
		if err != nil {
			return model.Channel{}, fmt.Errorf("parse channel last_test_at: %w", err)
		}
		channel.LastTestAt = &value
	}
	if lastError.Valid {
		value := lastError.String
		channel.LastError = &value
	}
	if linkedinAccessToken.Valid {
		value := linkedinAccessToken.String
		channel.LinkedInAccessToken = &value
	}
	if linkedinAuthorURN.Valid {
		value := linkedinAuthorURN.String
		channel.LinkedInAuthorURN = &value
	}
	if linkedinAPIBaseURL.Valid {
		value := linkedinAPIBaseURL.String
		channel.LinkedInAPIBaseURL = &value
	}
	if facebookPageAccessToken.Valid {
		value := facebookPageAccessToken.String
		channel.FacebookPageAccessToken = &value
	}
	if facebookPageID.Valid {
		value := facebookPageID.String
		channel.FacebookPageID = &value
	}
	if facebookAPIBaseURL.Valid {
		value := facebookAPIBaseURL.String
		channel.FacebookAPIBaseURL = &value
	}
	if instagramAccessToken.Valid {
		value := instagramAccessToken.String
		channel.InstagramAccessToken = &value
	}
	if instagramBusinessID.Valid {
		value := instagramBusinessID.String
		channel.InstagramBusinessAccountID = &value
	}
	if instagramAPIBaseURL.Valid {
		value := instagramAPIBaseURL.String
		channel.InstagramAPIBaseURL = &value
	}

	return channel, nil
}

func validateChannelConfig(input ChannelInput) (string, error) {
	switch input.Type {
	case model.ChannelTypeDryRun:
		return model.ChannelStatusActive, nil
	case model.ChannelTypeLinkedIn:
		if strings.TrimSpace(derefNullableString(input.LinkedInAccessToken)) == "" {
			return model.ChannelStatusError, fmt.Errorf("linkedin_access_token is required")
		}
		if strings.TrimSpace(derefNullableString(input.LinkedInAuthorURN)) == "" {
			return model.ChannelStatusError, fmt.Errorf("linkedin_author_urn is required")
		}
		return model.ChannelStatusActive, nil
	case model.ChannelTypeFacebook:
		if strings.TrimSpace(derefNullableString(input.FacebookPageAccessToken)) == "" {
			return model.ChannelStatusError, fmt.Errorf("facebook_page_access_token is required")
		}
		if strings.TrimSpace(derefNullableString(input.FacebookPageID)) == "" {
			return model.ChannelStatusError, fmt.Errorf("facebook_page_id is required")
		}
		return model.ChannelStatusActive, nil
	case model.ChannelTypeInstagram:
		if strings.TrimSpace(derefNullableString(input.InstagramAccessToken)) == "" {
			return model.ChannelStatusError, fmt.Errorf("instagram_access_token is required")
		}
		if strings.TrimSpace(derefNullableString(input.InstagramBusinessAccountID)) == "" {
			return model.ChannelStatusError, fmt.Errorf("instagram_business_account_id is required")
		}
		return model.ChannelStatusActive, nil
	default:
		return model.ChannelStatusError, fmt.Errorf("unsupported channel type: %s", input.Type)
	}
}

func validateLoadedChannel(channel model.Channel) error {
	input := ChannelInput{
		Type:                       channel.Type,
		DisplayName:                channel.DisplayName,
		LinkedInAccessToken:        channel.LinkedInAccessToken,
		LinkedInAuthorURN:          channel.LinkedInAuthorURN,
		LinkedInAPIBaseURL:         channel.LinkedInAPIBaseURL,
		FacebookPageAccessToken:    channel.FacebookPageAccessToken,
		FacebookPageID:             channel.FacebookPageID,
		FacebookAPIBaseURL:         channel.FacebookAPIBaseURL,
		InstagramAccessToken:       channel.InstagramAccessToken,
		InstagramBusinessAccountID: channel.InstagramBusinessAccountID,
		InstagramAPIBaseURL:        channel.InstagramAPIBaseURL,
	}
	_, err := validateChannelConfig(input)
	return err
}

func nullableValidationError(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func uniqueIDs(values []int64) []int64 {
	set := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		set[value] = struct{}{}
	}
	keys := make([]int64, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func derefNullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Store) ListChannelsByIDs(ctx context.Context, ids []int64) ([]model.Channel, error) {
	unique := uniqueIDs(ids)
	if len(unique) == 0 {
		return []model.Channel{}, nil
	}

	placeholders, err := inPlaceholders(len(unique))
	if err != nil {
		return nil, err
	}

	query := `SELECT id, type, display_name, status, created_at, updated_at, last_test_at, last_error,
		linkedin_access_token, linkedin_author_urn, linkedin_api_base_url,
		facebook_page_access_token, facebook_page_id, facebook_api_base_url,
		instagram_access_token, instagram_business_account_id, instagram_api_base_url
	 FROM channels
	 WHERE id IN (` + placeholders + `)
	 ORDER BY id ASC`

	rows, err := s.db.QueryContext(ctx, query, int64Args(unique)...)
	if err != nil {
		return nil, fmt.Errorf("list channels by ids: %w", err)
	}
	defer rows.Close()

	channels := make([]model.Channel, 0, len(unique))
	for rows.Next() {
		channel, scanErr := scanChannel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan channels by ids row: %w", scanErr)
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels by ids rows: %w", err)
	}
	return channels, nil
}

func (s *Store) SetChannelStatus(ctx context.Context, id int64, status string, lastError *string) (model.Channel, error) {
	if !isAllowedChannelStatus(status) {
		return model.Channel{}, fmt.Errorf("invalid channel status: %s", status)
	}

	now := time.Now().UTC()
	var lastErrorValue any
	if lastError != nil {
		trimmed := strings.TrimSpace(*lastError)
		if trimmed != "" {
			lastErrorValue = trimmed
		}
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE channels
		 SET status = ?, updated_at = ?, last_error = ?
		 WHERE id = ?`,
		status,
		formatDBTime(now),
		lastErrorValue,
		id,
	)
	if err != nil {
		return model.Channel{}, fmt.Errorf("update status for channel %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Channel{}, fmt.Errorf("read rows affected for update status channel %d: %w", id, err)
	}
	if affected == 0 {
		return model.Channel{}, ErrNotFound
	}

	return s.GetChannel(ctx, id)
}

func isAllowedChannelStatus(status string) bool {
	switch status {
	case model.ChannelStatusActive, model.ChannelStatusError, model.ChannelStatusDisabled:
		return true
	default:
		return false
	}
}
